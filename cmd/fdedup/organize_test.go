package main

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/engine/organize"
)

func TestOrganizeTarget(t *testing.T) {
	root := t.TempDir()
	spec, err := organizeTarget(root, "")
	if err != nil || spec.Rel != "" || spec.External {
		t.Fatalf("空 target 应返回空且无错，得到 %+v / %v", spec, err)
	}
	spec, err = organizeTarget(root, "sub/dir")
	if err != nil || spec.Rel != "sub/dir" || spec.External {
		t.Fatalf("相对 target 解析失败：%+v / %v", spec, err)
	}
	abs := filepath.Join(root, "inner")
	spec, err = organizeTarget(root, abs)
	if err != nil || spec.Rel != "inner" || spec.External {
		t.Fatalf("根内绝对 target 解析失败：%+v / %v", spec, err)
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	spec, err = organizeTarget(root, outside)
	if err != nil || !spec.External || spec.Abs != filepath.ToSlash(filepath.Clean(outside)) {
		t.Fatalf("根外绝对 target 解析失败：%+v / %v", spec, err)
	}
	for _, bad := range []string{".", "..", root} {
		if _, err := organizeTarget(root, bad); err == nil {
			t.Fatalf("target %q 应被拒绝", bad)
		}
	}
}

// TestRunOrganizeExternalTargetDryRun 根外目标在试运行下可规划，并按既有语义创建目标目录。
func TestRunOrganizeExternalTargetDryRun(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), "x")
	out := filepath.Join(t.TempDir(), "external")
	code := runOrganize([]string{
		"--root", root, "--target", out, "--dry-run", "--steps", "organize",
	})
	if code != 0 {
		t.Fatalf("organize 退出码=%d", code)
	}
	if info, err := os.Stat(out); err != nil || !info.IsDir() {
		t.Fatalf("根外目标目录应被创建：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("试运行不应移动源文件：%v", err)
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
