//go:build windows

package platform

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	hkeyLocalMachine = 0x80000002
	keyRead          = 0x20019
	keyWow6464       = 0x0100
	regSZ            = 1
)

// MachineIDParts 采集 Windows 机器码 v2 的原始项：
// 注册表 MachineGuid + 系统盘卷序列号。
func MachineIDParts() (MachineParts, error) {
	guid, err := machineGUID()
	if err != nil {
		return MachineParts{}, err
	}
	serial, err := systemVolumeSerial()
	if err != nil {
		return MachineParts{}, err
	}
	p := MachineParts{
		OS:          "windows",
		PrimaryID:   guid,
		SecondaryID: fmt.Sprintf("%08X", serial),
	}
	p.Hash = p.computeHash()
	return p, nil
}

// machineGUID 读取 HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid。
// 直接调用 advapi32，避免为一处读取引入 golang.org/x/sys 依赖。
func machineGUID() (string, error) {
	subkey, err := syscall.UTF16PtrFromString(`SOFTWARE\Microsoft\Cryptography`)
	if err != nil {
		return "", err
	}
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		hkeyLocalMachine,
		uintptr(unsafe.Pointer(subkey)),
		0,
		keyRead|keyWow6464,
		uintptr(unsafe.Pointer(&h)),
	)
	if int32(r) != 0 {
		return "", fmt.Errorf("platform: 打开注册表 MachineGuid 失败（%d）", int32(r))
	}
	defer procRegCloseKey.Call(uintptr(h))

	name, err := syscall.UTF16PtrFromString("MachineGuid")
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 256)
	size := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if int32(r) != 0 {
		return "", fmt.Errorf("platform: 读取注册表 MachineGuid 失败（%d）", int32(r))
	}
	if typ != regSZ {
		return "", fmt.Errorf("platform: MachineGuid 类型异常（%d）", typ)
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf)), nil
}

// systemVolumeSerial 返回系统盘卷序列号（原版用 GetVolumeInformationW 取）。
func systemVolumeSerial() (uint32, error) {
	root := os.Getenv("SystemDrive")
	if root == "" {
		root = "C:"
	}
	root += `\`
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, err
	}
	var serial, maxComponent, flags uint32
	r, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		0,
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComponent)),
		uintptr(unsafe.Pointer(&flags)),
		0,
		0,
	)
	if r == 0 {
		return 0, fmt.Errorf("platform: 读取系统盘卷序列号失败：%v", root)
	}
	return serial, nil
}
