//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ErrLocked 表示文件被其它进程占用，且（按调用方策略）未解除。
var ErrLocked = errors.New("platform: 文件被其它进程占用")

const (
	errorSharingViolation = syscall.Errno(32)
	errorLockViolation    = syscall.Errno(33)

	processTerminate              = 0x0001
	processQueryLimitedInformation = 0x1000
	synchronize                   = 0x00100000

	wmClose = 0x0010
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	rstrtmgr = syscall.NewLazyDLL("rstrtmgr.dll")

	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetProcessTimes            = kernel32.NewProc("GetProcessTimes")
	procProcessIdToSessionId       = kernel32.NewProc("ProcessIdToSessionId")
	procTerminateProcess           = kernel32.NewProc("TerminateProcess")
	procEnumWindows                = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessID   = user32.NewProc("GetWindowThreadProcessId")
	procIsWindowVisible            = user32.NewProc("IsWindowVisible")
	procPostMessageW               = user32.NewProc("PostMessageW")

	procRmStartSession      = rstrtmgr.NewProc("RmStartSession")
	procRmEndSession        = rstrtmgr.NewProc("RmEndSession")
	procRmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	procRmGetList           = rstrtmgr.NewProc("RmGetList")
)

// LockHolder 描述一个占用文件的进程，字段对齐原版 B/N.cs。
type LockHolder struct {
	PID             uint32
	Name            string
	Executable      string
	SessionID       uint32
	ApplicationType uint32
	HasWindow       bool
	Protected       bool

	hwnd      uintptr
	startTime syscall.Filetime
}

// IsLocked 判断文件是否被占用。
// 对齐原版 a.a.F()：以 FileShare.None + 读写方式打开；共享冲突算被占用，
// 其它错误（不存在、拒绝访问等）一律算“未占用”。
func IsLocked(path string) bool {
	ptr, err := syscall.UTF16PtrFromString(longPath(path))
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(ptr,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		0, // 不共享
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return errors.Is(err, errorSharingViolation) || errors.Is(err, errorLockViolation)
	}
	_ = syscall.CloseHandle(h)
	return false
}

type rmUniqueProcess struct {
	ProcessID uint32
	StartTime syscall.Filetime
}

type rmProcessInfo struct {
	Process         rmUniqueProcess
	AppName         [256]uint16
	ServiceName     [64]uint16
	ApplicationType uint32
	AppStatus       uint32
	SessionID       uint32
	Restartable     int32
}

// Locks 返回占用文件的全部进程；对齐原版 B/O.cs 的 Restart Manager 枚举。
func Locks(path string) ([]LockHolder, error) {
	var session uint32
	key, _ := syscall.UTF16PtrFromString("FileDedup-Go")
	if r, _, _ := procRmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(key))); r != 0 {
		return nil, fmt.Errorf("platform: RmStartSession 失败（%d）", r)
	}
	defer procRmEndSession.Call(uintptr(session))

	filePtr, err := syscall.UTF16PtrFromString(longPath(path))
	if err != nil {
		return nil, err
	}
	files := []*uint16{filePtr}
	if r, _, _ := procRmRegisterResources.Call(
		uintptr(session), 1, uintptr(unsafe.Pointer(&files[0])), 0, 0, 0, 0); r != 0 {
		return nil, fmt.Errorf("platform: RmRegisterResources 失败（%d）", r)
	}

	var needed, count uint32
	r, _, _ := procRmGetList.Call(uintptr(session),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), 0, 0)
	if r == 234 { // ERROR_MORE_DATA
		if needed == 0 {
			return nil, nil
		}
		infos := make([]rmProcessInfo, needed)
		got := needed
		r, _, _ = procRmGetList.Call(uintptr(session),
			uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&got)),
			uintptr(unsafe.Pointer(&infos[0])), 0)
		if r != 0 {
			return nil, fmt.Errorf("platform: RmGetList 失败（%d）", r)
		}
		return holdersFrom(infos[:got]), nil
	}
	if r != 0 {
		return nil, fmt.Errorf("platform: RmGetList 失败（%d）", r)
	}
	return nil, nil
}

// 受保护进程：系统关键进程与外壳进程，结束它们会造成系统不稳定。
var protectedProcessNames = map[string]bool{
	"system": true, "idle": true, "registry": true, "memory compression": true,
	"smss": true, "csrss": true, "wininit": true, "winlogon": true,
	"services": true, "lsass": true, "svchost": true, "dwm": true,
	"fontdrvhost": true, "sihost": true, "ctfmon": true, "explorer": true,
	"audiodg": true, "spoolsv": true, "taskhostw": true, "runtimebroker": true,
	"searchindexer": true, "shellexperiencehost": true, "startmenuexperiencehost": true,
	"textinputhost": true, "securityhealthservice": true, "msmpeng": true,
	"nissrv": true, "sppsvc": true, "wudfhost": true,
}

func holdersFrom(infos []rmProcessInfo) []LockHolder {
	selfPID := uint32(syscall.Getpid())
	selfSession := currentSessionID()
	out := make([]LockHolder, 0, len(infos))
	for _, in := range infos {
		pid := in.Process.ProcessID
		if pid <= 4 || pid == selfPID {
			continue
		}
		h := LockHolder{
			PID:             pid,
			Name:            syscall.UTF16ToString(in.AppName[:]),
			SessionID:       in.SessionID,
			ApplicationType: in.ApplicationType,
			startTime:       in.Process.StartTime,
		}
		h.Name = strings.TrimSuffix(h.Name, ".exe")
		h.Executable = processImage(pid)
		h.hwnd = firstVisibleWindow(pid)
		h.HasWindow = h.hwnd != 0
		h.Protected = in.ApplicationType == 3 || in.ApplicationType == 1000 ||
			in.SessionID == 0 || in.SessionID != selfSession ||
			protectedProcessNames[strings.ToLower(h.Name)]
		out = append(out, h)
	}
	return out
}

func processImage(pid uint32) string {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, pid)
	if err != nil {
		return ""
	}
	defer syscall.CloseHandle(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

func currentSessionID() uint32 {
	var sid uint32
	procProcessIdToSessionId.Call(uintptr(syscall.Getpid()), uintptr(unsafe.Pointer(&sid)))
	return sid
}

func firstVisibleWindow(pid uint32) uintptr {
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid != pid {
			return 1
		}
		if vis, _, _ := procIsWindowVisible.Call(hwnd); vis != 0 {
			found = hwnd
			return 0
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// ReleaseLocks 尝试解除占用：有可见窗口的进程先温和关闭，无窗口的进程直接结束；
// 结束后等待 600ms 复查。有可见窗口时若 confirm 为 nil 或返回 false，则不解占用。
// 该行为对齐原版 B/O.cs 的静默模式（只有可能丢未保存数据时才要求确认）。
func ReleaseLocks(path string, confirm func(path string, holders []LockHolder) bool) error {
	if !IsLocked(path) {
		return nil
	}
	holders, err := Locks(path)
	if err != nil {
		return err
	}
	var active []LockHolder
	for _, h := range holders {
		if !h.Protected {
			active = append(active, h)
		}
	}
	if len(active) == 0 {
		return fmt.Errorf("%w: 占用进程均为系统关键进程，拒绝结束", ErrLocked)
	}
	for _, h := range active {
		if !h.HasWindow {
			continue
		}
		if confirm == nil {
			return fmt.Errorf("%w: 占用程序 %s 有可见窗口，需要人工确认", ErrLocked, h.Name)
		}
		if !confirm(path, active) {
			return fmt.Errorf("%w: 用户拒绝关闭占用程序", ErrLocked)
		}
		break
	}
	acted := false
	for _, h := range active {
		if closeHolder(h) {
			acted = true
		}
	}
	if acted {
		time.Sleep(600 * time.Millisecond)
	}
	if IsLocked(path) {
		return fmt.Errorf("%w: 解除占用后文件仍被锁定", ErrLocked)
	}
	return nil
}

// closeHolder 结束单个占用进程；PID + 启动时间双重校验，避免 PID 复用误杀。
func closeHolder(h LockHolder) bool {
	if !sameProcess(h) {
		return false
	}
	if h.HasWindow {
		procPostMessageW.Call(h.hwnd, wmClose, 0, 0)
		ph, err := syscall.OpenProcess(synchronize|processQueryLimitedInformation, false, h.PID)
		if err == nil {
			syscall.WaitForSingleObject(ph, 5000)
			_ = syscall.CloseHandle(ph)
		}
		// 有窗口的程序可能持有未保存数据：未退出就不强杀。
		return !stillAlive(h.PID)
	}
	ph, err := syscall.OpenProcess(processTerminate|synchronize, false, h.PID)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(ph)
	if r, _, _ := procTerminateProcess.Call(uintptr(ph), 1); r == 0 {
		return false
	}
	syscall.WaitForSingleObject(ph, 2000)
	return !stillAlive(h.PID)
}

func stillAlive(pid uint32) bool {
	h, err := syscall.OpenProcess(synchronize|processQueryLimitedInformation, false, pid)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	r, _ := syscall.WaitForSingleObject(h, 0)
	return r == 258 // WAIT_TIMEOUT
}

func sameProcess(h LockHolder) bool {
	ph, err := syscall.OpenProcess(processQueryLimitedInformation, false, h.PID)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(ph)
	var creation, exit, kernel, user syscall.Filetime
	if r, _, _ := procGetProcessTimes.Call(uintptr(ph),
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r == 0 {
		return false
	}
	return creation.LowDateTime == h.startTime.LowDateTime &&
		creation.HighDateTime == h.startTime.HighDateTime
}
