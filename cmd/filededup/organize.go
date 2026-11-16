//go:build desktop

package main

// 整理器界面后端：把九步整理接进 UI。
//
// 与 CLI（cmd/fdedup organize）走的是同一套引擎调用：
// orgscan.Collect → PrepareProviders → Snapshot →（可选）BuildPreviewPlan → Plan → Apply。
// 两套执行顺序直接复用引擎的 OrderStandalone / OrderPreview，不在 UI 层重新排列。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"filededup/internal/auth"
	"filededup/internal/engine/model"
	"filededup/internal/engine/organize"
	"filededup/internal/orgscan"
	"filededup/internal/platform"
)

// OrganizeSteps 是九步开关的桥接类型。
type OrganizeSteps struct {
	RenamePrefix  bool `json:"renamePrefix"`
	RenameSuffix  bool `json:"renameSuffix"`
	SpecialDelete bool `json:"specialDelete"`
	Readonly      bool `json:"readonly"`
	ContentDedup  bool `json:"contentDedup"`
	ImageDedup    bool `json:"imageDedup"`
	AudioDedup    bool `json:"audioDedup"`
	Organize      bool `json:"organize"`
	EmptyDirs     bool `json:"emptyDirs"`
}

// DefaultOrganizeSteps 跟随引擎的原版默认开关。
func DefaultOrganizeSteps() OrganizeSteps {
	return stepsFromSwitches(organize.DefaultStepSwitches())
}

func stepsFromSwitches(s organize.StepSwitches) OrganizeSteps {
	return OrganizeSteps{
		RenamePrefix:  s.RenamePrefix,
		RenameSuffix:  s.RenameSuffix,
		SpecialDelete: s.SpecialDelete,
		Readonly:      s.Readonly,
		ContentDedup:  s.ContentDedup,
		ImageDedup:    s.ImageDedup,
		AudioDedup:    s.AudioDedup,
		Organize:      s.Organize,
		EmptyDirs:     s.EmptyDirs,
	}
}

func (s OrganizeSteps) toSwitches() organize.StepSwitches {
	return organize.StepSwitches{
		RenamePrefix:  s.RenamePrefix,
		RenameSuffix:  s.RenameSuffix,
		SpecialDelete: s.SpecialDelete,
		Readonly:      s.Readonly,
		ContentDedup:  s.ContentDedup,
		ImageDedup:    s.ImageDedup,
		AudioDedup:    s.AudioDedup,
		Organize:      s.Organize,
		EmptyDirs:     s.EmptyDirs,
	}
}

func (s OrganizeSteps) anyOn() bool {
	return s.RenamePrefix || s.RenameSuffix || s.SpecialDelete || s.Readonly ||
		s.ContentDedup || s.ImageDedup || s.AudioDedup || s.Organize || s.EmptyDirs
}

var validOrganizeModes = map[string]bool{
	"category": true, "extension": true, "namefeature": true, "keyword": true, "fixedfolder": true,
}

var validConflictPolicies = map[string]bool{"overwrite": true, "skip": true, "rename": true}

// OrganizeRequest 是整理页的一次执行请求。
type OrganizeRequest struct {
	Root           string `json:"root"`
	Target         string `json:"target"`
	UsePreviewPlan bool   `json:"usePreviewPlan"`
	DryRun         bool   `json:"dryRun"`
	// PlanOnly 只做扫描与规划，不创建目录、不改任何文件；用于先收集存疑项。
	PlanOnly        bool          `json:"planOnly"`
	Steps           OrganizeSteps `json:"steps"`
	Mode            string        `json:"mode"`
	Keyword         string        `json:"keyword"`
	Regex           string        `json:"regex"`
	FixedDir        string        `json:"fixedDir"`
	Conflict        string        `json:"conflict"`
	DeleteMode      string        `json:"deleteMode"`
	KeepPolicy      string        `json:"keepPolicy"`
	ImageKeepPolicy string        `json:"imageKeepPolicy"`
	ImageThreshold  int           `json:"imageThreshold"`
	AudioTolerance  int           `json:"audioTolerance"`
	Strength        int           `json:"strength"`
	MinSize         int64         `json:"minSize"`
	IncludeHidden   bool          `json:"includeHidden"`
	Excludes        []string      `json:"excludes"`
	ProtectWords    string        `json:"protectWords"`
	Hardlink        bool          `json:"hardlink"`
	ContinueOnError bool          `json:"continueOnError"`
	// ConfirmedSuspects 是用户逐条确认要删除的存疑项路径（规划期的当前相对路径）。
	// 只有出现在这里的存疑项才会在本次执行中删除；未列出的一律保留。
	ConfirmedSuspects []string `json:"confirmedSuspects"`
}

// OrganizeStepView 是单个步骤的执行结果，Skipped 表示该步被关闭或未启用。
type OrganizeStepView struct {
	ID         string           `json:"id"`
	Title      string           `json:"title"`
	Order      int              `json:"order"`
	Enabled    bool             `json:"enabled"`
	Skipped    bool             `json:"skipped"`
	SkipReason string           `json:"skipReason,omitempty"`
	ActionNum  int              `json:"actionNum"`
	Counters   organize.Counter `json:"counters"`
}

// OrganizeActionView 是动作样例（只回传前若干条，避免一次塞太多）。
// Mode 仅对 kind=move 有意义：同卷 move / 跨卷 copy / 无法判断 unknown。
type OrganizeActionView struct {
	Kind   string `json:"kind"`
	Step   string `json:"step"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Reason string `json:"reason,omitempty"`
	Rule   string `json:"rule,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

// OrganizeSuspectView 是存疑项；Key 为稳定的确认键（大小写折叠后的路径）。
type OrganizeSuspectView struct {
	Key        string `json:"key"`
	Path       string `json:"path"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
	Normalized string `json:"normalized,omitempty"`
	Score      int    `json:"score"`
}

// OrganizeExecView 是落盘阶段的统计。
type OrganizeExecView struct {
	Total   int      `json:"total"`
	Applied int      `json:"applied"`
	Failed  int      `json:"failed"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

// OrganizeState 是一次整理任务的完整状态；界面按 400ms 轮询。
type OrganizeState struct {
	ID             string `json:"id"`
	Phase          string `json:"phase"` // running | done | failed
	Stage          string `json:"stage"`
	DryRun         bool   `json:"dryRun"`
	Order          string `json:"order"` // standalone | preview
	UsePreviewPlan bool   `json:"usePreviewPlan"`
	// TargetDir 是规范化后的整理目标（根内相对路径或根外绝对路径），供界面展示。
	TargetDir      string `json:"targetDir"`
	TargetExternal bool   `json:"targetExternal"`
	// MoveMode 是整理目标与根目录的卷关系：move / copy / unknown（空表示未启用整理步骤）。
	MoveMode string `json:"moveMode"`
	// OpenDir 是整理完成后可在文件管理器中打开的绝对路径（整理步骤关闭或未设目标时为空）。
	OpenDir     string                `json:"openDir,omitempty"`
	ElapsedMS   int64                 `json:"elapsedMs"`
	Error       string                `json:"error,omitempty"`
	Steps       []OrganizeStepView    `json:"steps"`
	Counters    organize.Counter      `json:"counters"`
	Exec        OrganizeExecView      `json:"exec"`
	ActionTotal int                   `json:"actionTotal"`
	Actions     []OrganizeActionView  `json:"actions"`
	Suspects    []OrganizeSuspectView `json:"suspects"`
}

// OrganizeStepMeta 只用于界面渲染步骤顺序与标题。
type OrganizeStepMeta struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// OrganizeMeta 把引擎的两套顺序原样交给界面。
type OrganizeMeta struct {
	Standalone []OrganizeStepMeta `json:"standalone"`
	Preview    []OrganizeStepMeta `json:"preview"`
}

type organizeJob struct {
	mu    sync.Mutex
	state OrganizeState
}

func (j *organizeJob) update(fn func(*OrganizeState)) {
	j.mu.Lock()
	defer j.mu.Unlock()
	fn(&j.state)
}

func (j *organizeJob) snapshot() OrganizeState {
	j.mu.Lock()
	defer j.mu.Unlock()
	cp := j.state
	cp.Steps = append([]OrganizeStepView(nil), j.state.Steps...)
	cp.Actions = append([]OrganizeActionView(nil), j.state.Actions...)
	cp.Suspects = append([]OrganizeSuspectView(nil), j.state.Suspects...)
	cp.Exec.Errors = append([]string(nil), j.state.Exec.Errors...)
	return cp
}

// GetOrganizeMeta 返回两套步骤顺序。
// 实现方式是对空快照跑一次 Plan——不复制引擎里的顺序表，避免界面与引擎漂移。
func (a *App) GetOrganizeMeta() OrganizeMeta {
	var meta OrganizeMeta
	all := OrganizeSteps{
		RenamePrefix: true, RenameSuffix: true, SpecialDelete: true, Readonly: true,
		ContentDedup: true, ImageDedup: true, AudioDedup: true, Organize: true, EmptyDirs: true,
	}
	for _, mode := range []organize.OrderMode{organize.OrderStandalone, organize.OrderPreview} {
		cfg := organize.DefaultConfig()
		cfg.Order = mode
		cfg.Steps = all.toSwitches()
		res := organize.Plan(cfg, organize.Snapshot{Root: "."}, organize.Providers{})
		steps := make([]OrganizeStepMeta, 0, len(res.Steps))
		for _, sp := range res.Steps {
			steps = append(steps, OrganizeStepMeta{ID: sp.ID, Title: sp.Title})
		}
		if mode == organize.OrderStandalone {
			meta.Standalone = steps
		} else {
			meta.Preview = steps
		}
	}
	return meta
}

// StartOrganize 校验参数并异步执行。返回任务 ID，结果用 GetOrganizeState 轮询。
func (a *App) StartOrganize(req OrganizeRequest) (string, error) {
	absRoot, err := filepath.Abs(strings.TrimSpace(req.Root))
	if err != nil {
		return "", fmt.Errorf("根目录无效：%w", err)
	}
	if absRoot == "" {
		return "", errors.New("请先选择要整理的目录")
	}
	info, err := os.Stat(absRoot)
	if err != nil {
		return "", fmt.Errorf("根目录不可访问：%w", err)
	}
	if !info.IsDir() {
		return "", errors.New("根目录不是文件夹")
	}
	if err := rejectDangerousRoot(absRoot, "整理"); err != nil {
		return "", err
	}
	steps := req.Steps.toSwitches()
	if !req.Steps.anyOn() {
		return "", errors.New("至少启用一个步骤")
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if !validOrganizeModes[mode] {
		return "", fmt.Errorf("未知整理方式：%s", req.Mode)
	}
	conflict := strings.ToLower(strings.TrimSpace(req.Conflict))
	if !validConflictPolicies[conflict] {
		return "", fmt.Errorf("未知冲突策略：%s", req.Conflict)
	}
	targetSpec, err := organize.ResolveTarget(absRoot, req.Target)
	if err != nil {
		return "", err
	}
	if !req.DryRun && !req.PlanOnly {
		if err := requireLicense(auth.FeatureOrganize); err != nil {
			return "", err
		}
	}

	confirmed := make(map[string]bool, len(req.ConfirmedSuspects))
	for _, p := range req.ConfirmedSuspects {
		if p = strings.TrimSpace(p); p != "" {
			confirmed[organize.Fold(p)] = true
		}
	}

	cfg := organize.Config{
		Order:               organize.OrderStandalone,
		Steps:               steps,
		DryRun:              req.DryRun,
		Conflict:            organize.ParseConflictPolicy(conflict),
		Delete:              organize.ParseDeleteMode(req.DeleteMode),
		HardlinkMerge:       req.Hardlink,
		Excludes:            cleanList(req.Excludes),
		MinSize:             maxInt64(req.MinSize, 0),
		SkipHiddenSystem:    !req.IncludeHidden,
		ProtectWords:        req.ProtectWords,
		Strength:            organize.ParseStrength(req.Strength),
		LocalWhitelist:      true,
		KeepRule:            organize.ParseKeepRule(req.KeepPolicy),
		ImageKeepRule:       organize.ParseKeepRule(req.ImageKeepPolicy),
		ImageThreshold:      clampInt(req.ImageThreshold, 0, model.MaxImageThreshold, 8),
		AudioToleranceSec:   clampInt(req.AudioTolerance, 0, 60, 2),
		OrganizeMode:        organize.ParseOrganizeMode(mode),
		Keyword:             req.Keyword,
		Regex:               req.Regex,
		FixedFolder:         req.FixedDir,
		ConfirmedKeys:       confirmed,
		AutoDeleteConfirmed: len(confirmed) > 0,
	}

	moveMode := organizeMoveMode(absRoot, targetSpec, req.Steps.Organize)

	job := &organizeJob{state: OrganizeState{
		ID:             newJobID(),
		Phase:          "running",
		Stage:          "枚举目录",
		DryRun:         req.DryRun,
		Order:          string(cfg.Order),
		UsePreviewPlan: req.UsePreviewPlan,
		TargetDir:      displayTarget(targetSpec),
		TargetExternal: targetSpec.External,
		MoveMode:       moveMode,
		OpenDir:        organizeOpenDir(absRoot, targetSpec, req.Steps.Organize),
		Steps:          []OrganizeStepView{},
		Actions:        []OrganizeActionView{},
		Suspects:       []OrganizeSuspectView{},
		Exec:           OrganizeExecView{Errors: []string{}},
	}}

	a.mu.Lock()
	if a.orgJobs == nil {
		a.orgJobs = make(map[string]*organizeJob)
	}
	a.orgJobs[job.state.ID] = job
	a.mu.Unlock()

	target := targetSpec
	usePreview := req.UsePreviewPlan
	dryRun := req.DryRun
	planOnly := req.PlanOnly
	continueOnError := req.ContinueOnError
	jobID := job.state.ID

	go func() {
		defer func() {
			if r := recover(); r != nil {
				job.update(func(s *OrganizeState) {
					s.Phase = "failed"
					s.Error = fmt.Sprintf("内部错误：%v", r)
					s.Stage = ""
				})
			}
		}()
		start := time.Now()
		finish := func(s *OrganizeState) {
			s.ElapsedMS = time.Since(start).Milliseconds()
			s.Stage = ""
		}

		tree, err := orgscan.Collect(absRoot)
		if err != nil {
			job.update(func(s *OrganizeState) {
				s.Phase = "failed"
				s.Error = "枚举目录失败：" + err.Error()
				finish(s)
			})
			return
		}
		providers := orgscan.PrepareProviders(tree.Entries, absRoot, cfg)
		snap := organize.Snapshot{Root: absRoot, Target: target.Rel, Files: tree.Entries, Dirs: tree.Dirs}

		job.update(func(s *OrganizeState) { s.Stage = "规划动作" })
		if usePreview {
			job.update(func(s *OrganizeState) { s.Stage = "生成预览计划（先去重后整理）" })
			p := organize.BuildPreviewPlan(cfg, snap, providers)
			snap.PreviewPlan = p.Map()
			cfg.Order = organize.OrderPreview
		}
		res := organize.Plan(cfg, snap, providers)
		if target.External {
			organize.RewriteExternalTarget(&res, target.Abs)
		}

		job.update(func(s *OrganizeState) {
			s.Order = string(res.Order)
			s.Steps = stepViews(res)
			s.Counters = res.Counters
			total := 0
			sample := make([]OrganizeActionView, 0, 200)
			for _, sp := range res.Steps {
				total += len(sp.Actions)
				for _, act := range sp.Actions {
					if len(sample) >= 200 {
						break
					}
					sample = append(sample, OrganizeActionView{
						Kind: string(act.Kind), Step: sp.Title,
						From: act.From, To: act.To, Reason: act.Reason, Rule: act.Rule,
						Mode: actionMoveMode(act.Kind, act.To, target, moveMode),
					})
				}
			}
			s.ActionTotal = total
			s.Actions = sample
			s.Suspects = suspectViews(res)
			if !planOnly {
				s.Stage = "执行动作"
			}
		})

		if planOnly {
			job.update(func(s *OrganizeState) {
				s.Phase = "done"
				finish(s)
			})
			return
		}

		journal := &organizeUndoJournal{}
		hooks := platformHooks(cfg.Delete)
		hooks.Undo = journal.record
		rep, err := organize.Apply(absRoot, res, organize.ExecOptions{
			DryRun:          dryRun,
			ContinueOnError: continueOnError,
		}, hooks)

		job.update(func(s *OrganizeState) {
			if err != nil {
				s.Phase = "failed"
				s.Error = "执行失败：" + err.Error()
				finish(s)
				return
			}
			s.Phase = "done"
			s.Exec = OrganizeExecView{
				Total: rep.Total, Applied: rep.Applied, Failed: rep.Failed, Skipped: rep.Skipped,
				Errors: append([]string(nil), rep.Errors...),
			}
			finish(s)
		})

		if dryRun {
			return
		}
		recs := journal.snapshot()
		if len(recs) == 0 {
			return
		}
		if err := saveOrganizeUndo(jobID, absRoot, start, recs); err != nil {
			job.update(func(s *OrganizeState) {
				s.Exec.Errors = append(s.Exec.Errors, "撤销记录写入失败："+err.Error())
			})
		}
	}()

	return job.state.ID, nil
}

// GetOrganizeState 返回任务状态；未知 ID 直接报错，避免界面静默卡住。
func (a *App) GetOrganizeState(id string) (OrganizeState, error) {
	a.mu.Lock()
	job := a.orgJobs[id]
	a.mu.Unlock()
	if job == nil {
		return OrganizeState{}, fmt.Errorf("整理任务 %s 不存在", id)
	}
	return job.snapshot(), nil
}

// platformHooks 注入平台能力；引擎本身不依赖 internal/platform。
func platformHooks(delete organize.DeleteMode) organize.ExecHooks {
	hooks := organize.ExecHooks{
		IsLocked:   platform.IsLocked,
		SameVolume: platform.SameVolume,
		Link:       platform.Link,
	}
	if delete == organize.DeleteTrash {
		hooks.Trash = platform.Trash
	}
	return hooks
}

func stepViews(res organize.Result) []OrganizeStepView {
	views := make([]OrganizeStepView, 0, len(res.Steps))
	for _, sp := range res.Steps {
		views = append(views, OrganizeStepView{
			ID: sp.ID, Title: sp.Title, Order: sp.Order,
			Enabled: !sp.Skipped, Skipped: sp.Skipped, SkipReason: sp.SkipReason,
			ActionNum: len(sp.Actions), Counters: sp.Counters,
		})
	}
	sort.SliceStable(views, func(i, j int) bool { return views[i].Order < views[j].Order })
	return views
}

// suspectViews 汇总所有步骤的存疑项，Key 与引擎确认键一致（Fold 后）。
func suspectViews(res organize.Result) []OrganizeSuspectView {
	out := make([]OrganizeSuspectView, 0)
	for _, sp := range res.Steps {
		for _, su := range sp.Suspects {
			out = append(out, OrganizeSuspectView{
				Key:        organize.Fold(su.Path),
				Path:       su.Path,
				Reason:     su.Reason,
				Suggestion: su.Suggestion,
				Normalized: su.Normalized,
				Score:      su.Score,
			})
		}
	}
	return out
}

// displayTarget 返回界面展示用的整理目标：根内显示相对路径，根外显示绝对路径。
func displayTarget(spec organize.TargetSpec) string {
	if spec.External {
		return spec.Abs
	}
	return spec.Rel
}

// organizeOpenDir 返回整理完成后「打开目标文件夹」应指向的绝对路径。
// 整理步骤关闭、或没有指定目标时返回空串（界面不显示该按钮）。
func organizeOpenDir(absRoot string, spec organize.TargetSpec, organizeOn bool) string {
	if !organizeOn || (spec.Rel == "" && spec.Abs == "") {
		return ""
	}
	if spec.External {
		return filepath.FromSlash(spec.Abs)
	}
	return filepath.Join(absRoot, filepath.FromSlash(spec.Rel))
}

// organizeMoveMode 判断整理目标与根目录的卷关系。
// 返回 move（同卷重命名）/ copy（跨卷复制）/ unknown（无法判断，执行期会如实报错）。
func organizeMoveMode(absRoot string, spec organize.TargetSpec, organizeOn bool) string {
	if spec.Rel == "" || !organizeOn {
		return ""
	}
	dest := spec.Rel
	if spec.External {
		dest = spec.Abs
	}
	same, err := platform.SameVolume(absRoot, filepath.FromSlash(dest))
	switch {
	case err != nil:
		return "unknown"
	case same:
		return "move"
	default:
		return "copy"
	}
}

// actionMoveMode 给动作样例标注移动/复制方式；只对 move 动作有意义。
func actionMoveMode(kind organize.ActionKind, to string, spec organize.TargetSpec, moveMode string) string {
	if kind != organize.ActionMove {
		return ""
	}
	if !spec.External {
		return "move"
	}
	base := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(spec.Abs)), "/")
	if to != base && !strings.HasPrefix(to, base+"/") {
		return "move"
	}
	if moveMode == "" {
		return "move"
	}
	return moveMode
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt64(v, lo int64) int64 {
	if v < lo {
		return lo
	}
	return v
}

func newJobID() string {
	return fmt.Sprintf("org-%d", time.Now().UnixNano())
}
