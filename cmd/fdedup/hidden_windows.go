//go:build windows

package main

import (
	"io/fs"
	"syscall"
)

// hiddenFile 使用 Windows 文件属性判断 Hidden/System，与 C# 基线一致。
// 属性直接取自目录枚举得到的 FileInfo（syscall.Win32FileAttributeData），
// 每个文件只做一次系统调用；读不到属性时按隐藏处理（C# A/Y.cs 的 catch 分支）。
func hiddenFile(_ string, info fs.FileInfo) bool {
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
