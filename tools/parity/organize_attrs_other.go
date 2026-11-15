//go:build !windows

package main

import (
	"os"
	"path"
	"strings"
)

// fileFlags 在非 Windows 平台用权限位与 "." 前缀近似只读/隐藏。
func fileFlags(p string) (readonly, hidden, system bool) {
	info, err := os.Stat(p)
	if err != nil {
		return false, false, false
	}
	return info.Mode().Perm()&0o200 == 0, strings.HasPrefix(path.Base(strings.ReplaceAll(p, `\`, "/")), "."), false
}

// setFileHidden 在非 Windows 平台无需额外属性。
func setFileHidden(string) error { return nil }

// hiddenRel 在非 Windows 平台用 "." 前缀表达隐藏。
func hiddenRel(rel string) string {
	dir, base := "", rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		dir, base = rel[:i+1], rel[i+1:]
	}
	return dir + "." + base
}
