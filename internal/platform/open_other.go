//go:build !windows

package platform

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenPath 用系统默认程序打开文件或目录。
func OpenPath(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// RevealInExplorer 在文件管理器中定位文件；目录则直接打开。
func RevealInExplorer(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Start()
	}
	return OpenPath(filepath.Dir(path))
}
