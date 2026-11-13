package model

// StepID 是 9 步整理器的稳定步骤标识。字符串值写入 JSON，冻结。
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

// 两套执行顺序（C# B/E.cs#L447-L516）。两套都必须实现，顺序不可互换。
var (
	// StandaloneOrder：无预览计划（A_1.A_8 == null）时的自由整理顺序。
	StandaloneOrder = []StepID{
		StepRenamePrefix,
		StepRenameSuffix,
		StepSpecialDelete,
		StepReadonly,
		StepContentDedup,
		StepImageDedup,
		StepAudioDedup,
		StepOrganize,
		StepEmptyDirs,
	}

	// PreviewOrder：有预览/确认计划（A_1.A_8 != null）时的顺序，先去重后改名/整理。
	PreviewOrder = []StepID{
		StepContentDedup,
		StepImageDedup,
		StepAudioDedup,
		StepRenamePrefix,
		StepRenameSuffix,
		StepSpecialDelete,
		StepReadonly,
		StepOrganize,
		StepEmptyDirs,
	}
)

// OrderFor 返回指定模式下的执行顺序。
func OrderFor(withPreviewPlan bool) []StepID {
	if withPreviewPlan {
		return PreviewOrder
	}
	return StandaloneOrder
}

// StepToggles 对应 C# A_1 上的 9 个独立开关。
type StepToggles struct {
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

// Enabled 返回某步骤是否开启。
func (t StepToggles) Enabled(id StepID) bool {
	switch id {
	case StepRenamePrefix:
		return t.RenamePrefix
	case StepRenameSuffix:
		return t.RenameSuffix
	case StepSpecialDelete:
		return t.SpecialDelete
	case StepReadonly:
		return t.Readonly
	case StepContentDedup:
		return t.ContentDedup
	case StepImageDedup:
		return t.ImageDedup
	case StepAudioDedup:
		return t.AudioDedup
	case StepOrganize:
		return t.Organize
	case StepEmptyDirs:
		return t.EmptyDirs
	}
	return false
}

// ConflictPolicy 对应 C# b/D.cs#L101 的三值枚举。
// 注意：原版默认 A=overwrite（删除正在处理的源文件），属高危；产品默认改为 skip，见 DefaultOrganizeRequest。
type ConflictPolicy string

const (
	ConflictOverwrite ConflictPolicy = "overwrite" // 原版 A
	ConflictSkip      ConflictPolicy = "skip"      // 原版 a
	ConflictRename    ConflictPolicy = "rename"    // 原版 B，name (1).ext
)

// DeleteMode 对应 C# a/A.cs#L139。
type DeleteMode string

const (
	DeleteTrash     DeleteMode = "trash"     // 回收站（原版默认 A_f_1）
	DeletePermanent DeleteMode = "permanent" // 永久删除（原版 a）
)

// OrganizeMode 对应 C# 5 种整理方式（整理器规则.json organizeModes）。
type OrganizeMode string

const (
	OrganizeByCategory    OrganizeMode = "category"    // 按文件类型分类（hF）
	OrganizeByExtension   OrganizeMode = "extension"   // 按文件后缀名整理（hB）
	OrganizeToFolder      OrganizeMode = "folder"      // 按指定文件夹整理（hC）
	OrganizeByNameKeyword OrganizeMode = "nameKeyword" // 按文件名整理（hD）
	OrganizeByNameRegex   OrganizeMode = "nameRegex"   // 按文件名字符特征整理（he）
)

// OrganizeModeOptions 是整理方式的参数。Keyword 与 Regex 在 C# 中共用 A_1.b 字段。
type OrganizeModeOptions struct {
	Mode     OrganizeMode `json:"mode"`
	Keyword  string       `json:"keyword,omitempty"`
	Regex    string       `json:"regex,omitempty"`
	FixedDir string       `json:"fixedDir,omitempty"`
}

// OrganizeRequest 是一次 9 步整理运行的完整输入。
type OrganizeRequest struct {
	SchemaVersion int      `json:"schemaVersion"`
	RequestID     string   `json:"requestId"`
	Root          string   `json:"root"`      // 扫描根目录 A_1.A
	TargetDir     string   `json:"targetDir"` // 整理目标目录 A_1.a
	Excludes      []string `json:"excludes"`
	MinSize       int64    `json:"minSize"`
	IncludeHidden bool     `json:"includeHidden"`

	DryRun         bool `json:"dryRun"`         // C# c_1
	HasPreviewPlan bool `json:"hasPreviewPlan"` // C# A_1.A_8 != null，决定执行顺序

	ConflictPolicy          ConflictPolicy `json:"conflictPolicy"`
	DeleteMode              DeleteMode     `json:"deleteMode"`
	HardlinkInsteadOfDelete bool           `json:"hardlinkInsteadOfDelete"` // C# A_1.G
	RecognizeStrength       int            `json:"recognizeStrength"`       // 垃圾识别强度 0..2（C# B_2，默认 1）
	WhitelistExtensions     []string       `json:"whitelistExtensions"`     // 特殊文件删除的扩展名白名单（C# A_1.C）
	ConfirmedSuspects       []string       `json:"confirmedSuspects"`       // 人工确认的可疑文件键（C# A_1.A_7）
	AutoDeleteConfirmed     bool           `json:"autoDeleteConfirmed"`     // C# A_1.g

	KeepPolicy      KeepPolicy `json:"keepPolicy"`      // 内容/音频去重保留规则（C# D_1）
	ImageKeepPolicy KeepPolicy `json:"imageKeepPolicy"` // 图片去重保留规则（C# h_2，缺省 largest）

	ImageThreshold    int `json:"imageThreshold"`    // dHash 汉明距离阈值（C# A_5，默认 8）
	AudioToleranceSec int `json:"audioToleranceSec"` // 音频时长容差秒（C# a_2，默认 2）

	Steps    StepToggles         `json:"steps"`
	Organize OrganizeModeOptions `json:"organize"`
	Workers  int                 `json:"workers"`
}

// OrganizeCounters 是各步骤的确定性计数，供对拍。
type OrganizeCounters struct {
	RenamedPrefix    int `json:"renamedPrefix"`
	RenamedSuffix    int `json:"renamedSuffix"`
	SpecialDeleted   int `json:"specialDeleted"`
	SuspectsKept     int `json:"suspectsKept"`
	ReadonlyCleared  int `json:"readonlyCleared"`
	ContentDeleted   int `json:"contentDeleted"`
	ImageDeleted     int `json:"imageDeleted"`
	AudioDeleted     int `json:"audioDeleted"`
	Moved            int `json:"moved"`
	EmptyDirsRemoved int `json:"emptyDirsRemoved"`
	ConflictsSkipped int `json:"conflictsSkipped"`
	Failed           int `json:"failed"`
}

// StepResult 是单个步骤的执行摘要。
type StepResult struct {
	ID          StepID   `json:"id"`
	Title       string   `json:"title"`
	Enabled     bool     `json:"enabled"`
	Executed    bool     `json:"executed"`
	Scanned     int      `json:"scanned"`
	Changed     int      `json:"changed"`
	Skipped     int      `json:"skipped"`
	Failed      int      `json:"failed"`
	ActionCount int      `json:"actionCount"`
	Messages    []string `json:"messages,omitempty"`
}

// OrganizeResult 是一次 9 步整理的输出。
type OrganizeResult struct {
	SchemaVersion int              `json:"schemaVersion"`
	RequestID     string           `json:"requestId"`
	Request       OrganizeRequest  `json:"request"`
	DryRun        bool             `json:"dryRun"`
	Order         []StepID         `json:"order"`
	Steps         []StepResult     `json:"steps"`
	Counters      OrganizeCounters `json:"counters"`
	Actions       []Action         `json:"actions"`
	Skipped       []Skip           `json:"skipped"`
	Runtime       *Runtime         `json:"runtime,omitempty"`
}
