//go:build desktop

package main

// 运行时行为：窗口关闭策略、托盘生命周期、设置变更后的同步。
// 定时任务的具体调度在 schedule.go。

import (
	"context"
	"log"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// markQuitting 由托盘“退出”调用，允许窗口真正关闭。
func (a *App) markQuitting() {
	a.quittingMu.Lock()
	a.quitting = true
	a.quittingMu.Unlock()
}

func (a *App) isQuitting() bool {
	a.quittingMu.Lock()
	defer a.quittingMu.Unlock()
	return a.quitting
}

// beforeClose 是 Wails 的 OnBeforeClose。
//
// 逻辑顺序：
//   - 托盘“退出”已置位时直接放行；
//   - 关闭行为是“最小化到托盘”时只隐藏窗口，任务继续跑，不做中断确认；
//   - 真正要退出时，若扫描/执行/整理等任务仍在跑，先弹一次确认框，空闲时不打扰。
func (a *App) beforeClose(ctx context.Context) bool {
	if a.isQuitting() {
		return false
	}
	if a.trayAvailable() {
		s, _ := LoadSettings()
		if s.Window.CloseAction == "tray" {
			wruntime.WindowHide(ctx)
			log.Printf("窗口已最小化到托盘（设置：关闭窗口时最小化）")
			return true
		}
	}
	if !a.confirmQuitWhileBusy(ctx) {
		log.Printf("关闭被取消：仍有任务在执行")
		return true
	}
	return false
}

// applyRuntimeSettings 在启动或保存设置后同步托盘与定时任务。
// 失败只记日志，不影响设置已保存这一事实。
func (a *App) applyRuntimeSettings(s UISettings) {
	if s.Window.TrayEnabled && a.trayAvailable() {
		startTray(a)
	} else {
		stopTray()
	}
	a.restartScheduler(s)
}
