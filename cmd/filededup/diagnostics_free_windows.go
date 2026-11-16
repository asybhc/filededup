//go:build desktop && windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetDiskFreeSpaceExW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// diskFreeImpl 调用 GetDiskFreeSpaceExW 取路径所在卷的可用空间。
func diskFreeImpl(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var free, total, totalFree uint64
	r, _, callErr := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&free)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, callErr
	}
	return int64(free), nil
}
