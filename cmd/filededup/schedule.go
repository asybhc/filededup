//go:build desktop

package main

// 定时任务：按间隔或每日时间自动跑一次“只读扫描”。
//
// 产品取舍（保守实现）：定时任务只做扫描，不做删除与整理的真实执行。
// 扫描不会改动磁盘，失败也不会造成损失；自动删除/整理需要更严格的确认机制，本版不做。
// 上次执行结果落在 %AppData%\FileDedup\schedule-state.json，界面通过 GetScheduleState 读取。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filededup/internal/engine/model"
	"filededup/internal/engine/scan"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ScheduleRunState 是一次定时扫描的结果。
type ScheduleRunState struct {
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	Status      string `json:"status"` // ok | failed | skipped | cancelled
	Message     string `json:"message,omitempty"`
	Root        string `json:"root"`
	Files       int64  `json:"files"`
	Groups      int64  `json:"groups"`
	Reclaimable int64  `json:"reclaimable"`
	ElapsedMS   int64  `json:"elapsedMs"`
}

func scheduleStatePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "schedule-state.json"), nil
}

func loadScheduleState() ScheduleRunState {
	path, err := scheduleStatePath()
	if err != nil {
		return ScheduleRunState{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ScheduleRunState{}
	}
	var st ScheduleRunState
	_ = json.Unmarshal(raw, &st)
	return st
}

func saveScheduleState(st ScheduleRunState) {
	path, err := scheduleStatePath()
	if err != nil {
		return
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// GetScheduleState 返回上次定时扫描结果，供设置面板展示。
func (a *App) GetScheduleState() ScheduleRunState {
	return loadScheduleState()
}

// stopScheduler 取消当前循环。
func (a *App) stopScheduler() {
	a.schedMu.Lock()
	cancel := a.schedCancel
	a.schedCancel = nil
	a.schedMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// restartScheduler 按最新设置重建定时循环。关闭、根目录为空都不启动。
func (a *App) restartScheduler(s UISettings) {
	a.stopScheduler()
	if !s.Schedule.Enabled {
		return
	}
	root := strings.TrimSpace(s.Schedule.Root)
	if root == "" {
		root = strings.TrimSpace(s.Scan.Root)
	}
	if root == "" {
		st := ScheduleRunState{
			Status:     "skipped",
			Message:    "定时任务已启用，但没有可扫描的根目录（请在设置或查重页选择目录）",
			FinishedAt: time.Now().Format(time.RFC3339),
		}
		saveScheduleState(st)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.schedMu.Lock()
	a.schedCancel = cancel
	a.schedMu.Unlock()
	go a.scheduleLoop(ctx, s, root)
}

func (a *App) scheduleLoop(ctx context.Context, s UISettings, root string) {
	for {
		delay := scheduleDelay(s.Schedule, time.Now())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		a.runScheduledScan(ctx, s.Scan, root)
	}
}

// scheduleDelay 计算距离下一次执行的时间。
func scheduleDelay(sc ScheduleSettings, now time.Time) time.Duration {
	if sc.Mode == "daily" {
		parts := strings.Split(sc.DailyTime, ":")
		if len(parts) == 2 {
			h, _ := strconv.Atoi(parts[0])
			m, _ := strconv.Atoi(parts[1])
			next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
			if !next.After(now) {
				next = next.Add(24 * time.Hour)
			}
			return next.Sub(now)
		}
	}
	minutes := sc.IntervalMinutes
	if minutes < 10 {
		minutes = 10
	}
	return time.Duration(minutes) * time.Minute
}

func (a *App) runScheduledScan(ctx context.Context, sc ScanSettings, root string) {
	st := ScheduleRunState{
		StartedAt: time.Now().Format(time.RFC3339),
		Root:      root,
		Status:    "ok",
	}
	start := time.Now()

	info, err := os.Stat(root)
	if err != nil {
		st.Status = "failed"
		st.Message = fmt.Sprintf("无法访问目录：%v", err)
		st.FinishedAt = time.Now().Format(time.RFC3339)
		saveScheduleState(st)
		return
	}
	if !info.IsDir() {
		st.Status = "failed"
		st.Message = fmt.Sprintf("不是目录：%s", root)
		st.FinishedAt = time.Now().Format(time.RFC3339)
		saveScheduleState(st)
		return
	}

	req := scanRequestFromSettings(sc, root)
	res, scanErr := scan.RunWithOptions(ctx, req, scan.Options{})
	st.ElapsedMS = time.Since(start).Milliseconds()
	st.FinishedAt = time.Now().Format(time.RFC3339)
	switch {
	case scanErr == nil && res != nil:
		st.Files = res.Stats.FilesEnumerated
		st.Groups = res.Stats.Groups
		st.Reclaimable = res.Stats.ReclaimableBytes
		st.Message = fmt.Sprintf("扫描完成：%d 个文件，%d 组重复，可回收 %d 字节",
			st.Files, st.Groups, st.Reclaimable)
	case scanErr != nil && ctx.Err() != nil:
		st.Status = "cancelled"
		st.Message = "应用退出，定时扫描已取消"
	default:
		st.Status = "failed"
		st.Message = scanErr.Error()
	}
	saveScheduleState(st)
	log.Printf("定时扫描结束：%s %s", st.Status, st.Message)
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "schedule:done", st)
	}
}

// scanRequestFromSettings 把持久化选项转成引擎请求；与界面 StartScan 的映射保持一致。
func scanRequestFromSettings(sc ScanSettings, root string) model.Request {
	keep := model.KeepPolicy(sc.KeepPolicy)
	switch keep {
	case model.KeepNewest, model.KeepOldest, model.KeepNameShortest, model.KeepPathShortest:
	default:
		keep = model.KeepPathShortest
	}
	mreq := model.DefaultRequest()
	mreq.RequestID = fmt.Sprintf("schedule-%d", time.Now().UnixNano())
	mreq.Roots = []string{root}
	mreq.Excludes = append([]string{}, sc.Excludes...)
	mreq.MinSize = sc.MinSize
	mreq.IncludeHidden = sc.IncludeHidden
	mreq.Exact = true
	mreq.KeepPolicy = keep
	mreq.ImageKeepPolicy = model.KeepLargest
	if sc.SimilarImage {
		threshold := sc.ImageThreshold
		if threshold < 0 || threshold > model.MaxImageThreshold {
			threshold = 8
		}
		mreq.SimilarImage = &model.ImageOpts{Enabled: true, Threshold: threshold}
	}
	if sc.SimilarAudio {
		tol := sc.AudioTolerance
		if tol < 0 || tol > 60 {
			tol = 2
		}
		mreq.SimilarAudio = &model.AudioOpts{Enabled: true, Tolerance: tol}
	}
	return mreq
}
