//go:build desktop

package main

// 关闭确认：扫描 / 删除执行 / 整理 / 搜索 / 上传正在进行时，关闭窗口或从托盘退出前
// 先弹一次系统确认框；空闲时不打扰。
//
// 判定只看各任务的内存态，不查磁盘，因此不会因为上次异常退出留下的状态而误报。

import (
	"context"
	"fmt"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	quitKeepRunning = "继续执行"
	quitAnyway      = "仍然退出"
)

// runningTasks 返回当前仍在执行的任务名称（去重后的中文短语）。
func (a *App) runningTasks() []string {
	var out []string

	a.mu.Lock()
	for _, s := range a.sessions {
		s.mu.Lock()
		if s.state.Phase == "running" || s.state.Phase == "cancelling" {
			out = append(out, "查重扫描")
		}
		if s.executing {
			out = append(out, "删除执行")
		}
		s.mu.Unlock()
	}
	for _, j := range a.orgJobs {
		j.mu.Lock()
		if j.state.Phase == "running" {
			out = append(out, "九步整理")
		}
		j.mu.Unlock()
	}
	a.mu.Unlock()

	a.searchMu.Lock()
	for _, j := range a.searches {
		j.mu.Lock()
		if j.state.Running {
			out = append(out, "本地搜索")
		}
		j.mu.Unlock()
	}
	a.searchMu.Unlock()

	a.lanzouMu.Lock()
	for _, p := range a.lanzouPreps {
		p.mu.Lock()
		if p.running {
			out = append(out, "网盘上传")
		}
		p.mu.Unlock()
	}
	a.lanzouMu.Unlock()

	seen := make(map[string]bool, len(out))
	uniq := out[:0]
	for _, name := range out {
		if seen[name] {
			continue
		}
		seen[name] = true
		uniq = append(uniq, name)
	}
	return uniq
}

// confirmQuitWhileBusy 在有关键任务时弹确认框。返回 true 表示可以继续退出。
// 空闲或对话框不可用时返回 true：不能因为对话框故障把窗口卡死。
func (a *App) confirmQuitWhileBusy(ctx context.Context) bool {
	tasks := a.runningTasks()
	if len(tasks) == 0 {
		return true
	}
	if ctx == nil {
		return true
	}
	res, err := wruntime.MessageDialog(ctx, wruntime.MessageDialogOptions{
		Type:    wruntime.QuestionDialog,
		Title:   "任务正在执行",
		Message: fmt.Sprintf("以下任务仍在执行：%s。\n\n立即退出会中断任务，已处理的数据可能不完整。仍要退出吗？", strings.Join(tasks, "、")),
		Buttons: []string{quitKeepRunning, quitAnyway},
		// 默认落在“继续执行”，避免误按回车直接中断任务。
		DefaultButton: quitKeepRunning,
		CancelButton:  quitKeepRunning,
	})
	if err != nil {
		return true
	}
	return res == quitAnyway
}

// HasRunningTasks 供前端在自定义关闭流程里查询；返回 false 时界面不应弹出任何提示。
func (a *App) HasRunningTasks() (bool, string) {
	tasks := a.runningTasks()
	if len(tasks) == 0 {
		return false, ""
	}
	return true, strings.Join(tasks, "、")
}
