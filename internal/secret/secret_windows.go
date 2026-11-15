//go:build windows

package secret

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// SupportsProtection 报告当前平台是否支持 DPAPI 加密存储。
func SupportsProtection() bool { return true }

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
	enc := make([]byte, out.Size)
	copy(enc, unsafe.Slice(out.Data, out.Size))
	return enc, nil
}

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
	plain := make([]byte, out.Size)
	copy(plain, unsafe.Slice(out.Data, out.Size))
	return plain, nil
}
