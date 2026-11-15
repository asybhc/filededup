package organize

import "time"

// OrderMode 整理器的执行顺序，对应原版是否存在“预览/确认计划”。
type OrderMode string

const (
	// OrderStandalone 无预览计划：改名 → 特殊删除 → 去只读 → 去重 → 整理 → 清空目录。
	OrderStandalone OrderMode = "standalone"
	// OrderPreview 有预览计划：先去重，再改名/删除/去只读，最后整理与清空目录。
	OrderPreview OrderMode = "preview"
)

// ConflictPolicy 冲突策略，对应原版 b.D 枚举（A/a/B）。
type ConflictPolicy string

const (
	ConflictOverwrite ConflictPolicy = "overwrite" // A：删除源文件，保留已存在目标
	ConflictSkip      ConflictPolicy = "skip"      // a：跳过并计数
	ConflictRename    ConflictPolicy = "rename"    // B：自动重命名 name (1).ext
)

// DeleteMode 删除方式，对应 a.A（回收站 / 永久删除）。
type DeleteMode string

const (
	DeleteTrash     DeleteMode = "trash"
	DeletePermanent DeleteMode = "permanent"
)

// OrganizeMode 第五步文件整理的分类方式。
type OrganizeMode string

const (
	ModeCategory    OrganizeMode = "category"    // 按文件类型分类
	ModeExtension   OrganizeMode = "extension"   // 按文件后缀名整理（扩展名大写）
	ModeNameFeature OrganizeMode = "nameFeature" // 按文件名字符特征整理（正则）
	ModeKeyword     OrganizeMode = "keyword"     // 按文件名整理（关键字包含）
	ModeFixedFolder OrganizeMode = "fixedFolder" // 按指定文件夹整理
)

// KeepRule 内容/音频去重的保留规则；空字符串等价于原版默认“保留路径最短”。
type KeepRule string

const (
	KeepShortestPath KeepRule = "shortestPath" // 默认：路径长度最短（再按路径长度稳定）
	KeepNewest       KeepRule = "newest"       // 保留最新
	KeepOldest       KeepRule = "oldest"       // 保留最旧
	KeepLargest      KeepRule = "largest"      // 保留最大文件
	KeepShortestName KeepRule = "shortestName" // 保留文件名最短
)

// StepSwitches 九步开关，默认值与 b/D.cs 一致。
type StepSwitches struct {
	RenamePrefix  bool
	RenameSuffix  bool
	SpecialDelete bool
	Readonly      bool
	ContentDedup  bool
	ImageDedup    bool
	AudioDedup    bool
	Organize      bool
	EmptyDirs     bool
}

// DefaultStepSwitches 返回原版默认开关。
func DefaultStepSwitches() StepSwitches {
	return StepSwitches{
		RenamePrefix:  true,
		RenameSuffix:  true,
		SpecialDelete: false,
		Readonly:      true,
		ContentDedup:  false,
		ImageDedup:    false,
		AudioDedup:    false,
		Organize:      true,
		EmptyDirs:     false,
	}
}

// FileEntry 是纯函数层的文件快照条目；Path 为相对根目录的路径（统一 '/'）。
type FileEntry struct {
	Path     string
	Size     int64
	ModTime  time.Time
	Readonly bool
	Hidden   bool
	System   bool
	SHA256   string
	DHash    uint64
	HasDHash bool
	AudioMs  int64
	HasAudio bool
}

// Snapshot 是某一时刻的文件系统快照。Target 为整理目标目录（相对根目录，可为空）。
type Snapshot struct {
	Root   string
	Target string
	Files  []FileEntry
	Dirs   []string
	// PreviewPlan 为“有预览计划”时的映射：原路径（小写）→ 保留路径（小写）。
	PreviewPlan map[string]string
}

// Providers 提供纯规划器需要、但必须由调用方预计算的数据。
type Providers struct {
	ContentHash func(entry FileEntry) (string, bool)
	ImageHash   func(entry FileEntry) (uint64, bool)
	AudioMsFn   func(entry FileEntry) (int64, bool)
}

// Config 是整理器配置；字段命名对齐原版语义。
type Config struct {
	Order OrderMode
	Steps StepSwitches

	DryRun        bool
	Conflict      ConflictPolicy
	Delete        DeleteMode
	HardlinkMerge bool

	// 扫描过滤
	Excludes         []string // d_f_2：按 [,，;；\s]+ 拆分后的子串，作用于目录路径
	MinSize          int64    // A_6
	SkipHiddenSystem bool     // F

	// 特殊文件删除
	ProtectWords        string          // A_1.C，按 [,，;；\s]+ 拆分，用于跳过改名/特殊判定
	Strength            int             // B_2，0 低 / 1 中 / 2 高
	LocalWhitelist      bool            // T.A_2，默认 true
	AutoDeleteConfirmed bool            // g
	ConfirmedKeys       map[string]bool // A_7

	// 去重
	KeepRule          KeepRule // D_1
	ImageKeepRule     KeepRule // h_2，空则原版默认最大文件
	ImageThreshold    int      // A_5，默认 8
	AudioToleranceSec int      // a_2，默认 2

	// 第五步
	OrganizeMode OrganizeMode
	Keyword      string // 关键字模式，支持逗号/分号/空格分隔
	Regex        string
	FixedFolder  string // 指定文件夹模式下的子目录名
}

// DefaultConfig 返回与原版一致的默认配置。
func DefaultConfig() Config {
	return Config{
		Order:             OrderStandalone,
		Steps:             DefaultStepSwitches(),
		Conflict:          ConflictOverwrite,
		Delete:            DeleteTrash,
		MinSize:           0,
		SkipHiddenSystem:  true,
		Strength:          1,
		LocalWhitelist:    true,
		KeepRule:          KeepShortestPath,
		ImageKeepRule:     KeepLargest,
		ImageThreshold:    8,
		AudioToleranceSec: 2,
		OrganizeMode:      ModeCategory,
	}
}

// ActionKind 动作类型；Suggest 只用于报告，不产生文件系统变化。
type ActionKind string

const (
	ActionRename        ActionKind = "rename"
	ActionMove          ActionKind = "move"
	ActionDelete        ActionKind = "delete"
	ActionHardlink      ActionKind = "hardlink"
	ActionClearReadonly ActionKind = "clear_readonly"
	ActionRemoveDir     ActionKind = "remove_dir"
	ActionEnsureDir     ActionKind = "ensure_dir"
	ActionSuggest       ActionKind = "suggest"
)

// Action 是规划器输出的最小执行单元；To 为最终路径（冲突已在规划期解析）。
type Action struct {
	Kind   ActionKind `json:"kind"`
	From   string     `json:"from,omitempty"`
	To     string     `json:"to,omitempty"`
	Reason string     `json:"reason,omitempty"`
	Rule   string     `json:"rule,omitempty"`
}

// Counter 对齐原版 b.E 的计数项。
type Counter struct {
	Renamed        int `json:"renamed"`        // A
	SpecialDeleted int `json:"specialDeleted"` // 通过删除动作成功的特殊文件数
	Moved          int `json:"moved"`          // B
	ConflictSkip   int `json:"conflictSkip"`   // b
	Readonly       int `json:"readonly"`       // C
	OrganizeSkip   int `json:"organizeSkip"`   // c
	ContentDeleted int `json:"contentDeleted"` // D
	ImageDeleted   int `json:"imageDeleted"`   // d
	AudioDeleted   int `json:"audioDeleted"`   // F
	EmptyDirs      int `json:"emptyDirs"`      // E_f_1
	Excluded       int `json:"excluded"`       // e
	HiddenSkipped  int `json:"hiddenSkipped"`  // G
	SuspectsKept   int `json:"suspectsKept"`   // g
	HardlinkMerged int `json:"hardlinkMerged"` // f
}

// Add 累加另一个计数器。
func (c *Counter) Add(o Counter) {
	c.Renamed += o.Renamed
	c.SpecialDeleted += o.SpecialDeleted
	c.Moved += o.Moved
	c.ConflictSkip += o.ConflictSkip
	c.Readonly += o.Readonly
	c.OrganizeSkip += o.OrganizeSkip
	c.ContentDeleted += o.ContentDeleted
	c.ImageDeleted += o.ImageDeleted
	c.AudioDeleted += o.AudioDeleted
	c.EmptyDirs += o.EmptyDirs
	c.Excluded += o.Excluded
	c.HiddenSkipped += o.HiddenSkipped
	c.SuspectsKept += o.SuspectsKept
	c.HardlinkMerged += o.HardlinkMerged
}

// StepPlan 是单步规划结果。
type StepPlan struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Order      int      `json:"order"`
	Actions    []Action `json:"actions"`
	Counters   Counter  `json:"counters"`
	Skipped    bool     `json:"skipped,omitempty"`
	SkipReason string   `json:"skipReason,omitempty"`
}

// Result 是整体规划结果。
type Result struct {
	Order    OrderMode  `json:"order"`
	Steps    []StepPlan `json:"steps"`
	Counters Counter    `json:"counters"`
}

// StepID 九步的稳定标识。
type StepID string

const (
	StepRenamePrefix  StepID = "rename_prefix"
	StepRenameSuffix  StepID = "rename_suffix"
	StepSpecialDelete StepID = "special_delete"
	StepReadonly      StepID = "readonly"
	StepContentDedup  StepID = "content_dedup"
	StepImageDedup    StepID = "image_dedup"
	StepAudioDedup    StepID = "audio_dedup"
	StepOrganize      StepID = "organize"
	StepEmptyDirs     StepID = "empty_dirs"
)

// stepTitles 与解密后的标题原文一致（aIC/aIb/aIe 等 getter）。
var stepTitles = map[StepID]string{
	StepRenamePrefix:  "第一步：去除文件名前缀序列号（含中间序列号）",
	StepRenameSuffix:  "第二步：去除文件名后缀序列号",
	StepSpecialDelete: "第三步：删除特殊文件（保护层优先；确定垃圾自动删；存疑项需人工确认，未确认一律保留）",
	StepReadonly:      "第四步：去除所有文件只读属性",
	StepContentDedup:  "内容去重",
	StepImageDedup:    "相似图片去重",
	StepAudioDedup:    "音频相似去重（实验性）",
	StepOrganize:      "第五步：文件整理",
	StepEmptyDirs:     "清理空文件夹",
}

// standaloneOrder / previewOrder 对应 B/E.cs L447-516 的两套顺序。
var standaloneOrder = []StepID{
	StepRenamePrefix, StepRenameSuffix, StepSpecialDelete, StepReadonly,
	StepContentDedup, StepImageDedup, StepAudioDedup, StepOrganize, StepEmptyDirs,
}

var previewOrder = []StepID{
	StepContentDedup, StepImageDedup, StepAudioDedup,
	StepRenamePrefix, StepRenameSuffix, StepSpecialDelete, StepReadonly,
	StepOrganize, StepEmptyDirs,
}

// orderedSteps 返回指定顺序下的步骤列表。
func orderedSteps(mode OrderMode) []StepID {
	if mode == OrderPreview {
		return append([]StepID(nil), previewOrder...)
	}
	return append([]StepID(nil), standaloneOrder...)
}

// stepEnabled 判断步骤是否启用；大小写与命名均与原版开关一致。
func stepEnabled(s StepSwitches, id StepID) bool {
	switch id {
	case StepRenamePrefix:
		return s.RenamePrefix
	case StepRenameSuffix:
		return s.RenameSuffix
	case StepSpecialDelete:
		return s.SpecialDelete
	case StepReadonly:
		return s.Readonly
	case StepContentDedup:
		return s.ContentDedup
	case StepImageDedup:
		return s.ImageDedup
	case StepAudioDedup:
		return s.AudioDedup
	case StepOrganize:
		return s.Organize
	case StepEmptyDirs:
		return s.EmptyDirs
	}
	return false
}
