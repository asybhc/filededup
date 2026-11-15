//go:build windows

package lanzou

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// SupportsProtection 报告当前平台是否支持 DPAPI 凭据加密。
func SupportsProtection() bool { return true }

// protect 用 DPAPI（CurrentUser）加密，算法与 C# 版 ProtectedData.Protect 一致，
// 因此两边可以互相读取凭据文件。
func protect(plain []byte) ([]byte, error) {
	if len(plain) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(plain)), Data: &plain[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return unsafe.Slice(out.Data, out.Size), nil
}

// unprotect 用 DPAPI（CurrentUser）解密。
func unprotect(cipher []byte) ([]byte, error) {
	if len(cipher) == 0 {
		return nil, nil
	}
	in := windows.DataBlob{Size: uint32(len(cipher)), Data: &cipher[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return unsafe.Slice(out.Data, out.Size), nil
}
