//go:build !windows

package platform

import (
	"os"
	"unicode/utf16"
)

// IsReparse 在非 Windows 平台按符号链接判断。
func IsReparse(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// UTF16Len 返回路径的 UTF-16 码元数。
func UTF16Len(path string) int {
	return len(utf16.Encode([]rune(path)))
}
