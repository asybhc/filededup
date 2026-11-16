//go:build desktop

package main

// 按时间整理：预演返回分桶计划；真实执行异步跑，进度用 GetTimelineState 轮询。
// 移动/复制的落盘语义在 internal/timeline 里实现并有单测覆盖。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filededup/internal/auth"
	"filededup/internal/timeline"
)

// TimelineRequest 是界面传来的参数。
type TimelineRequest struct {
	Root          string `json:"root"`
	Target        string `json:"target"`
	Bucket        string `json:"bucket"`
	Action        string `json:"action"`
	Conflict      string `json:"conflict"`
	IncludeHidden bool   `json:"includeHidden"`
	Confirmed     bool   `json:"confirmed"`
}

// TimelineState 是异步执行状态。
type TimelineState struct {
	ID         string          `json:"id"`
	Phase      string          `json:"phase"` // running / done / failed / cancelled
	Total      int             `json:"total"`
	Done       int             `json:"done"`
	Current    string          `json:"current"`
	Error      string          `json:"error"`
	Result     timeline.Result `json:"result"`
	StartedAt  string          `json:"startedAt"`
	FinishedAt string          `json:"finishedAt"`
	ElapsedMS  int64           `json:"elapsedMs"`
}

type timelineJob struct {
	mu        sync.Mutex
	state     TimelineState
	cancel    context.CancelFunc
	startedAt time.Time
}

func (j *timelineJob) update(fn func(*TimelineState)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.state)
}

func (a *App) timelineJob(id string) (*timelineJob, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	j, ok := a.timelineJobs[id]
	return j, ok
}

// TimelineMeta 提供界面选项与后端校验保持同一套取值。
type TimelineMeta struct {
	Buckets   []TimelineOption `json:"buckets"`
	Actions   []TimelineOption `json:"actions"`
	Conflicts []TimelineOption `json:"conflicts"`
}

type TimelineOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func (a *App) GetTimelineMeta() TimelineMeta {
	return TimelineMeta{
		Buckets: []TimelineOption{
			{"day", "按日（2024-03-08）"},
			{"week", "按周（2024-W10）"},
			{"month", "按月（2024-03）"},
			{"year", "按年（2024）"},
		},
		Actions: []TimelineOption{
			{"move", "移动（源目录不再保留）"},
			{"copy", "复制（源目录保留）"},
		},
		Conflicts: []TimelineOption{
			{"rename", "改名保留（默认）"},
			{"skip", "跳过同名文件"},
			{"overwrite", "覆盖同名文件"},
		},
	}
}

func timelineOptions(req TimelineRequest) (timeline.Options, error) {
	root := strings.TrimSpace(req.Root)
	target := strings.TrimSpace(req.Target)
	if root == "" {
		return timeline.Options{}, errors.New("请先选择要按时间整理的源目录")
	}
	if target == "" {
		return timeline.Options{}, errors.New("请选择整理目标目录")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return timeline.Options{}, fmt.Errorf("源目录无效：%w", err)
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return timeline.Options{}, fmt.Errorf("源目录不可访问：%w", err)
	}
	if !info.IsDir() {
		return timeline.Options{}, errors.New("源路径不是文件夹")
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return timeline.Options{}, fmt.Errorf("目标目录无效：%w", err)
	}
	return timeline.Options{
		Root:          absRoot,
		Target:        absTarget,
		Bucket:        timeline.Bucket(strings.ToLower(strings.TrimSpace(req.Bucket))),
		Action:        timeline.Action(strings.ToLower(strings.TrimSpace(req.Action))),
		Conflict:      timeline.Conflict(strings.ToLower(strings.TrimSpace(req.Conflict))),
		IncludeHidden: req.IncludeHidden,
	}, nil
}

// PreviewTimeline 只读预演，最多返回 3000 条样例，总数与统计仍是全量。
func (a *App) PreviewTimeline(req TimelineRequest) (timeline.Plan, error) {
	opts, err := timelineOptions(req)
	if err != nil {
		return timeline.Plan{}, err
	}
	opts.Limit = 3000
	return timeline.PlanTree(opts)
}

// StartTimeline 确认后异步执行；移动/复制与冲突策略全部由引擎实现。
func (a *App) StartTimeline(req TimelineRequest) (string, error) {
	if !req.Confirmed {
		return "", errors.New("未确认，拒绝执行")
	}
	opts, err := timelineOptions(req)
	if err != nil {
		return "", err
	}
	if err := requireLicense(auth.FeatureApply); err != nil {
		return "", err
	}
	plan, err := timeline.PlanTree(opts)
	if err != nil {
		return "", err
	}
	if plan.Ready == 0 {
		return "", errors.New("没有可整理的文件（全部被跳过或已存在）")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &timelineJob{startedAt: time.Now()}
	job.state = TimelineState{
		ID:        newJobID(),
		Phase:     "running",
		Total:     plan.Ready,
		StartedAt: job.startedAt.Format(time.RFC3339),
		Result:    timeline.Result{Errors: []string{}},
	}
	job.cancel = cancel

	a.mu.Lock()
	if a.timelineJobs == nil {
		a.timelineJobs = make(map[string]*timelineJob)
	}
	a.timelineJobs[job.state.ID] = job
	a.mu.Unlock()

	id := job.state.ID
	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.update(func(s *TimelineState) {
					s.Phase = "failed"
					s.Error = fmt.Sprintf("内部错误：%v", r)
				})
			}
		}()
		res, err := timeline.ExecutePlan(ctx, opts, plan, func(idx int, it timeline.Item, ferr error) {
			job.update(func(s *TimelineState) {
				if it.Status == "ready" || it.Status == "conflict" {
					s.Done++
				}
				s.Current = it.Source
			})
		})
		job.update(func(s *TimelineState) {
			s.Result = res
			s.Current = ""
			s.ElapsedMS = time.Since(job.startedAt).Milliseconds()
			s.FinishedAt = time.Now().Format(time.RFC3339)
			switch {
			case ctx.Err() != nil:
				s.Phase = "cancelled"
				if s.Error == "" {
					s.Error = "已取消；已处理的文件不会回滚"
				}
			case err != nil:
				s.Phase = "failed"
				if s.Error == "" {
					s.Error = err.Error()
				}
			default:
				s.Phase = "done"
			}
		})
	}()
	return id, nil
}

// GetTimelineState 返回执行进度；找不到任务时返回错误。
func (a *App) GetTimelineState(id string) (TimelineState, error) {
	job, ok := a.timelineJob(id)
	if !ok {
		return TimelineState{}, errors.New("任务不存在或已结束")
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	st := job.state
	st.Result.Errors = append([]string(nil), job.state.Result.Errors...)
	return st, nil
}

// CancelTimeline 请求取消；已经落盘的文件由引擎逐项跳过后续处理并保留现状。
func (a *App) CancelTimeline(id string) {
	job, ok := a.timelineJob(id)
	if !ok {
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.cancel != nil {
		job.cancel()
	}
}
