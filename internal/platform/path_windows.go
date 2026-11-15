//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"syscall"
)

const fileAttributeReparsePoint = 0x00000400

// longPath 给绝对路径加 \\?\ / \\?\UNC\ 前缀，等价于原版 a.a.A()。
// 原版所有文件操作都先经过这一步；Go 的 os 包对多数操作已自动处理长路径，
// 这里主要用于直接调用 Win32 API 的场景（占用检测、属性读取）。
func longPath(path string) string {
	if path == "" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if strings.HasPrefix(abs, `\\?\`) {
		return abs
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(abs, `\\`)
	}
	if len(abs) >= 2 && abs[1] == ':' {
		return `\\?\` + abs
	}
	return abs
}

// IsReparse 判断路径是否为重解析点（符号链接、目录联接等）。
// 对齐原版 a.a.D()：读取 FileAttributes 的 ReparsePoint 位；读取失败按 false 处理。
func IsReparse(path string) bool {
	ptr, err := syscall.UTF16PtrFromString(longPath(path))
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil || attrs == 0xFFFFFFFF {
		return false
	}
	return attrs&fileAttributeReparsePoint != 0
}

// UTF16Len 返回路径的 UTF-16 码元数，用于长路径上限判断。
func UTF16Len(path string) int {
	n := 0
	for _, r := range path {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
