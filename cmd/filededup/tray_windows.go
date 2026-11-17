//go:build desktop && windows

package main

// Windows 托盘：用 Shell_NotifyIcon 建一个隐藏窗口，右键菜单提供“打开 / 扫描 / 退出”。
//
// 说明：Wails v2 本身不提供托盘，这里直接用 Win32 实现，不引入第三方托盘库，
// 避免与 Wails 的窗口消息循环抢主线程。消息循环跑在独立 OS 线程上。

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"filededup/internal/brand"
)

const (
	wmCommand      = 0x0111
	wmClose        = 0x0010
	wmDestroy      = 0x0002
	wmNull         = 0x0000
	wmApp          = 0x8000
	trayCallback   = wmApp + 1
	trayID         = 1
	nimAdd         = 0
	nimDelete      = 2
	nifMessage     = 0x0001
	nifIcon        = 0x0002
	nifTip         = 0x0004
	menuOpen       = 1001
	menuScan       = 1002
	menuQuit       = 1003
	idiApplication = 32512
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pLoadIconW           = user32.NewProc("LoadIconW")
	pShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

// trayAvailable 报告当前平台是否有托盘。
func (a *App) trayAvailable() bool { return true }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type winMsg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

type point struct{ X, Y int32 }

type notifyIconData struct {
	Size             uint32
	Hwnd             syscall.Handle
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             syscall.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUID             syscall.GUID
	BalloonIcon      syscall.Handle
}

// trayController 管理托盘图标生命周期。
type trayController struct {
	app  *App
	hwnd uintptr
	done chan struct{}
	once sync.Once
}

var (
	trayMu  sync.Mutex
	current *trayController
	wndProc = syscall.NewCallback(trayWindowProc)
)

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func trayWindowProc(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCommand:
		id := wparam & 0xFFFF
		trayMu.Lock()
		t := current
		trayMu.Unlock()
		if t == nil {
			return 0
		}
		switch id {
		case menuOpen:
			t.openWindow()
		case menuScan:
			t.runScan()
		case menuQuit:
			t.quit()
		}
		return 0
	case trayCallback:
		// 右键或左键都弹菜单；双击（lparam == WM_LBUTTONDBLCLK）直接打开窗口。
		if lparam == 0x0203 { // WM_LBUTTONDBLCLK
			trayMu.Lock()
			t := current
			trayMu.Unlock()
			if t != nil {
				t.openWindow()
			}
			return 0
		}
		showTrayMenu(hwnd)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return ret
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	pAppendMenuW.Call(menu, 0, menuOpen, uintptr(unsafe.Pointer(utf16Ptr("打开主界面"))))
	pAppendMenuW.Call(menu, 0, menuScan, uintptr(unsafe.Pointer(utf16Ptr("扫描（打开窗口并开始）"))))
	pAppendMenuW.Call(menu, 0x800, 0, 0) // MF_SEPARATOR
	pAppendMenuW.Call(menu, 0, menuQuit, uintptr(unsafe.Pointer(utf16Ptr("退出"))))

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// TrackPopupMenu 前必须把窗口置前，否则点菜单外面菜单不会消失。
	pSetForegroundWindow.Call(hwnd)
	pTrackPopupMenu.Call(menu, 0x0002, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pPostMessageW.Call(hwnd, wmNull, 0, 0)
}

// startTray 创建托盘图标。重复调用会复用现有实例。
func startTray(a *App) {
	trayMu.Lock()
	if current != nil {
		trayMu.Unlock()
		return
	}
	trayMu.Unlock()

	t := &trayController{app: a, done: make(chan struct{})}
	trayMu.Lock()
	current = t
	trayMu.Unlock()

	go t.loop()
}

// stopTray 移除托盘图标并结束消息循环。
func stopTray() {
	trayMu.Lock()
	t := current
	current = nil
	trayMu.Unlock()
	if t != nil {
		t.stop()
	}
}

func (t *trayController) loop() {
	// 托盘窗口与消息循环必须固定在一个 OS 线程上。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)

	hInst, _, _ := pGetModuleHandleW.Call(0)
	className := utf16Ptr("FileDedupTrayWnd")
	wc := wndClassEx{
		WndProc:   wndProc,
		Instance:  syscall.Handle(hInst),
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		// 1410 = ERROR_CLASS_ALREADY_EXISTS：重复启动时复用已注册的类。
		if errno, ok := err.(syscall.Errno); !ok || errno != 1410 {
			return
		}
	}

	hwnd, _, _ := pCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(utf16Ptr("FileDedupTray"))),
		0, 0, 0, 0, 0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return
	}
	t.hwnd = hwnd

	icon, _, _ := pLoadIconW.Call(0, idiApplication)
	var nid notifyIconData
	nid.Size = uint32(unsafe.Sizeof(nid))
	nid.Hwnd = syscall.Handle(hwnd)
	nid.ID = trayID
	nid.Flags = nifMessage | nifIcon | nifTip
	nid.CallbackMessage = trayCallback
	nid.Icon = syscall.Handle(icon)
	copy(nid.Tip[:len(nid.Tip)-1], syscall.StringToUTF16(brand.DisplayName))
	if r, _, _ := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		pDestroyWindow.Call(hwnd)
		return
	}

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	pDestroyWindow.Call(hwnd)
}

func (t *trayController) stop() {
	t.once.Do(func() {
		if t.hwnd != 0 {
			pPostMessageW.Call(t.hwnd, wmClose, 0, 0)
		}
	})
}

func (t *trayController) openWindow() {
	if t.app == nil || t.app.ctx == nil {
		return
	}
	wruntime.WindowShow(t.app.ctx)
}

func (t *trayController) runScan() {
	t.openWindow()
	if t.app != nil && t.app.ctx != nil {
		// 扫描按钮的实际参数以界面当前选择为准，避免托盘用一份过期配置。
		wruntime.EventsEmit(t.app.ctx, "tray:scan")
	}
}

func (t *trayController) quit() {
	if t.app == nil {
		return
	}
	if t.app.ctx != nil {
		// 任务执行中从托盘退出同样需要确认；窗口可能处于隐藏状态，对话框仍由应用主窗口承载。
		if !t.app.confirmQuitWhileBusy(t.app.ctx) {
			return
		}
	}
	t.app.markQuitting()
	if t.app.ctx != nil {
		wruntime.Quit(t.app.ctx)
	}
}
