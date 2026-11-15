package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedKeyEndToEnd 用真实更新私钥走完整链路：签清单、HTTP 取清单、
// 用内置公钥验签、下载安装包、校验长度/摘要/包签名，再在临时目录执行替换。
// 私钥不在仓库里；未设置 FILEDEDUP_UPDATE_KEY 时跳过。
func TestEmbeddedKeyEndToEnd(t *testing.T) {
	keyPath := os.Getenv("FILEDEDUP_UPDATE_KEY")
	if keyPath == "" {
		t.Skip("未设置 FILEDEDUP_UPDATE_KEY，跳过真实密钥端到端测试")
	}
	rawKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("读取更新私钥失败：%v", err)
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(rawKey)))
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("更新私钥格式不正确")
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub, err := PublicKey()
	if err != nil {
		t.Fatalf("读取内置公钥失败：%v", err)
	}
	if !pub.Equal(priv.Public()) {
		t.Fatal("内置公钥与签名私钥不匹配")
	}

	pkgData := []byte("signed-installer-payload-for-e2e")
	sha := sha256Hex(pkgData)
	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestBytes(t, baseURL, pkgData, priv))
		case "/pkg.exe":
			_, _ = w.Write(pkgData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	baseURL = srv.URL

	man, _, err := FetchManifest(context.Background(), srv.Client(), srv.URL+"/update.json")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if err := VerifyManifest(man, pub); err != nil {
		t.Fatalf("内置公钥验签清单失败：%v", err)
	}
	asset, err := SelectAsset(man, "windows", "amd64")
	if err != nil {
		t.Fatalf("SelectAsset: %v", err)
	}
	dir := t.TempDir()
	pkg, err := DownloadAsset(context.Background(), srv.Client(), asset, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatalf("DownloadAsset（内置公钥）: %v", err)
	}
	if err := VerifyFile(pkg, asset.Size, sha); err != nil {
		t.Fatalf("下载文件校验失败：%v", err)
	}

	exe := filepath.Join(dir, "fdedup-ui.exe")
	if err := os.WriteFile(exe, []byte("old-running-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Replace(exe, pkg, asset.Size, asset.SHA256, func(string) error { return nil })
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if res.Size != int64(len(pkgData)) {
		t.Fatalf("Replace size=%d", res.Size)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != string(pkgData) {
		t.Fatalf("替换后内容不符")
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func manifestBytes(t *testing.T, baseURL string, pkg []byte, priv ed25519.PrivateKey) []byte {
	t.Helper()
	sum := sha256Hex(pkg)
	m := Manifest{
		SchemaVersion: SchemaVersion,
		Kind:          Kind,
		Version:       "0.8.1",
		Notes:         "e2e",
		Assets: []Asset{{
			Name:      "pkg.exe",
			OS:        "windows",
			Arch:      "amd64",
			URL:       baseURL + "/pkg.exe",
			Size:      int64(len(pkg)),
			SHA256:    sum,
			Signature: SignPackageDigest(priv, sum),
		}},
	}
	if err := SignManifest(&m, priv); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
