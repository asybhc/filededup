//go:build windows

package platform

import (
	"fmt"
	"syscall"
	"unsafe"
)

// 共享的 Win32 动态库句柄。只在本包内使用，避免每次调用都解析符号。
var (
	kernel32dll = syscall.NewLazyDLL("kernel32.dll")
	advapi32dll = syscall.NewLazyDLL("advapi32.dll")
	ole32dll    = syscall.NewLazyDLL("ole32.dll")
	shell32dll  = syscall.NewLazyDLL("shell32.dll")

	procCloseHandle             = kernel32dll.NewProc("CloseHandle")
	procGetVolumeInformationW   = kernel32dll.NewProc("GetVolumeInformationW")
	procCreateMutexW            = kernel32dll.NewProc("CreateMutexW")
	procRegOpenKeyExW           = advapi32dll.NewProc("RegOpenKeyExW")
	procRegQueryValueExW        = advapi32dll.NewProc("RegQueryValueExW")
	procRegCloseKey             = advapi32dll.NewProc("RegCloseKey")
	procCoInitializeEx          = ole32dll.NewProc("CoInitializeEx")
	procCoUninitialize          = ole32dll.NewProc("CoUninitialize")
	procCoCreateInstance        = ole32dll.NewProc("CoCreateInstance")
	procSHCreateItemFromParsing = shell32dll.NewProc("SHCreateItemFromParsingName")
	procSHFileOperationW        = shell32dll.NewProc("SHFileOperationW")
	procShellExecuteW           = shell32dll.NewProc("ShellExecuteW")
)

// comGUID 是 Windows GUID 的内存布局。
type comGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// comCallHR 按 vtable 下标调用 COM 方法（0 基，含 IUnknown 三个方法）。
func comCallHR(obj unsafe.Pointer, index int, args ...uintptr) uintptr {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, uintptr(index)*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, uintptr(obj))
	callArgs = append(callArgs, args...)
	r, _, _ := syscall.SyscallN(fn, callArgs...)
	return r
}

// comRelease 调用 IUnknown::Release。
func comRelease(obj unsafe.Pointer) {
	if obj == nil {
		return
	}
	comCallHR(obj, 2)
}

// hresultError 把失败的 HRESULT 转成可读错误。
func hresultError(what string, hr uintptr) error {
	return fmt.Errorf("platform: %s 失败（HRESULT 0x%08X）", what, uint32(hr))
}
