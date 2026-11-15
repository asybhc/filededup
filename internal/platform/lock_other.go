//go:build !windows

package platform

import "errors"

// ErrLocked 与 Windows 实现保持一致；非 Windows 平台不会触发占用检测。
var ErrLocked = errors.New("platform: 文件被其它进程占用")

// LockHolder 描述占用文件的进程（非 Windows 平台为空实现）。
type LockHolder struct {
	PID             uint32
	Name            string
	Executable      string
	SessionID       uint32
	ApplicationType uint32
	HasWindow       bool
	Protected       bool
}

// IsLocked 在非 Windows 平台恒为 false。
func IsLocked(string) bool { return false }

// Locks 在非 Windows 平台返回空列表。
func Locks(string) ([]LockHolder, error) { return nil, nil }

// ReleaseLocks 在非 Windows 平台直接视为无需解除占用。
func ReleaseLocks(string, func(string, []LockHolder) bool) error { return nil }
