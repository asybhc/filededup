package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"filededup/internal/engine/organize"
)

// orgManifest 返回排序后的 "相对路径|大小|sha256|只读" 清单，用于确定性断言。
func orgManifest(t *testing.T, root string) []string {
	t.Helper()
	tree, err := collectOrgTree(root)
	if err != nil {
		t.Fatalf("枚举 %s 失败：%v", root, err)
	}
	out := make([]string, 0, len(tree.Files))
	for _, f := range tree.Files {
		out = append(out, f.Path+"|"+strconv.FormatInt(f.Size, 10)+"|"+f.SHA256+"|"+boolStr(f.Readonly))
	}
	sort.Strings(out)
	return out
}

// makeTreeWritable 在清理前去掉只读位，避免 Windows 上 TempDir 清理失败。
func makeTreeWritable(root string) {
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			_ = os.Chmod(path, 0o644)
		}
		return nil
	})
}

// TestOrgScenesDeterministic：同一场景两次生成，文件清单、大小、sha256、只读位完全一致。
func TestOrgScenesDeterministic(t *testing.T) {
	for _, sc := range orgScenes {
		sc := sc
		t.Run(sc.id, func(t *testing.T) {
			a := t.TempDir()
			b := t.TempDir()
			defer makeTreeWritable(a)
			defer makeTreeWritable(b)
			fa, err := sc.gen(a)
			if err != nil {
				t.Fatalf("第一次生成失败：%v", err)
			}
			fb, err := sc.gen(b)
			if err != nil {
				t.Fatalf("第二次生成失败：%v", err)
			}
			if !sameOrgStrings(fa, fb) {
				t.Fatalf("文件清单不一致：%v vs %v", fa, fb)
			}
			if len(fa) == 0 {
				t.Fatal("场景未生成任何文件")
			}
			ma, mb := orgManifest(t, a), orgManifest(t, b)
			if !sameOrgStrings(ma, mb) {
				t.Fatalf("生成内容不确定：\n%s\n%s", strings.Join(ma, "\n"), strings.Join(mb, "\n"))
			}
		})
	}
}

// TestOrgCompareClassification：改名/删除/新增/只读位/counters/dirs 的分类与一致率。
func TestOrgCompareClassification(t *testing.T) {
	base := &orgDump{
		SchemaVersion: 1,
		Root:          `F:\base`,
		Counters:      organize.Counter{Renamed: 1, Moved: 3},
		Dirs:          []string{"sub"},
		Files: []orgDumpFile{
			{Path: "a.txt", Size: 10, SHA256: "aaa"},
			{Path: "b.txt", Size: 20, SHA256: "bbb"},
			{Path: "c.txt", Size: 30, SHA256: "ccc", Readonly: true},
			{Path: "gone.txt", Size: 5, SHA256: "ddd"},
		},
	}
	cand := &orgDump{
		SchemaVersion: 1,
		Root:          `G:\cand`,
		Counters:      organize.Counter{Renamed: 2, Moved: 3},
		Dirs:          []string{"sub", "extra"},
		Files: []orgDumpFile{
			{Path: "a.txt", Size: 10, SHA256: "aaa"},
			{Path: "b_renamed.txt", Size: 20, SHA256: "bbb"},
			{Path: "c.txt", Size: 30, SHA256: "ccc"},
			{Path: "new.txt", Size: 7, SHA256: "eee"},
		},
	}

	out := compareOrgDumps(base, cand, 0)
	if out.Result != "DIFF" {
		t.Fatalf("应 DIFF，实际 %s", out.Result)
	}
	if len(out.Renamed) != 1 || out.Renamed[0].Base != "b.txt" || out.Renamed[0].Cand != "b_renamed.txt" {
		t.Fatalf("改名分类错误：%+v", out.Renamed)
	}
	if len(out.Deleted) != 1 || out.Deleted[0] != "gone.txt" {
		t.Fatalf("删除分类错误：%+v", out.Deleted)
	}
	if len(out.Added) != 1 || out.Added[0] != "new.txt" {
		t.Fatalf("新增分类错误：%+v", out.Added)
	}
	if len(out.Unpaired) != 0 {
		t.Fatalf("不应有无法配对项：%+v", out.Unpaired)
	}
	// 路径维度：a.txt 与 c.txt 匹配；并集 6 条。
	if out.Files.Matched != 2 || out.Files.Union != 6 {
		t.Fatalf("文件一致率计算错误：matched=%d union=%d", out.Files.Matched, out.Files.Union)
	}
	if got, want := out.Files.Rate, 2.0/6.0; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("文件一致率=%v，期望 %v", got, want)
	}
	if out.Counters.Matched != 13 || out.Counters.Total != 14 || out.Counters.Match {
		t.Fatalf("counters 统计错误：%+v", out.Counters)
	}
	if out.Dirs.Matched != 1 || out.Dirs.Union != 2 {
		t.Fatalf("dirs 一致率错误：%+v", out.Dirs)
	}

	wantFields := map[string]bool{
		"counters.renamed":      false,
		"files[c.txt].readonly": false,
		"dirs[extra]":           false,
		"rename[b.txt]":         false,
		"files[gone.txt]":       false,
		"files[new.txt]":        false,
	}
	for _, d := range out.Diffs {
		if _, ok := wantFields[d.Field]; ok {
			wantFields[d.Field] = true
		}
	}
	for f, seen := range wantFields {
		if !seen {
			t.Fatalf("缺少差异 %s：%v", f, out.Diffs)
		}
	}

	// 同一份数据、不同绝对根前缀必须 MATCH。
	same := *base
	same.Root = `Z:\other\place`
	if res := compareOrgDumps(base, &same, 0); res.Result != "MATCH" {
		t.Fatalf("绝对前缀不同应 MATCH：%v", res.Diffs)
	}

	// 无法配对：哈希缺省时不能判定改名/删除。
	noHash := &orgDump{Files: []orgDumpFile{{Path: "x.txt", Size: 1}}}
	empty := &orgDump{Files: []orgDumpFile{}}
	if res := compareOrgDumps(noHash, empty, 0); len(res.Unpaired) != 1 || len(res.Deleted) != 0 {
		t.Fatalf("无法配对分类错误：unpaired=%v deleted=%v", res.Unpaired, res.Deleted)
	}
}

// TestCollectOrgTreeSkipsReparse：重解析点（目录联接/符号链接）不枚举、不跟随、不计数。
func TestCollectOrgTreeSkipsReparse(t *testing.T) {
	root := t.TempDir()
	defer makeTreeWritable(root)
	outside := t.TempDir()
	defer makeTreeWritable(outside)

	plain := map[string]string{
		"real.txt":      "real-payload",
		"sub/inner.txt": "inner-payload",
	}
	for rel, data := range plain {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "target.txt"), []byte("target-payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(root, "link")
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", linkPath, outside).CombinedOutput(); err != nil {
			t.Skipf("环境无法创建目录联接，跳过：%v %s", err, out)
		}
	} else {
		if err := os.Symlink(outside, linkPath); err != nil {
			t.Skipf("环境无法创建目录符号链接，跳过：%v", err)
		}
	}

	tree, err := collectOrgTree(root)
	if err != nil {
		t.Fatalf("枚举失败：%v", err)
	}
	for _, f := range tree.Files {
		if f.Path == "link" || strings.HasPrefix(f.Path, "link/") {
			t.Fatalf("不应跟进重解析点目录内的文件：%s", f.Path)
		}
	}
	for _, d := range tree.Dirs {
		if d == "link" {
			t.Fatalf("重解析点目录不应计入 Dirs：%v", tree.Dirs)
		}
	}

	paths := map[string]bool{}
	for _, f := range tree.Files {
		paths[f.Path] = true
	}
	for rel := range plain {
		if !paths[rel] {
			t.Fatalf("普通文件被误伤，缺少 %s（实际 %v）", rel, paths)
		}
	}
}

// TestOrgSnapshotBuild：枚举 3 个文件（含只读位）与子目录，构造 Snapshot 输入。
func TestOrgSnapshotBuild(t *testing.T) {
	root := t.TempDir()
	defer makeTreeWritable(root)
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"a.txt":      "alpha",
		"sub/b.bin":  "beta-payload",
		"sub/ro.txt": "readonly-payload",
	}
	for rel, data := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "sub", "ro.txt"), 0o444); err != nil {
		t.Fatal(err)
	}

	tree, err := collectOrgTree(root)
	if err != nil {
		t.Fatalf("枚举失败：%v", err)
	}
	if len(tree.Entries) != 3 {
		t.Fatalf("文件数=%d，期望 3", len(tree.Entries))
	}
	if len(tree.Dirs) != 1 || tree.Dirs[0] != "sub" {
		t.Fatalf("目录清单错误：%v", tree.Dirs)
	}
	paths := map[string]bool{}
	for _, e := range tree.Entries {
		paths[e.Path] = true
		want, ok := files[e.Path]
		if !ok {
			t.Fatalf("意外路径 %s", e.Path)
		}
		if e.Size != int64(len(want)) {
			t.Fatalf("%s 大小=%d，期望 %d", e.Path, e.Size, len(want))
		}
		sum := sha256.Sum256([]byte(want))
		if e.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("%s 哈希不符：%s", e.Path, e.SHA256)
		}
		if e.Path == "sub/ro.txt" {
			if !e.Readonly {
				t.Fatal("ro.txt 应识别为只读")
			}
		} else if e.Readonly {
			t.Fatalf("%s 不应为只读", e.Path)
		}
	}
	for rel := range files {
		if !paths[rel] {
			t.Fatalf("缺少文件 %s", rel)
		}
	}

	cfg, err := buildOrgConfig(defaultOrgOptions(), root)
	if err != nil {
		t.Fatalf("构造配置失败：%v", err)
	}
	providers := prepareOrgProviders(tree.Entries, root, cfg)
	if h, ok := providers.ContentHash(tree.Entries[0]); !ok || h == "" {
		t.Fatalf("内容哈希 Provider 应返回已有 SHA-256：%q %v", h, ok)
	}
	snap := buildOrgSnapshot(root, "", tree)
	if snap.Root != root || len(snap.Files) != 3 || len(snap.Dirs) != 1 || snap.Target != "" {
		t.Fatalf("Snapshot 构造错误：%+v", snap)
	}

	// 只读文件不应阻塞规划器。
	if res := organize.Plan(cfg, snap, providers); res.Counters.Readonly != 1 {
		t.Fatalf("readonly 计数=%d，期望 1", res.Counters.Readonly)
	}
}
