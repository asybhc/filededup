//go:build windows

package main

import (
	"syscall"
)

const (
	winAttrReadonly uint32 = 0x00000001
	winAttrHidden   uint32 = 0x00000002
	winAttrSystem   uint32 = 0x00000004
)

// setFileHidden 追加隐藏属性，用于生成 hidden 场景语料。
func setFileHidden(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil {
		return err
	}
	return syscall.SetFileAttributes(ptr, attrs|winAttrHidden)
}

// hiddenRel 在 Windows 上靠属性表达隐藏，文件名保持不变。
func hiddenRel(rel string) string { return rel }
