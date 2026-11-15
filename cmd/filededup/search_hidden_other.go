//go:build desktop && !windows

package main

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// searchHiddenFile 在非 Windows 平台按 "." 前缀判定隐藏。
func searchHiddenFile(path string, _ fs.FileInfo) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}
