//go:build desktop && windows

package main

import (
	"io/fs"
	"syscall"
)

// searchHiddenFile 用 Windows 文件属性判断隐藏/系统文件；属性取自目录枚举结果。
func searchHiddenFile(_ string, info fs.FileInfo) bool {
	if info == nil {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return true
	}
	const mask = syscall.FILE_ATTRIBUTE_HIDDEN | syscall.FILE_ATTRIBUTE_SYSTEM
	return data.FileAttributes&mask != 0
}
