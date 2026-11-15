//go:build !windows

package main

import (
	"strings"
)

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
