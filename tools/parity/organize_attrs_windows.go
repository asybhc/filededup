//go:build windows

package main

import (
	"os"
	"syscall"
)

const (
	winAttrReadonly uint32 = 0x00000001
	winAttrHidden   uint32 = 0x00000002
	winAttrSystem   uint32 = 0x00000004
)

// fileFlags 读取 Windows 文件属性；调用失败时退回权限位判断只读。
func fileFlags(path string) (readonly, hidden, system bool) {
	if ptr, err := syscall.UTF16PtrFromString(path); err == nil {
		if attrs, aerr := syscall.GetFileAttributes(ptr); aerr == nil && attrs != 0xFFFFFFFF {
			return attrs&winAttrReadonly != 0, attrs&winAttrHidden != 0, attrs&winAttrSystem != 0
		}
	}
	if info, ierr := os.Stat(path); ierr == nil {
		return info.Mode().Perm()&0o200 == 0, false, false
	}
	return false, false, false
}

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
