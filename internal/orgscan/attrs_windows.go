//go:build windows

package orgscan

import (
	"os"
	"syscall"
)

const (
	winAttrReadonly uint32 = 0x00000001
	winAttrHidden   uint32 = 0x00000002
	winAttrSystem   uint32 = 0x00000004
)

// FileFlags 读取 Windows 文件属性；调用失败时退回权限位判断只读。
func FileFlags(path string) (readonly, hidden, system bool) {
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
