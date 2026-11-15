//go:build !windows

package lanzou

// SupportsProtection 报告当前平台是否支持 DPAPI 凭据加密。
func SupportsProtection() bool { return false }

// protect / unprotect 在非 Windows 平台不可用：没有 DPAPI，就不落盘凭据，
// 登录态只留在内存里（由调用方决定）。
func protect([]byte) ([]byte, error) { return nil, ErrDPAPIUnsupported }

func unprotect([]byte) ([]byte, error) { return nil, ErrDPAPIUnsupported }
