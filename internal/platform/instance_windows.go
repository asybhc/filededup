//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

// instanceMutexName 是单实例互斥体名（Local\ 前缀：同一登录会话内唯一）。
const instanceMutexName = `Local\FileDedupTool.SingleInstance`

const errorAlreadyExists = 183

// AcquireInstance 尝试获取单实例互斥体。
// alreadyRunning=true 表示已有实例在运行，此时 release 为 no-op。
func AcquireInstance() (release func(), alreadyRunning bool, err error) {
	name, err := syscall.UTF16PtrFromString(instanceMutexName)
	if err != nil {
		return nil, false, err
	}
	r, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if r == 0 {
		return nil, false, lastErr
	}
	if errno, ok := lastErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		procCloseHandle.Call(r)
		return func() {}, true, nil
	}
	return func() { procCloseHandle.Call(r) }, false, nil
}
