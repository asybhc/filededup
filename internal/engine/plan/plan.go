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
}

var (
	// ErrNilResult 表示没有传入扫描结果。
	ErrNilResult = errors.New("plan: 扫描结果为空")
	// ErrGroupInvalid 表示重复组结构非法（保留项不在成员中、成员不足等）。
	ErrGroupInvalid = errors.New("plan: 重复组结构非法")
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

	p := &model.Plan{
		SchemaVersion: model.SchemaVersion,
		RequestID:     requestID,
		CreatedAt:     createdAt.Format(time.RFC3339Nano),
		Source:        model.PlanFromScan,
		Actions:       []model.Action{},
	}
	for _, g := range res.Groups {
		actions, err := groupActions(g, mode)
		if err != nil {
			return nil, err
		}
		p.Actions = append(p.Actions, actions...)
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
