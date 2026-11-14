//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var procGetLongPathNameW = syscall.NewLazyDLL("kernel32.dll").NewProc("GetLongPathNameW")

// longPath 把 8.3 短路径展开为长路径，避免 C#/Go 两侧对同一目录写出不同形式的绝对路径。
func longPath(p string) string {
	if p == "" {
		return p
	}
	src, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 512)
	for {
		n, _, _ := procGetLongPathNameW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			return p
		}
		if int(n) <= len(buf) {
			return syscall.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n)
	}
}
