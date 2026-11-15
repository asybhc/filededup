//go:build windows

package search

import "io/fs"

// platformHidden 是未注入 HiddenFile 时的默认判定。与查重引擎一致：Windows 不看属性，
// 一律 false；桌面门面会注入基于 Win32 属性的实现。
func platformHidden(string, fs.FileInfo) bool { return false }
