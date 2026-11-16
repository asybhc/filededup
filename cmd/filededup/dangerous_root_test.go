//go:build desktop

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDangerousRootRejectsVolumeRoot 覆盖整盘根目录拒绝。
func TestDangerousRootRejectsVolumeRoot(t *testing.T) {
	for _, op := range []string{"扫描", "整理"} {
		if err := rejectDangerousRoot(`C:\`, op); err == nil {
			t.Fatalf("%s 应拒绝 C 盘根目录", op)
		}
		if err := rejectDangerousRoot("/", op); err == nil {
			t.Fatalf("%s 应拒绝 / 根目录", op)
		}
	}
	msg := rejectDangerousRoot(`C:\`, "扫描")
	if msg == nil || !strings.Contains(msg.Error(), "全盘查重") {
		t.Fatalf("扫描根目录拒绝应提示全盘查重: %v", msg)
	}
}

// TestDangerousRootRejectsSystemDirs 覆盖系统目录及其子目录拒绝。
func TestDangerousRootRejectsSystemDirs(t *testing.T) {
	paths := []string{
		`C:\Windows`,
		`C:\Windows\Temp`,
		`C:\Program Files\App`,
		`C:\Program Files (x86)\App`,
		`C:\ProgramData\App`,
		`C:\$Recycle.Bin`,
		`C:\System Volume Information`,
		`C:\Recovery`,
		`C:\Windows.old`,
		`D:\PerfLogs`,
		`C:\Windows\System32\drivers`,
	}
	for _, p := range paths {
		if err := rejectDangerousRoot(p, "扫描"); err == nil {
			t.Fatalf("应拒绝系统目录: %s", p)
		}
	}
}

// TestDangerousRootAllowsUserDirs 覆盖普通目录放行。
func TestDangerousRootAllowsUserDirs(t *testing.T) {
	tmp := t.TempDir()
	paths := []string{
		tmp,
		filepath.Join(tmp, "照片"),
		`C:\Users\me\Pictures`,
		`D:\资料\2024`,
	}
	for _, p := range paths {
		if err := rejectDangerousRoot(p, "扫描"); err != nil {
			t.Fatalf("普通目录不应拒绝: %s (%v)", p, err)
		}
	}
}
