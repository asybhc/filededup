//go:build !windows

package secret

import "errors"

// SupportsProtection 在非 Windows 平台返回 false：没有 DPAPI 就不落盘密钥。
func SupportsProtection() bool { return false }

func protect([]byte) ([]byte, error) {
	return nil, errors.New("当前平台不支持 DPAPI，AI 密钥无法加密保存")
}

func unprotect([]byte) ([]byte, error) {
	return nil, errors.New("当前平台不支持 DPAPI，无法读取 AI 密钥")
}
