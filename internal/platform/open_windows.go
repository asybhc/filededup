//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const swShowNormal = 1

// OpenPath 用系统默认关联程序打开文件或目录（等价 C# Process.Start / ShellExecute）。
func OpenPath(path string) error {
	return shellExecute("open", path)
}

// RevealInExplorer 在资源管理器中定位文件；目录则直接打开。
func RevealInExplorer(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return OpenPath(abs)
	}
	cmd := exec.Command("explorer.exe", "/select,"+abs)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func shellExecute(verb, file string) error {
	v, err := syscall.UTF16PtrFromString(verb)
	if err != nil {
		return err
	}
	f, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	r, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(v)),
		uintptr(unsafe.Pointer(f)),
		0,
		0,
		swShowNormal,
	)
	// ShellExecuteW 的返回值 <= 32 表示失败。
	if r <= 32 {
		return fmt.Errorf("platform: ShellExecuteW 打开 %q 失败（%d）", file, int32(r))
	}
	return nil
}
