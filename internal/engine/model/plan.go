package model

// PlanSource 标识计划来源。
type PlanSource string

const (
	PlanFromScan     PlanSource = "scan"
	PlanFromOrganize PlanSource = "organize"
)

// PlanSummary 是给用户确认用的汇总。UI 必须展示：将删除 N 项、释放 X、不可逆项 M。
type PlanSummary struct {
	Actions          int   `json:"actions"`
	Deletes          int   `json:"deletes"` // trash + permanent
	Trash            int   `json:"trash"`
	Permanent        int   `json:"permanent"`
	Moves            int   `json:"moves"`
	Renames          int   `json:"renames"`
	Hardlinks        int   `json:"hardlinks"`
	ClearReadonly    int   `json:"clearReadonly"`
	RemoveDirs       int   `json:"removeDirs"`
	Irreversible     int   `json:"irreversible"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
}

// Plan 是 Scan/Organize 与 Execute 之间的唯一中间产物，可导出为 JSON 并 diff。
type Plan struct {
	SchemaVersion int         `json:"schemaVersion"`
	RequestID     string      `json:"requestId"`
	CreatedAt     string      `json:"createdAt"` // UTC RFC3339Nano
	Source        PlanSource  `json:"source"`
	Actions       []Action    `json:"actions"`
	Summary       PlanSummary `json:"summary"`
}

// Summarize 依据 Actions 计算 Summary。执行器与 UI 都必须使用同一实现。
func (p *Plan) Summarize() {
	s := PlanSummary{Actions: len(p.Actions)}
	for _, a := range p.Actions {
		switch a.Kind {
		case ActionDeleteTrash:
			s.Deletes++
			s.Trash++
		case ActionDeletePermanent:
			s.Deletes++
			s.Permanent++
		case ActionMove:
			s.Moves++
		case ActionRename:
			s.Renames++
		case ActionHardlink:
			s.Hardlinks++
		case ActionClearReadonly:
			s.ClearReadonly++
		case ActionRemoveDir:
			s.RemoveDirs++
		}
		if a.Irreversible {
			s.Irreversible++
		}
	}
	p.Summary = s
}
