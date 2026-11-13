//go:build !windows

package main

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// hiddenFile 在非 Windows 平台按 "." 前缀判定隐藏。
func hiddenFile(path string, _ fs.FileInfo) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}
