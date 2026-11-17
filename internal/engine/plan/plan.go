// Package plan 把扫描结果转换为可审查、可导出、可回放的执行计划。
//
// 纯函数：不触碰文件系统、不读时钟（时钟可注入），输出完全由输入决定。
// 执行器在 engine/apply；本包不做任何删除决策以外的推断。
package plan

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"filededup/internal/engine/model"
)

// Options 是计划生成参数。零值 = 移入回收站 + 当前时间。
type Options struct {
	RequestID  string
	DeleteMode model.DeleteMode
	Now        func() time.Time
	// DeletePaths 非 nil 时按"用户显式勾选"生成动作（键为成员绝对路径）；
	// nil 表示沿用"删除组内全部 Deletable 成员"的默认行为。
	// 空切片是合法输入，表示一个都不删。
	DeletePaths []string
}

var (
	// ErrNilResult 表示没有传入扫描结果。
	ErrNilResult = errors.New("plan: 扫描结果为空")
	// ErrGroupInvalid 表示重复组结构非法（保留项不在成员中、成员不足等）。
	ErrGroupInvalid = errors.New("plan: 重复组结构非法")
	// ErrGroupFullySelected 表示某组的所有成员都被勾选删除，至少必须保留一个。
	ErrGroupFullySelected = errors.New("plan: 勾选会删除整组文件")
	// ErrSelectionUnknown 表示勾选路径不属于任何重复组（扫描结果可能已过期）。
	ErrSelectionUnknown = errors.New("plan: 勾选了不属于任何重复组的文件")
)

// FromScan 生成"删除组内可删除成员"的计划。
//
// 只处理扫描结果，不改写保留策略：保留项由扫描阶段的 KeepPolicy 决定，
// 计划阶段只把 Deletable 成员转换成动作，避免"扫描与执行各有一套策略"。
func FromScan(res *model.Result, opt Options) (*model.Plan, error) {
	if res == nil {
		return nil, ErrNilResult
	}
	mode := opt.DeleteMode
	if mode == "" {
		mode = model.DeleteTrash
	}
	if mode != model.DeleteTrash && mode != model.DeletePermanent {
		return nil, fmt.Errorf("plan: 未知删除方式 %q", mode)
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	createdAt := now().UTC()
	requestID := opt.RequestID
	if requestID == "" {
		requestID = res.RequestID
	}
	if requestID == "" {
		// 扫描结果未带 requestId 时生成计划 ID，保证操作清单可追溯。
		requestID = "plan-" + createdAt.Format("20060102T150405Z")
	}

	var selected map[string]bool
	if opt.DeletePaths != nil {
		selected = make(map[string]bool, len(opt.DeletePaths))
		for _, path := range opt.DeletePaths {
			if path != "" {
				selected[path] = true
			}
		}
	}

	p := &model.Plan{
		SchemaVersion: model.SchemaVersion,
		RequestID:     requestID,
		CreatedAt:     createdAt.Format(time.RFC3339Nano),
		Source:        model.PlanFromScan,
		Roots:         append([]string(nil), res.Request.Roots...),
		Actions:       []model.Action{},
	}
	known := map[string]bool{}
	for _, g := range res.Groups {
		var actions []model.Action
		var err error
		if selected != nil {
			actions, err = groupActionsSelected(g, mode, selected, known)
		} else {
			actions, err = groupActions(g, mode)
		}
		if err != nil {
			return nil, err
		}
		p.Actions = append(p.Actions, actions...)
	}
	if selected != nil {
		var unknown []string
		for path := range selected {
			if !known[path] {
				unknown = append(unknown, path)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return nil, fmt.Errorf("%w：%q 等 %d 个", ErrSelectionUnknown, unknown[0], len(unknown))
		}
	}
	for i := range p.Actions {
		p.Actions[i].Seq = i + 1
	}
	p.Summarize()
	return p, nil
}

// groupActions 把一个重复组转换为删除动作，顺序由路径决定（确定性）。
func groupActions(g model.Group, mode model.DeleteMode) ([]model.Action, error) {
	if len(g.Members) < 2 {
		return nil, fmt.Errorf("%w: 组 %s 成员不足 2", ErrGroupInvalid, g.ID)
	}
	keepFound := false
	for _, m := range g.Members {
		if m.Path == g.Keep {
			keepFound = true
			break
		}
	}
	if !keepFound {
		return nil, fmt.Errorf("%w: 组 %s 的保留项 %q 不在成员中", ErrGroupInvalid, g.ID, g.Keep)
	}

	kind := model.ActionDeleteTrash
	if mode == model.DeletePermanent {
		kind = model.ActionDeletePermanent
	}
	members := make([]model.Member, len(g.Members))
	copy(members, g.Members)
	sort.SliceStable(members, func(i, j int) bool { return members[i].Path < members[j].Path })

	actions := make([]model.Action, 0, len(members)-1)
	for _, m := range members {
		if m.Path == g.Keep || !m.Deletable {
			continue
		}
		pre := m.Snapshot
		if pre == nil {
			pre = &model.Precheck{Size: m.Size, ModTime: m.ModTime, Hash: m.Hash}
		}
		actions = append(actions, model.Action{
			Kind:         kind,
			Src:          m.Path,
			Reason:       fmt.Sprintf("%s duplicate; keep=%s", g.Kind, g.Keep),
			Irreversible: kind == model.ActionDeletePermanent,
			Precheck:     pre,
			Rollback:     model.RollbackNone,
			Status:       model.StatusPlanned,
		})
	}
	return actions, nil
}

// groupActionsSelected 只把用户显式勾选的成员转换为删除动作。
//
// 与 groupActions 的区别：允许删掉保留策略选中的成员（用户可改留另一份），
// 但不允许把整组都勾选删除；known 收集本组出现过的全部路径，供调用方校验未知勾选。
func groupActionsSelected(g model.Group, mode model.DeleteMode, selected, known map[string]bool) ([]model.Action, error) {
	if len(g.Members) < 2 {
		return nil, fmt.Errorf("%w: 组 %s 成员不足 2", ErrGroupInvalid, g.ID)
	}
	keepFound := false
	for _, m := range g.Members {
		known[m.Path] = true
		if m.Path == g.Keep {
			keepFound = true
		}
	}
	if !keepFound {
		return nil, fmt.Errorf("%w: 组 %s 的保留项 %q 不在成员中", ErrGroupInvalid, g.ID, g.Keep)
	}

	picked := 0
	for _, m := range g.Members {
		if selected[m.Path] {
			picked++
		}
	}
	if picked == len(g.Members) {
		return nil, fmt.Errorf("%w：组 %s 共 %d 个成员全部被勾选，至少保留一个", ErrGroupFullySelected, g.ID, len(g.Members))
	}
	if picked == 0 {
		return nil, nil
	}

	kind := model.ActionDeleteTrash
	if mode == model.DeletePermanent {
		kind = model.ActionDeletePermanent
	}
	members := make([]model.Member, len(g.Members))
	copy(members, g.Members)
	sort.SliceStable(members, func(i, j int) bool { return members[i].Path < members[j].Path })

	actions := make([]model.Action, 0, picked)
	for _, m := range members {
		if !selected[m.Path] {
			continue
		}
		pre := m.Snapshot
		if pre == nil {
			pre = &model.Precheck{Size: m.Size, ModTime: m.ModTime, Hash: m.Hash}
		}
		actions = append(actions, model.Action{
			Kind:         kind,
			Src:          m.Path,
			Reason:       fmt.Sprintf("%s duplicate; explicitly selected; keep=%s", g.Kind, g.Keep),
			Irreversible: kind == model.ActionDeletePermanent,
			Precheck:     pre,
			Rollback:     model.RollbackNone,
			Status:       model.StatusPlanned,
		})
	}
	return actions, nil
}

// Validate 校验计划能否安全执行：契约版本、序号连续、动作类型受支持、删除动作带预检。
func Validate(p *model.Plan) error {
	if p == nil {
		return errors.New("plan: 计划为空")
	}
	if p.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("plan: schemaVersion 不支持：%d（期望 %d）", p.SchemaVersion, model.SchemaVersion)
	}
	seen := make(map[int]bool, len(p.Actions))
	for i, a := range p.Actions {
		if a.Seq != i+1 {
			return fmt.Errorf("plan: 第 %d 条动作序号为 %d，应为 %d", i+1, a.Seq, i+1)
		}
		if seen[a.Seq] {
			return fmt.Errorf("plan: 序号重复：%d", a.Seq)
		}
		seen[a.Seq] = true
		if a.Src == "" {
			return fmt.Errorf("plan: 第 %d 条动作缺少 src", a.Seq)
		}
		switch a.Kind {
		case model.ActionDeleteTrash, model.ActionDeletePermanent:
			if a.Precheck == nil {
				return fmt.Errorf("plan: 第 %d 条删除动作缺少 precheck", a.Seq)
			}
			if a.Irreversible != (a.Kind == model.ActionDeletePermanent) {
				return fmt.Errorf("plan: 第 %d 条动作 irreversible 标记与类型不符", a.Seq)
			}
		case model.ActionMove, model.ActionRename, model.ActionHardlink:
			if a.Dst == "" {
				return fmt.Errorf("plan: 第 %d 条动作缺少 dst", a.Seq)
			}
		case model.ActionClearReadonly, model.ActionMkdir, model.ActionRemoveDir:
		default:
			return fmt.Errorf("plan: 第 %d 条动作类型不支持：%q", a.Seq, a.Kind)
		}
		if a.Status != model.StatusPlanned {
			return fmt.Errorf("plan: 第 %d 条动作状态应为 planned，实际 %q", a.Seq, a.Status)
		}
	}
	return nil
}
