package model

// ActionKind 是执行动作类型。字符串值写入 JSON，冻结。
type ActionKind string

const (
	ActionRename          ActionKind = "rename"           // 同目录改名
	ActionMove            ActionKind = "move"             // 跨目录移动
	ActionDeleteTrash     ActionKind = "delete_trash"     // 移入回收站
	ActionDeletePermanent ActionKind = "delete_permanent" // 永久删除（不可逆）
	ActionHardlink        ActionKind = "hardlink"         // 硬链接合并替代删除
	ActionClearReadonly   ActionKind = "clear_readonly"   // 去除只读属性
	ActionMkdir           ActionKind = "mkdir"            // 创建目标目录
	ActionRemoveDir       ActionKind = "remove_dir"       // 删除空目录
)

// ActionStatus 是动作状态。字符串值写入 JSON，冻结。
type ActionStatus string

const (
	StatusPlanned ActionStatus = "planned"
	StatusApplied ActionStatus = "applied"
	StatusSkipped ActionStatus = "skipped"
	StatusFailed  ActionStatus = "failed"
	StatusDryRun  ActionStatus = "dry_run"
)

// RollbackKind 描述失败后的回滚方式。
type RollbackKind string

const (
	RollbackNone        RollbackKind = "none"         // 不可回滚（永久删除）
	RollbackRenameBack  RollbackKind = "rename_back"  // 改名/移动：反向移动
	RollbackDropLink    RollbackKind = "drop_link"    // 硬链接：删除新链接
	RollbackRestoreAttr RollbackKind = "restore_attr" // 只读属性：恢复原属性
	RollbackRemoveDir   RollbackKind = "remove_dir"   // 新建目录：删除空目录
)

// Precheck 是执行前复核所需的文件指纹。
// C# 对应 A/Z.cs 的扫描期快照（R9/C-03/R8）。
type Precheck struct {
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // UTC RFC3339Nano
	Hash    string `json:"hash,omitempty"`
}

// Action 是 Plan 与整理器共用的最小执行单元。
type Action struct {
	Seq          int          `json:"seq"`
	Step         string       `json:"step,omitempty"` // 整理步骤 ID；扫描计划为空
	Kind         ActionKind   `json:"kind"`
	Src          string       `json:"src"`
	Dst          string       `json:"dst,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Irreversible bool         `json:"irreversible"`
	Precheck     *Precheck    `json:"precheck,omitempty"`
	Rollback     RollbackKind `json:"rollback"`
	Status       ActionStatus `json:"status"`
	Err          string       `json:"error,omitempty"`
}
