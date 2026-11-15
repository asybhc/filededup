//go:build desktop && !windows

package main

// 非 Windows 平台没有托盘，相关调用退化为空操作；窗口关闭行为按“退出”处理。

func startTray(a *App) {}
func stopTray()        {}
func (a *App) trayAvailable() bool {
	return false
}
