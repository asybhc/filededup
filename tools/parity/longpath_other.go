//go:build !windows

package main

// longPath 在非 Windows 平台无需展开短路径。
func longPath(p string) string { return p }
