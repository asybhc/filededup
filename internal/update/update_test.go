package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func sampleAsset(t *testing.T, data []byte, priv ed25519.PrivateKey) Asset {
	t.Helper()
	sum := sha256.Sum256(data)
	return Asset{
		Name:      "fdedup-ui-new.exe",
		OS:        "windows",
		Arch:      "amd64",
		URL:       "https://example.com/fdedup-ui-new.exe",
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		Signature: SignPackageDigest(priv, hex.EncodeToString(sum[:])),
	}
}

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.8.0", "0.8.1", -1},
		{"0.8.1", "0.8.0", 1},
		{"0.8.0", "0.8", 0},
		{"v0.8.0", "0.8.0", 0},
		{"0.8.0-beta.1", "0.8.0", -1},
		{"0.8.0-beta.1", "0.8.0-beta.2", -1},
		{"1.0.0", "0.99.99", 1},
	}
	for _, c := range cases {
		got, err := CompareVersion(c.a, c.b)
		if err != nil {
			t.Fatalf("CompareVersion(%q,%q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("CompareVersion(%q,%q)=%d，期望 %d", c.a, c.b, got, c.want)
		}
	}
	for _, bad := range []string{"", "1.a", "1..2", "1.2.3.4.5"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) 应报错", bad)
		}
	}
}

func TestManifestParseAndVerify(t *testing.T) {
	pub, priv := testKey(t)
	data := []byte("fake-installer-bytes")
	m := Manifest{
		SchemaVersion: SchemaVersion,
		Kind:          Kind,
		Version:       "0.8.1",
		Assets:        []Asset{sampleAsset(t, data, priv)},
	}
	if err := SignManifest(&m, priv); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(m)

	parsed, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if err := VerifyManifest(parsed, pub); err != nil {
		t.Fatalf("VerifyManifest: %v", err)
	}
	if err := VerifyAsset(parsed.Assets[0], pub); err != nil {
		t.Fatalf("VerifyAsset: %v", err)
	}

	// 篡改说明字段后签名必须失效。
	tampered := parsed
	tampered.Notes = "被篡改"
	if err := VerifyManifest(tampered, pub); err == nil {
		t.Fatal("篡改后的清单不应通过验签")
	}

	// 未知字段必须被拒绝。
	withUnknown := append([]byte(`{"unexpected":1,`), raw[1:]...)
	if _, err := ParseManifest(withUnknown); err == nil {
		t.Fatal("含未知字段的清单应被拒绝")
	}

	// 未签名必须被拒绝。
	unsigned := parsed
	unsigned.Signature = ""
	if _, err := ParseManifest(mustJSON(t, unsigned)); err == nil {
		t.Fatal("未签名清单应被拒绝")
	}

	// 换一把公钥必须失败。
	otherPub, _ := testKey(t)
	if err := VerifyManifest(parsed, otherPub); err == nil {
		t.Fatal("错误公钥不应通过验签")
	}
}

func TestManifestRejectsBadAsset(t *testing.T) {
	_, priv := testKey(t)
	base := Asset{
		Name:      "a.exe",
		URL:       "https://example.com/a.exe",
		Size:      1,
		SHA256:    strings.Repeat("a", 64),
		Signature: "x",
	}
	cases := []struct {
		name string
		mut  func(*Asset)
	}{
		{"路径穿越", func(a *Asset) { a.Name = "../a.exe" }},
		{"非法协议", func(a *Asset) { a.URL = "file:///C:/a.exe" }},
		{"大小为 0", func(a *Asset) { a.Size = 0 }},
		{"摘要长度", func(a *Asset) { a.SHA256 = "abcd" }},
		{"缺签名", func(a *Asset) { a.Signature = "" }},
	}
	for _, c := range cases {
		a := base
		c.mut(&a)
		m := Manifest{SchemaVersion: SchemaVersion, Kind: Kind, Version: "1.0.0", Assets: []Asset{a}}
		if err := SignManifest(&m, priv); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseManifest(mustJSON(t, m)); err == nil {
			t.Errorf("%s：应被拒绝", c.name)
		}
	}
}

func TestSelectAsset(t *testing.T) {
	m := Manifest{Assets: []Asset{
		{Name: "linux.tar.gz", OS: "linux", Arch: "amd64"},
		{Name: "win.exe", OS: "windows", Arch: "amd64"},
	}}
	a, err := SelectAsset(m, "windows", "amd64")
	if err != nil || a.Name != "win.exe" {
		t.Fatalf("SelectAsset windows/amd64 = %q, %v", a.Name, err)
	}
	if _, err := SelectAsset(m, "darwin", "arm64"); err == nil {
		t.Fatal("无匹配平台时应报错")
	}
	m.Assets = append(m.Assets, Asset{Name: "generic.exe"})
	a, err = SelectAsset(m, "darwin", "arm64")
	if err != nil || a.Name != "generic.exe" {
		t.Fatalf("应回退到通用包，实际 %q, %v", a.Name, err)
	}
}

func TestDownloadAssetEndToEnd(t *testing.T) {
	pub, priv := testKey(t)
	data := []byte("real-installer-content-for-download")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	asset := sampleAsset(t, data, priv)
	asset.URL = srv.URL + "/fdedup-ui-new.exe"
	dir := t.TempDir()
	path, err := downloadAsset(context.Background(), srv.Client(), asset, dir, pub)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatal("下载内容不一致")
	}

	// 摘要不符必须拒绝，且不留下最终文件。
	bad := asset
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := downloadAsset(context.Background(), srv.Client(), bad, dir, pub); err == nil {
		t.Fatal("摘要不符应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(dir, bad.Name)); err == nil {
		t.Fatal("摘要不符时不应留下最终文件")
	}

	// 错误公钥必须拒绝包签名。
	otherPub, _ := testKey(t)
	if _, err := downloadAsset(context.Background(), srv.Client(), asset, dir, otherPub); err == nil {
		t.Fatal("错误公钥应拒绝下载")
	}
}

func TestReplaceAndRollback(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fdedup-ui.exe")
	pkg := filepath.Join(dir, "new.exe")
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pkg, []byte("new-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("new-binary"))
	sha := hex.EncodeToString(sum[:])

	var launched string
	res, err := Replace(exe, pkg, int64(len("new-binary")), sha, func(p string) error {
		launched = p
		return nil
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if launched != exe || res.BackupPath != exe+".old" {
		t.Fatalf("launch=%q backup=%q", launched, res.BackupPath)
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("替换后内容 = %q", got)
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "old-binary" {
		t.Fatalf("备份内容 = %q", got)
	}
	if err := Cleanup(exe); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(exe + ".old"); err == nil {
		t.Fatal("成功后 Cleanup 应删除备份")
	}

	// 启动失败必须回滚。
	if err := os.WriteFile(pkg, []byte("new-binary-2"), 0o755); err != nil {
		t.Fatal(err)
	}
	sum2 := sha256.Sum256([]byte("new-binary-2"))
	if _, err := Replace(exe, pkg, int64(len("new-binary-2")), hex.EncodeToString(sum2[:]), func(string) error {
		return os.ErrPermission
	}); err == nil {
		t.Fatal("启动失败时 Replace 应返回错误")
	}
	if got, _ := os.ReadFile(exe); string(got) != "new-binary" {
		t.Fatalf("回滚后内容 = %q，期望 new-binary", got)
	}
}

func TestReplaceRejectsBadDigest(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fdedup-ui.exe")
	pkg := filepath.Join(dir, "new.exe")
	_ = os.WriteFile(exe, []byte("old"), 0o755)
	_ = os.WriteFile(pkg, []byte("new"), 0o755)
	if _, err := Replace(exe, pkg, 3, strings.Repeat("0", 64), nil); err == nil {
		t.Fatal("摘要不符应拒绝替换")
	}
	if got, _ := os.ReadFile(exe); string(got) != "old" {
		t.Fatalf("拒绝替换时原程序不应变化，实际 %q", got)
	}
}

func TestCleanupRestoresMissingExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fdedup-ui.exe")
	_ = os.WriteFile(exe+".old", []byte("backup"), 0o755)
	if err := Cleanup(exe); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "backup" {
		t.Fatalf("应恢复备份，实际 %q", got)
	}
}

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPruneCacheKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, age time.Duration) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("fdedup-ui-0.7.0.exe", 72*time.Hour)
	mid := write("fdedup-ui-0.8.0.exe", 24*time.Hour)
	newest := write("fdedup-ui-0.8.1.exe", time.Hour)
	part := write("fdedup-ui-0.8.1.exe.part", 10*time.Minute)
	oldCopy := write("fdedup-ui.exe.old", 20*time.Minute)
	newCopy := write("fdedup-ui.exe.new", 5*time.Minute)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := PruneCache(dir, 2)
	if err != nil {
		t.Fatalf("PruneCache 返回错误：%v", err)
	}
	if res.Kept != 2 || res.Removed != 4 {
		t.Fatalf("统计不符：Kept=%d Removed=%d Freed=%d", res.Kept, res.Removed, res.Freed)
	}
	for _, p := range []string{old, part, oldCopy, newCopy} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s 应被删除", filepath.Base(p))
		}
	}
	for _, p := range []string{mid, newest} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s 应保留", filepath.Base(p))
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Fatal("子目录不应被删除")
	}
}

func TestPruneCacheMissingDir(t *testing.T) {
	res, err := PruneCache(filepath.Join(t.TempDir(), "nope"), 2)
	if err != nil {
		t.Fatalf("目录不存在应返回 nil 错误，实际 %v", err)
	}
	if res.Removed != 0 || res.Kept != 0 {
		t.Fatalf("目录不存在不应有统计：%+v", res)
	}
}
