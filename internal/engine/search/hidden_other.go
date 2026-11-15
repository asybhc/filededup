//go:build !windows

package search

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// platformHidden 在非 Windows 平台以 "." 前缀作为隐藏约定。
func platformHidden(path string, _ fs.FileInfo) bool {
	return strings.HasPrefix(filepath.Base(path), ".")
}
