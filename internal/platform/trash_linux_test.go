//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTrashLinuxXDG 验证同卷时按 XDG 规范移入 Trash，并写入 .trashinfo。
func TestTrashLinuxXDG(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("写入待删除文件失败: %v", err)
	}
	if err := Trash(src); err != nil {
		t.Fatalf("Trash 失败: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("源文件仍然存在: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dataHome, "Trash", "files", "note*"))
	if err != nil || len(files) != 1 {
		t.Fatalf("回收站文件 = %v, err=%v", files, err)
	}
	infos, err := filepath.Glob(filepath.Join(dataHome, "Trash", "info", "note*.trashinfo"))
	if err != nil || len(infos) != 1 {
		t.Fatalf(".trashinfo = %v, err=%v", infos, err)
	}
	raw, err := os.ReadFile(infos[0])
	if err != nil {
		t.Fatalf("读取 .trashinfo 失败: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "Path=") || !strings.Contains(body, "DeletionDate=") {
		t.Fatalf(".trashinfo 内容不完整: %q", body)
	}
}

// TestTrashLinuxUniqueName 验证同名文件不会互相覆盖。
func TestTrashLinuxUniqueName(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := os.MkdirAll(filepath.Join(dataHome, "Trash", "files"), 0o700); err != nil {
		t.Fatalf("创建回收站目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataHome, "Trash", "info"), 0o700); err != nil {
		t.Fatalf("创建回收站信息目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataHome, "Trash", "files", "note.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("写入已存在文件失败: %v", err)
	}
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatalf("写入待删除文件失败: %v", err)
	}
	if err := Trash(src); err != nil {
		t.Fatalf("Trash 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "Trash", "files", "note.1.txt")); err != nil {
		t.Fatalf("同名文件未生成 note.1.txt: %v", err)
	}
	old, err := os.ReadFile(filepath.Join(dataHome, "Trash", "files", "note.txt"))
	if err != nil || string(old) != "old" {
		t.Fatalf("已存在的回收站文件被覆盖: %q, err=%v", old, err)
	}
}
