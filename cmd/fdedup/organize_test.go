package main

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/engine/organize"
)

func TestOrganizeTarget(t *testing.T) {
	root := t.TempDir()
	if got, err := organizeTarget(root, ""); err != nil || got != "" {
		t.Fatalf("空 target 应返回空且无错，得到 %q / %v", got, err)
	}
	if got, err := organizeTarget(root, "sub/dir"); err != nil || got != "sub/dir" {
		t.Fatalf("相对 target 解析失败：%q / %v", got, err)
	}
	abs := filepath.Join(root, "inner")
	if got, err := organizeTarget(root, abs); err != nil || got != "inner" {
		t.Fatalf("根内绝对 target 解析失败：%q / %v", got, err)
	}
	for _, bad := range []string{".", "..", filepath.Join(root, "..", "out")} {
		if _, err := organizeTarget(root, bad); err == nil {
			t.Fatalf("target %q 应被拒绝", bad)
		}
	}
}

func TestCheckPreviewRefs(t *testing.T) {
	entries := []organize.FileEntry{{Path: "a.txt"}, {Path: "b.txt"}}
	ok := organize.PreviewPlan{Entries: []organize.PreviewEntry{{From: "a.txt", Keep: "b.txt"}}}
	if err := checkPreviewRefs(ok, entries); err != nil {
		t.Fatalf("引用存在时不应报错：%v", err)
	}
	bad := organize.PreviewPlan{Entries: []organize.PreviewEntry{{From: "a.txt", Keep: "gone.txt"}}}
	if err := checkPreviewRefs(bad, entries); err == nil {
		t.Fatal("引用不存在的保留项应报错")
	}
}

func TestRunOrganizeDedupAndOrganize(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "same-content")
	mustWrite(t, filepath.Join(root, "sub", "b.txt"), "same-content")
	mustWrite(t, filepath.Join(root, "note.log"), "beta")

	code := runOrganize([]string{
		"--root", root, "--target", "sorted", "--yes",
		"--steps", "content,organize", "--conflict", "rename", "--delete", "permanent",
	})
	if code != 0 {
		t.Fatalf("organize 退出码=%d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "b.txt")); !os.IsNotExist(err) {
		t.Fatalf("重复文件应被删除：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("保留文件应被移入整理目录：%v", err)
	}
	found := false
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "a.txt" {
			found = filepath.Base(filepath.Dir(p)) == "文档"
		}
		return nil
	})
	if !found {
		t.Fatal("a.txt 未进入 sorted/文档")
	}
}

func TestRunOrganizeDryRunDoesNotTouchFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "001-a.txt"), "x")
	code := runOrganize([]string{
		"--root", root, "--dry-run", "--steps", "prefix", "--conflict", "rename",
	})
	if code != 0 {
		t.Fatalf("dry-run 退出码=%d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "001-a.txt")); err != nil {
		t.Fatalf("试运行不应改名：%v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
