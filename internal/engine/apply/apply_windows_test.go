//go:build windows

package apply_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"filededup/internal/engine/apply"
	"filededup/internal/engine/model"
	"filededup/internal/platform"
)

// lockExclusive 以共享模式 0 打开文件，模拟"被其他进程占用"。
func lockExclusive(path string) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
}

func closeHandle(h syscall.Handle) { syscall.CloseHandle(h) }

func winSnapshot(t *testing.T, path string) *model.Precheck {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Precheck{
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}

func winPlan(path string) *model.Plan {
	return &model.Plan{
		SchemaVersion: model.SchemaVersion,
		RequestID:     "win",
		Source:        model.PlanFromScan,
		Actions: []model.Action{{
			Seq:      1,
			Kind:     model.ActionDeletePermanent,
			Src:      path,
			Precheck: nil,
			Status:   model.StatusPlanned,
		}},
	}
}

// TestPermanentDeleteReadonlyFailsSafely 验证真实只读文件在永久删除时安全失败（Windows 上 os.Remove 拒绝只读文件）。
func TestPermanentDeleteReadonlyFailsSafely(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	p := winPlan(path)
	p.Actions[0].Precheck = winSnapshot(t, path)

	report, err := (&apply.Executor{}).Execute(context.Background(), p, apply.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 {
		t.Fatalf("只读文件永久删除应失败：%+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("失败后文件应保持原样：%v", err)
	}
	os.Chmod(path, 0o644)
}

// TestReadonlyThenClearThenDelete 验证"先 clear_readonly 再永久删除"的补救路径。
func TestReadonlyThenClearThenDelete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	p := winPlan(path)
	p.Actions = []model.Action{
		{Seq: 1, Kind: model.ActionClearReadonly, Src: path, Precheck: winSnapshot(t, path), Status: model.StatusPlanned},
		{Seq: 2, Kind: model.ActionDeletePermanent, Src: path, Precheck: winSnapshot(t, path), Status: model.StatusPlanned},
	}
	report, err := (&apply.Executor{}).Execute(context.Background(), p, apply.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 2 || report.Failed != 0 {
		t.Fatalf("补救路径应成功：%+v", report)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("文件应已删除：%v", err)
	}
}

// TestTrashLockedFileFailsSafely 验证被独占占用的文件无法移入回收站，且文件保持原样。
func TestTrashLockedFileFailsSafely(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	handle, err := lockExclusive(path)
	if err != nil {
		t.Skipf("无法建立独占句柄：%v", err)
	}
	defer closeHandle(handle)

	p := winPlan(path)
	p.Actions[0].Kind = model.ActionDeleteTrash
	p.Actions[0].Precheck = winSnapshot(t, path)

	report, err := (&apply.Executor{Trash: platform.Trash}).Execute(context.Background(), p, apply.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 {
		t.Fatalf("被占用文件应失败：%+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("失败后文件应保持原样：%v", err)
	}
}
