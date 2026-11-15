//go:build desktop

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"filededup/internal/auth"
	"filededup/internal/cloud/lanzou"
	"filededup/internal/cloud/lanzouyou"
	"filededup/internal/engine/apply"
	"filededup/internal/engine/model"
	"filededup/internal/engine/plan"
	"filededup/internal/engine/scan"
	"filededup/internal/logx"
	"filededup/internal/platform"
	"filededup/internal/update"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const appVersion = "0.9.5"

// ScanRequest 是 UI 的扫描参数。只暴露当前流程用得到的字段。
type ScanRequest struct {
	Root           string   `json:"root"`
	KeepPolicy     string   `json:"keepPolicy"`
	SimilarImage   bool     `json:"similarImage"`
	ImageThreshold int      `json:"imageThreshold"`
	SimilarAudio   bool     `json:"similarAudio"`
	AudioTolerance int      `json:"audioTolerance"`
	MinSize        int64    `json:"minSize"`
	IncludeHidden  bool     `json:"includeHidden"`
	Excludes       []string `json:"excludes"`
}

// ScanState 是扫描进度的只读快照，前端轮询获取。
type ScanState struct {
	ID              string `json:"id"`
	Phase           string `json:"phase"` // running | cancelling | done | failed | cancelled
	Files           int64  `json:"files"`
	Bytes           int64  `json:"bytes"`
	HashTargets     int64  `json:"hashTargets"`
	HashDone        int64  `json:"hashDone"`
	HashBytes       int64  `json:"hashBytes"`
	HashTotalBytes  int64  `json:"hashTotalBytes"`
	EnumerationDone bool   `json:"enumerationDone"`
	Groups          int64  `json:"groups"`
	DuplicateFiles  int64  `json:"duplicateFiles"`
	Reclaimable     int64  `json:"reclaimable"`
	Skipped         int64  `json:"skipped"`
	Error           string `json:"error,omitempty"`
	ElapsedMS       int64  `json:"elapsedMs"`
}

// MemberView 是组内成员。ModTime 保持 UTC RFC3339Nano，由前端本地化。
type MemberView struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	ModTime   string `json:"modTime"`
	Keep      bool   `json:"keep"`
	Deletable bool   `json:"deletable"`
}

// GroupView 是一个重复组及其全部成员。
type GroupView struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Keep        string       `json:"keep"`
	Reclaimable int64        `json:"reclaimable"`
	Members     []MemberView `json:"members"`
}

type GroupsPage struct {
	Total  int         `json:"total"`
	Offset int         `json:"offset"`
	Items  []GroupView `json:"items"`
}

type SkipView struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SkippedPage struct {
	Total  int        `json:"total"`
	Offset int        `json:"offset"`
	Items  []SkipView `json:"items"`
}

type ActionView struct {
	Seq    int    `json:"seq"`
	Kind   string `json:"kind"`
	Src    string `json:"src"`
	Reason string `json:"reason"`
	Size   int64  `json:"size"`
}

type PlanPreview struct {
	RequestID        string         `json:"requestId"`
	DeleteMode       string         `json:"deleteMode"`
	Total            int            `json:"total"`
	Groups           int            `json:"groups"`
	ReclaimableBytes int64          `json:"reclaimableBytes"`
	KindCounts       map[string]int `json:"kindCounts"`
	Samples          []ActionView   `json:"samples"`
}

type ExecuteState struct {
	Running    bool   `json:"running"`
	Done       bool   `json:"done"`
	Total      int    `json:"total"`
	Applied    int    `json:"applied"`
	Failed     int    `json:"failed"`
	Skipped    int    `json:"skipped"`
	RolledBack int    `json:"rolledBack"`
	Reclaimed  int64  `json:"reclaimed"`
	Error      string `json:"error,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

type AppInfo struct {
	Version  string `json:"version"`
	Platform string `json:"platform"`
}

type scanSession struct {
	id     string
	req    model.Request
	cancel context.CancelFunc
	start  time.Time

	mu             sync.Mutex
	state          ScanState
	result         *model.Result
	plan           *model.Plan
	planMode       model.DeleteMode
	exec           ExecuteState
	executing      bool
	execCancelling context.CancelFunc
}

func (s *scanSession) snapshot() ScanState {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.state
	if !s.start.IsZero() {
		st.ElapsedMS = time.Since(s.start).Milliseconds()
	}
	return st
}

// App 是 Wails 绑定的门面。方法在 JS 侧通过 window.go.main.App 调用。
type App struct {
	ctx context.Context

	mu       sync.Mutex
	sessions map[string]*scanSession
	orgJobs  map[string]*organizeJob
	// undoMu 串行化“撤销上次整理”，避免并发回滚同一份记录。
	undoMu sync.Mutex
	// updateMu 串行化安装更新，避免并发下载/替换。
	updateMu sync.Mutex

	// lanzouMu 保护蓝奏云/蓝奏优享客户端与预检结果；lanzouPreps 里是尚未上传完的批次。
	lanzouMu       sync.Mutex
	lanzouClient   *lanzou.Client
	lanzouSource   string
	youxiangClient *lanzouyou.Client
	youxiangSource string
	lanzouPreps    map[string]*lanzouPrepare

	// quitting 由托盘“退出”置位：OnBeforeClose 据此放行，不再最小化到托盘。
	quittingMu sync.Mutex
	quitting   bool
	// schedCancel 取消当前定时任务循环；设置为空表示没有启用定时任务。
	schedMu     sync.Mutex
	schedCancel context.CancelFunc

	// searches 是本地搜索任务；searchMu 保护这张表，任务内部状态另有锁。
	searchMu sync.Mutex
	searches map[string]*searchJob
}

func NewApp() *App {
	return &App{
		sessions:    make(map[string]*scanSession),
		orgJobs:     make(map[string]*organizeJob),
		lanzouPreps: make(map[string]*lanzouPrepare),
		searches:    make(map[string]*searchJob),
	}
}

func currentPlatform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// 上次更新若在替换中途中断，这里把 .old 恢复或清理掉。
	if exe, err := os.Executable(); err == nil {
		if err := update.Cleanup(exe); err != nil {
			log.Printf("更新残留清理失败：%v", err)
		}
	}
	// 清掉旧安装包与下载残留，只保留最近两个包。
	pruneUpdateCache()
	// 首次启动创建试用记录；之后每次启动更新“最后见到”时间，用于检测回拨。
	if err := auth.TouchTrial(); err != nil {
		log.Printf("试用记录更新失败：%v", err)
	}
	// 托盘与定时任务按保存的设置启动。
	a.applyRuntimeSettings(mustLoadSettings())
}

// mustLoadSettings 读取设置，失败时退回默认值；启动阶段不能让设置问题阻塞窗口。
func mustLoadSettings() UISettings {
	s, _ := LoadSettings()
	return s
}

func (a *App) shutdown(ctx context.Context) {
	stopTray()
	a.stopScheduler()
	a.mu.Lock()
	sessions := make([]*scanSession, 0, len(a.sessions))
	for _, s := range a.sessions {
		sessions = append(sessions, s)
	}
	a.mu.Unlock()
	for _, s := range sessions {
		s.cancel()
		s.mu.Lock()
		if s.execCancelling != nil {
			s.execCancelling()
		}
		s.mu.Unlock()
	}

	a.lanzouMu.Lock()
	preps := make([]*lanzouPrepare, 0, len(a.lanzouPreps))
	for _, p := range a.lanzouPreps {
		preps = append(preps, p)
	}
	a.lanzouMu.Unlock()
	for _, p := range preps {
		p.mu.Lock()
		if p.cancel != nil {
			p.cancel()
		}
		p.mu.Unlock()
	}
}

// GetAppInfo 返回版本与平台，供界面显示。
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Version: appVersion, Platform: currentPlatform()}
}

// PickDirectory 打开系统目录选择框。
func (a *App) PickDirectory() (string, error) {
	if a.ctx == nil {
		return "", errors.New("应用尚未完成初始化")
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择要查重的目录",
	})
}

// StartScan 启动异步扫描并立即返回会话 ID。进度用 GetScanState 轮询。
func (a *App) StartScan(req ScanRequest) (string, error) {
	root := strings.TrimSpace(req.Root)
	if root == "" {
		return "", errors.New("请先选择目录")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("目录必须是绝对路径：%s", root)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("无法访问目录：%w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("不是目录：%s", root)
	}
	keep := model.KeepPolicy(req.KeepPolicy)
	if keep == "" {
		keep = model.KeepPathShortest
	}
	switch keep {
	case model.KeepNewest, model.KeepOldest, model.KeepNameShortest, model.KeepPathShortest:
	default:
		return "", fmt.Errorf("不支持的保留策略：%s", req.KeepPolicy)
	}
	if req.MinSize < 0 {
		return "", errors.New("最小文件大小不能为负")
	}
	if req.ImageThreshold < 0 || req.ImageThreshold > 64 {
		return "", fmt.Errorf("相似阈值必须在 0..64：%d", req.ImageThreshold)
	}
	if req.AudioTolerance < 0 || req.AudioTolerance > 60 {
		return "", fmt.Errorf("音频容差必须在 0..60 秒：%d", req.AudioTolerance)
	}

	mreq := model.DefaultRequest()
	mreq.RequestID = fmt.Sprintf("ui-%d", time.Now().UnixNano())
	mreq.Roots = []string{root}
	mreq.Excludes = append([]string{}, req.Excludes...)
	mreq.MinSize = req.MinSize
	mreq.IncludeHidden = req.IncludeHidden
	mreq.Exact = true
	mreq.KeepPolicy = keep
	mreq.ImageKeepPolicy = model.KeepLargest
	if req.SimilarImage {
		mreq.SimilarImage = &model.ImageOpts{Enabled: true, Threshold: req.ImageThreshold}
	}
	if req.SimilarAudio {
		mreq.SimilarAudio = &model.AudioOpts{Enabled: true, Tolerance: req.AudioTolerance}
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &scanSession{
		id:     mreq.RequestID,
		req:    mreq,
		cancel: cancel,
		start:  time.Now(),
		state:  ScanState{ID: mreq.RequestID, Phase: "running"},
	}

	a.mu.Lock()
	a.sessions[s.id] = s
	a.mu.Unlock()

	go func() {
		res, err := scan.RunWithOptions(ctx, mreq, scan.Options{
			Progress: func(p scan.Progress) {
				s.mu.Lock()
				s.state.Files = p.Files
				s.state.Bytes = p.Bytes
				s.state.HashTargets = p.HashTargets
				s.state.HashDone = p.HashDone
				s.state.HashBytes = p.HashBytes
				s.state.HashTotalBytes = p.HashTotalBytes
				s.state.EnumerationDone = p.EnumerationDone
				s.mu.Unlock()
			},
		})
		s.mu.Lock()
		if res != nil {
			s.result = res
			s.state.Files = res.Stats.FilesEnumerated
			s.state.Bytes = res.Stats.BytesEnumerated
			s.state.Groups = res.Stats.Groups
			s.state.DuplicateFiles = res.Stats.DuplicateFiles
			s.state.Reclaimable = res.Stats.ReclaimableBytes
			s.state.Skipped = res.Stats.Skipped
			s.state.EnumerationDone = true
		}
		s.state.ElapsedMS = time.Since(s.start).Milliseconds()
		switch {
		case err == nil:
			s.state.Phase = "done"
		case errors.Is(err, context.Canceled):
			s.state.Phase = "cancelled"
		default:
			s.state.Phase = "failed"
			s.state.Error = err.Error()
		}
		s.mu.Unlock()
	}()
	return s.id, nil
}

// GetScanState 返回进度快照；扫描结束前可反复调用。
func (a *App) GetScanState(id string) (ScanState, error) {
	s, err := a.session(id)
	if err != nil {
		return ScanState{}, err
	}
	return s.snapshot(), nil
}

// CancelScan 请求取消扫描；已完成的扫描调用无效。
func (a *App) CancelScan(id string) error {
	s, err := a.session(id)
	if err != nil {
		return err
	}
	s.cancel()
	s.mu.Lock()
	if s.state.Phase == "running" {
		s.state.Phase = "cancelling"
	}
	s.mu.Unlock()
	return nil
}

// GetGroups 分页返回重复组。每组携带全部成员，便于一次展开。
func (a *App) GetGroups(id string, offset, limit int) (GroupsPage, error) {
	s, err := a.session(id)
	if err != nil {
		return GroupsPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return GroupsPage{}, errors.New("扫描尚未完成")
	}
	groups := s.result.Groups
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset > len(groups) {
		offset = len(groups)
	}
	end := offset + limit
	if end > len(groups) {
		end = len(groups)
	}
	page := GroupsPage{Total: len(groups), Offset: offset, Items: make([]GroupView, 0, end-offset)}
	for _, g := range groups[offset:end] {
		view := GroupView{
			ID:          g.ID,
			Kind:        string(g.Kind),
			Keep:        g.Keep,
			Reclaimable: g.Reclaimable,
			Members:     make([]MemberView, 0, len(g.Members)),
		}
		for _, m := range g.Members {
			view.Members = append(view.Members, MemberView{
				Path:      m.Path,
				Name:      filepath.Base(m.Path),
				Size:      m.Size,
				ModTime:   m.ModTime,
				Keep:      m.Keep,
				Deletable: m.Deletable,
			})
		}
		page.Items = append(page.Items, view)
	}
	return page, nil
}

// GetSkipped 分页返回扫描期间跳过的文件。
func (a *App) GetSkipped(id string, offset, limit int) (SkippedPage, error) {
	s, err := a.session(id)
	if err != nil {
		return SkippedPage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return SkippedPage{}, errors.New("扫描尚未完成")
	}
	all := s.result.Skipped
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	if offset > len(all) {
		offset = len(all)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	page := SkippedPage{Total: len(all), Offset: offset, Items: make([]SkipView, 0, end-offset)}
	for _, sk := range all[offset:end] {
		page.Items = append(page.Items, SkipView{Path: sk.Path, Code: string(sk.Code), Message: sk.Message})
	}
	return page, nil
}

// PreviewPlan 生成执行计划并返回摘要与前 200 条动作。执行前必看。
//
// deletePaths 为 nil 时删除每组全部可删除成员（兼容旧界面）；
// 非 nil 时只删除勾选路径，空切片表示一个都不删。
func (a *App) PreviewPlan(id, deleteMode string, deletePaths []string) (PlanPreview, error) {
	s, err := a.session(id)
	if err != nil {
		return PlanPreview{}, err
	}
	mode, err := parseDeleteMode(deleteMode)
	if err != nil {
		return PlanPreview{}, err
	}
	p, err := s.planForPaths(mode, deletePaths)
	if err != nil {
		return PlanPreview{}, err
	}
	preview := PlanPreview{
		RequestID:  p.RequestID,
		DeleteMode: string(mode),
		Total:      len(p.Actions),
		KindCounts: map[string]int{},
	}
	if deletePaths == nil {
		s.mu.Lock()
		if s.result != nil {
			preview.Groups = len(s.result.Groups)
			preview.ReclaimableBytes = s.result.Stats.ReclaimableBytes
		}
		s.mu.Unlock()
	} else {
		picked := make(map[string]bool, len(deletePaths))
		for _, path := range deletePaths {
			picked[path] = true
		}
		s.mu.Lock()
		if s.result != nil {
			for _, g := range s.result.Groups {
				for _, m := range g.Members {
					if picked[m.Path] {
						preview.Groups++
						break
					}
				}
			}
		}
		s.mu.Unlock()
	}
	for i, act := range p.Actions {
		preview.KindCounts[string(act.Kind)]++
		var size int64
		if act.Precheck != nil {
			size = act.Precheck.Size
		}
		if deletePaths != nil {
			preview.ReclaimableBytes += size
		}
		if i < 200 {
			preview.Samples = append(preview.Samples, ActionView{
				Seq: act.Seq, Kind: string(act.Kind), Src: act.Src, Reason: act.Reason, Size: size,
			})
		}
	}
	return preview, nil
}

// StartExecute 异步执行计划；confirmed=false 时拒绝执行（前端确认框之外的第二道闸）。
//
// deletePaths 语义同 PreviewPlan，必须与预览时的勾选一致，否则执行范围会变。
func (a *App) StartExecute(id, deleteMode string, deletePaths []string, confirmed bool) error {
	if !confirmed {
		return errors.New("未确认，拒绝执行")
	}
	if err := requireLicense(auth.FeatureApply); err != nil {
		return err
	}
	s, err := a.session(id)
	if err != nil {
		return err
	}
	mode, err := parseDeleteMode(deleteMode)
	if err != nil {
		return err
	}
	p, err := s.planForPaths(mode, deletePaths)
	if err != nil {
		return err
	}
	if len(p.Actions) == 0 {
		return errors.New("计划为空，没有可执行的动作")
	}
	if err := plan.Validate(p); err != nil {
		return fmt.Errorf("计划校验失败：%w", err)
	}

	s.mu.Lock()
	if s.executing {
		s.mu.Unlock()
		return errors.New("已有执行任务在进行")
	}
	s.executing = true
	ctx, cancel := context.WithCancel(context.Background())
	s.execCancelling = cancel
	s.exec = ExecuteState{Running: true, Total: len(p.Actions)}
	s.mu.Unlock()

	go func() {
		defer cancel()
		exec := &apply.Executor{Trash: platform.Trash}
		if writer, jerr := openJournal(p.RequestID); jerr == nil {
			exec.Journal = writer
			defer writer.Close()
		}
		rep, execErr := exec.Execute(ctx, p, apply.Options{Now: time.Now})
		s.mu.Lock()
		defer s.mu.Unlock()
		s.executing = false
		s.exec.Running = false
		s.exec.Done = true
		s.exec.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		if rep != nil {
			s.exec.Total = rep.Total
			s.exec.Applied = rep.Applied
			s.exec.Failed = rep.Failed
			s.exec.Skipped = rep.Skipped
			s.exec.RolledBack = rep.RolledBack
			for _, act := range rep.Actions {
				if act.Status != model.StatusApplied {
					continue
				}
				switch act.Kind {
				case model.ActionDeleteTrash, model.ActionDeletePermanent:
					if act.Precheck != nil && act.Precheck.Size > 0 {
						s.exec.Reclaimed += act.Precheck.Size
					}
				}
			}
		}
		if execErr != nil {
			s.exec.Error = execErr.Error()
		}
	}()
	return nil
}

// GetExecuteState 轮询执行进度与最终统计。
func (a *App) GetExecuteState(id string) (ExecuteState, error) {
	s, err := a.session(id)
	if err != nil {
		return ExecuteState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exec, nil
}

func (s *scanSession) planFor(mode model.DeleteMode) (*model.Plan, error) {
	return s.planForPaths(mode, nil)
}

// planForPaths 生成计划。deletePaths == nil 时复用缓存（默认全删）；
// 显式勾选不缓存，因为每次勾选调整都对应不同范围。
func (s *scanSession) planForPaths(mode model.DeleteMode, deletePaths []string) (*model.Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return nil, errors.New("扫描尚未完成")
	}
	if deletePaths != nil {
		return plan.FromScan(s.result, plan.Options{
			RequestID: s.result.RequestID, DeleteMode: mode, DeletePaths: deletePaths,
		})
	}
	if s.plan != nil && s.planMode == mode {
		return s.plan, nil
	}
	p, err := plan.FromScan(s.result, plan.Options{RequestID: s.result.RequestID, DeleteMode: mode})
	if err != nil {
		return nil, err
	}
	s.plan = p
	s.planMode = mode
	return p, nil
}

func (a *App) session(id string) (*scanSession, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[id]
	if !ok {
		return nil, fmt.Errorf("找不到扫描会话：%s", id)
	}
	return s, nil
}

func parseDeleteMode(v string) (model.DeleteMode, error) {
	switch model.DeleteMode(v) {
	case "", model.DeleteTrash:
		return model.DeleteTrash, nil
	case model.DeletePermanent:
		return model.DeletePermanent, nil
	default:
		return "", fmt.Errorf("不支持的删除方式：%s", v)
	}
}

func openJournal(requestID string) (*logx.OpsWriter, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return logx.OpenOps(filepath.Join(dir, "FileDedup", "ops"), requestID, time.Now())
}
