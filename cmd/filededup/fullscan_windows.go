//go:build desktop && windows

package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procGetLogicalDrives = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetLogicalDrives")
	procGetDriveTypeW    = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDriveTypeW")
)

// driveFixed 是 GetDriveTypeW 的 DRIVE_FIXED：本地固定磁盘。
const driveFixed = 3

// listFixedVolumes 枚举本地固定磁盘（排除 U 盘、光驱、网络映射盘与内存盘）。
func listFixedVolumes() ([]string, error) {
	mask, _, callErr := procGetLogicalDrives.Call()
	if mask == 0 {
		if callErr != nil {
			return nil, callErr
		}
		return nil, errors.New("无法枚举磁盘驱动器")
	}
	bits := uint32(mask)
	volumes := make([]string, 0, 8)
	for i := 0; i < 26; i++ {
		if bits&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + ":\\"
		p, err := windows.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p)))
		if t != driveFixed {
			continue
		}
		volumes = append(volumes, root)
	}
	if len(volumes) == 0 {
		return nil, errors.New("没有找到可用的本地固定磁盘")
	}
	return volumes, nil
}
