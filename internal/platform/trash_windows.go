//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// COM 常量与 IID/CLSID（与原版 A\A.cs#L325-L356 使用的 Shell API 等价）。
var (
	clsidFileOperation = comGUID{0x3AD05575, 0x8857, 0x4850, [8]byte{0x92, 0x77, 0x11, 0xB8, 0x5B, 0xDB, 0x8E, 0x09}}
	iidIFileOperation  = comGUID{0x947AAB5F, 0x0A5C, 0x4C13, [8]byte{0xB4, 0xD6, 0x4B, 0xF7, 0x83, 0x6F, 0xC9, 0xF8}}
	iidIShellItem      = comGUID{0x43826D1E, 0xE718, 0x42EE, [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1

	// IFileOperation::SetOperationFlags
	fofSilent           = 0x0004
	fofNoConfirmation   = 0x0010
	fofAllowUndo        = 0x0040
	fofNoErrorUI        = 0x0400
	fofxRecycleOnDelete = 0x00080000

	// SHFileOperationW
	foDelete       = 0x0003
	fofAllowUndo16 = 0x0040
)

// Trash 把文件或目录移入回收站，保留原名（IFileOperation + FOFX_RECYCLEONDELETE）。
//
// 失败语义：
//   - 路径不存在或不可读 → 直接返回错误，文件系统不变；
//   - 卷不支持回收站（UNC 网络路径、FAT/FAT32/exFAT）→ 返回错误，不做"静默永久删除"；
//   - COM 不可用 → 回退 SHFileOperationW(FO_DELETE + FOF_ALLOWUNDO)。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	if err := checkRecycleSupported(abs); err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(hr) < 0 {
		return shFileOperationTrash(abs)
	}
	defer procCoUninitialize.Call()

	var op unsafe.Pointer
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOperation)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIFileOperation)),
		uintptr(unsafe.Pointer(&op)),
	)
	if int32(hr) < 0 || op == nil {
		return shFileOperationTrash(abs)
	}
	defer comRelease(op)

	flags := uintptr(fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofxRecycleOnDelete)
	if hr := comCallHR(op, 5, flags); int32(hr) < 0 {
		return hresultError("IFileOperation.SetOperationFlags", hr)
	}

	item, hr := shellItemFromPath(abs)
	if int32(hr) < 0 || item == nil {
		return hresultError("SHCreateItemFromParsingName", hr)
	}
	defer comRelease(item)

	if hr := comCallHR(op, 18, uintptr(item), 0); int32(hr) < 0 { // DeleteItem(item, nil)
		return hresultError("IFileOperation.DeleteItem", hr)
	}
	if hr := comCallHR(op, 21); int32(hr) < 0 { // PerformOperations()
		return hresultError("IFileOperation.PerformOperations", hr)
	}
	var aborted int32
	if hr := comCallHR(op, 22, uintptr(unsafe.Pointer(&aborted))); int32(hr) < 0 { // GetAnyOperationsAborted
		return hresultError("IFileOperation.GetAnyOperationsAborted", hr)
	}
	if aborted != 0 {
		return errors.New("platform: 移入回收站被取消")
	}
	return nil
}

func shellItemFromPath(abs string) (unsafe.Pointer, uintptr) {
	p, err := syscall.UTF16PtrFromString(abs)
	if err != nil {
		return nil, 0x80070057 // E_INVALIDARG
	}
	var item unsafe.Pointer
	hr, _, _ := procSHCreateItemFromParsing.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&iidIShellItem)),
		uintptr(unsafe.Pointer(&item)),
	)
	return item, hr
}

// shFileOpStructW 是 SHFILEOPSTRUCTW 的 Go 布局（字段顺序与对齐必须一致）。
type shFileOpStructW struct {
	Hwnd                  syscall.Handle
	WFunc                 uint32
	PFrom                 *uint16
	PTo                   *uint16
	FFlags                uint16
	FAnyOperationsAborted int32
	HNameMappings         uintptr
	LpszProgressTitle     *uint16
}

// shFileOperationTrash 是 COM 不可用时的兜底路径。
func shFileOperationTrash(abs string) error {
	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return err
	}
	from = append(from, 0) // 双 null 结尾
	op := shFileOpStructW{
		WFunc:  foDelete,
		PFrom:  &from[0],
		FFlags: fofAllowUndo16 | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("platform: SHFileOperationW 移入回收站失败（%d）", int32(r))
	}
	if op.FAnyOperationsAborted != 0 {
		return errors.New("platform: 移入回收站被取消")
	}
	return nil
}

// checkRecycleSupported 拒绝确定没有回收站的卷，避免 Shell 静默永久删除。
func checkRecycleSupported(abs string) error {
	volume := filepath.VolumeName(abs)
	if strings.HasPrefix(abs, `\\`) && volume == "" {
		return fmt.Errorf("platform: 网络路径没有回收站：%s", abs)
	}
	if volume == "" {
		return nil
	}
	root, err := syscall.UTF16PtrFromString(volume + `\`)
	if err != nil {
		return err
	}
	fsBuf := make([]uint16, 16)
	var serial, maxComponent, flags uint32
	r, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(root)),
		0,
		0,
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComponent)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&fsBuf[0])),
		uintptr(len(fsBuf)),
	)
	if r == 0 {
		return fmt.Errorf("platform: 无法读取卷信息：%s", abs)
	}
	switch strings.ToUpper(syscall.UTF16ToString(fsBuf)) {
	case "FAT", "FAT32", "EXFAT":
		return fmt.Errorf("platform: 该卷文件系统不支持回收站，请使用永久删除：%s", abs)
	}
	return nil
}
