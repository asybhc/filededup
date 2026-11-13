package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goldenPathList 是 seed=20260113 的路径清单哈希。
// 该值跨平台必须一致（路径全部为 NFC + "/" 分隔），改动语料时必须同步更新。
const goldenPathList = "6ffaff777d4cc947e561fb061eb5f7fb0454ff8d73c1b1d7bab0bfc29c380273"

// goldenFileSet 是内容清单哈希；仅在硬链接/符号链接未被平台降级时有效。
const goldenFileSet = "f93c27ba8b41c9028bf2376db9fd2b91ae70b48256ff11c9f707b49de0c4e6d0"

func generate(t *testing.T, seed int64) *Manifest {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "corpus")
	m, err := Generate(dir, seed)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return m
}

// 同一 seed 两次生成，两个哈希与条目数量必须完全一致。
func TestGenerateDeterministic(t *testing.T) {
	a := generate(t, 20260113)
	b := generate(t, 20260113)
	if a.PathListSHA256 != b.PathListSHA256 {
		t.Fatalf("PathListSHA256 不可复现: %s != %s", a.PathListSHA256, b.PathListSHA256)
	}
	if a.FileSetSHA256 != b.FileSetSHA256 {
		t.Fatalf("FileSetSHA256 不可复现: %s != %s", a.FileSetSHA256, b.FileSetSHA256)
	}
	if len(a.Entries) != len(b.Entries) {
		t.Fatalf("条目数不一致: %d != %d", len(a.Entries), len(b.Entries))
	}
}

// 路径清单哈希锁定语料结构，防止无意改动破坏已有对拍基线。
func TestGoldenPathList(t *testing.T) {
	m := generate(t, 20260113)
	if m.PathListSHA256 != goldenPathList {
		t.Fatalf("路径清单哈希变化:\n got %s\nwant %s\n（若为有意修改语料，请同步更新 goldenPathList 与 goldenFileSet）",
			m.PathListSHA256, goldenPathList)
	}
	if degraded(m) {
		t.Logf("平台降级导致内容清单可变，跳过 FileSetSHA256 golden 断言: %v", m.Notes)
		return
	}
	if m.FileSetSHA256 != goldenFileSet {
		t.Fatalf("内容清单哈希变化:\n got %s\nwant %s", m.FileSetSHA256, goldenFileSet)
	}
}

func degraded(m *Manifest) bool {
	for _, e := range m.Entries {
		if e.Note == "symlink_unsupported" {
			return true
		}
	}
	for _, n := range m.Notes {
		if strings.Contains(n, "降级") {
			return true
		}
	}
	return false
}

// 关键场景必须真实存在，否则后续里程碑的对拍会静默失去覆盖。
func TestRequiredPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "corpus")
	if _, err := Generate(dir, 20260113); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	required := []string{
		"01-exact/a.txt",
		"01-exact/c.txt",
		"01-exact/empty-1.bin",
		"01-exact/medium-2.bin",
		"01-exact/no-ext-a",
		"02-unicode/报告 2024 最终版.txt",
		"02-unicode/emoji_😀_🙂.txt",
		"02-unicode/space dir/with space.txt",
		"03-rename/2024-01-02_c.txt",
		"04-images/solid-red-64.png",
		"04-images/alpha-64.png",
		"04-images/exif-orient6.jpg",
		"04-images/corrupt.png",
		"05-audio/tone-440-1s.wav",
		"05-audio/vbr-10s.mp3",
		"05-audio/cbr-10s-id3.mp3",
		"05-audio/zero.mp3",
		"06-links/hardlink-alias.txt",
		"07-hidden/.hidden-dot.txt",
		"08-long-path/short.txt",
		"09-readonly/readonly.txt",
		"10-organize/src/dup-name.txt",
		"10-organize/target/dup-name.txt",
		"11-special-delete/garbage/123456.txt",
		"11-special-delete/protected/IMG_20240101_120000.jpg",
		"12-filter/small.txt",
		"13-ext/archive.tar.gz",
	}
	for _, rel := range required {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("缺少必需路径 %s: %v", rel, err)
		}
	}
	// 长路径目录链
	longRel := "08-long-path"
	for i := 0; i < 6; i++ {
		longRel += "/" + strings.Repeat("d", 48)
	}
	if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(longRel), "long.txt")); err != nil {
		t.Errorf("长路径文件缺失: %v", err)
	}
	// 空目录
	for _, rel := range []string{"10-organize/empty-dir", "10-organize/nested/empty-b", "10-organize/nested/leaf-only/deeper"} {
		ents, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("空目录缺失 %s: %v", rel, err)
			continue
		}
		if len(ents) != 0 {
			t.Errorf("目录 %s 应为空，实际 %d 项", rel, len(ents))
		}
	}
}

// 硬链接必须真的共享 inode，否则 06-links 场景失去意义。
func TestHardlinkSharesInode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "corpus")
	m, err := Generate(dir, 20260113)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var alias *Entry
	for i := range m.Entries {
		if m.Entries[i].Path == "06-links/hardlink-alias.txt" {
			alias = &m.Entries[i]
		}
	}
	if alias == nil {
		t.Fatal("清单中缺少 hardlink-alias.txt")
	}
	if alias.Kind != "hardlink" {
		t.Skipf("当前平台不支持硬链接，已降级为 %s", alias.Kind)
	}
	a, err := os.Stat(filepath.Join(dir, "06-links", "hardlink-alias.txt"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(filepath.Join(dir, "06-links", "hardlink-target.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatal("hardlink-alias.txt 与目标不是同一个 inode")
	}
}

// 非空且无标记文件的目录必须拒绝覆盖，避免误删用户数据。
func TestRefuseForeignNonEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "userdata")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Generate(dir, 20260113); err == nil {
		t.Fatal("应拒绝覆盖无标记文件的非空目录")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("用户文件被删除: %v", err)
	}
}

// 重复生成到同一目录（有标记文件）应成功，且结果保持一致。
func TestRegenerateOwnDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "corpus")
	first, err := Generate(dir, 20260113)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate(dir, 20260113)
	if err != nil {
		t.Fatalf("重复生成失败: %v", err)
	}
	if first.PathListSHA256 != second.PathListSHA256 {
		t.Fatalf("重复生成结果不一致: %s != %s", first.PathListSHA256, second.PathListSHA256)
	}
}

// 不同 seed 必须改变随机内容（证明 seed 真的生效）。
func TestSeedAffectsContent(t *testing.T) {
	a := generate(t, 20260113)
	b := generate(t, 20260114)
	if a.FileSetSHA256 == b.FileSetSHA256 {
		t.Fatal("不同 seed 产生了相同内容，seed 未生效")
	}
	if a.PathListSHA256 != b.PathListSHA256 {
		t.Fatal("seed 不应影响路径清单")
	}
}
