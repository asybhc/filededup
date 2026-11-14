package model

// Request 是一次只读扫描（查重）的完整输入。
// 对应 C# 侧 B/R.cs 的 d 参数对象，字段语义在注释中标注原实现位置。
type Request struct {
	SchemaVersion   int        `json:"schemaVersion"`
	RequestID       string     `json:"requestId"`
	Roots           []string   `json:"roots"`         // 扫描根目录（绝对路径）；C# 只支持单根，Go 允许多根
	Excludes        []string   `json:"excludes"`      // 子串排除；C# A_1.d_f_2 按 [,，;；\s]+ 拆分后做子串匹配
	MinSize         int64      `json:"minSize"`       // 最小文件大小（字节），0 = 不限；C# A_1.A_6
	IncludeHidden   bool       `json:"includeHidden"` // false = 跳过 Hidden/System（C# F=true 的默认语义）
	FollowLinks     bool       `json:"followLinks"`   // false = 跳过 ReparsePoint/符号链接（C# 行为）
	Exact           bool       `json:"exact"`         // 精确查重（大小预筛 → SHA-256）
	QuickHash       bool       `json:"quickHash"`     // 64KB 快筛；删除前必须全量复核
	SimilarImage    *ImageOpts `json:"similarImage,omitempty"`
	SimilarAudio    *AudioOpts `json:"similarAudio,omitempty"`
	KeepPolicy      KeepPolicy `json:"keepPolicy"`      // 精确查重保留策略
	ImageKeepPolicy KeepPolicy `json:"imageKeepPolicy"` // 图片组保留策略（C-15：独立设置，缺省 largest）
	Workers         int        `json:"workers"`         // 0 = 引擎默认 min(NumCPU,4)
	UseCache        bool       `json:"useCache"`
}

// ImageOpts 是相似图片判定参数。C# A_1.E / A_1.A_5。
type ImageOpts struct {
	Enabled   bool `json:"enabled"`
	Threshold int  `json:"threshold"` // 汉明距离阈值 0..64，默认 8，判定为 <=
	// SkipUniform 为 true 时，无行内梯度的退化指纹（全 0 或纯色图产生的 0x0101…01）不参与相似分组。
	// 原版没有该规则（互不相关的纯色图会被并入同组），默认 false 保持等价；开启后会在 Skipped 中
	// 登记 code=uniform_fingerprint。详见 docs/similar-image.md。
	SkipUniform bool `json:"skipUniform,omitempty"`
}

// AudioOpts 是相似音频判定参数。C# A_1.f / A_1.a_2。
type AudioOpts struct {
	Enabled   bool `json:"enabled"`
	Tolerance int  `json:"tolerance"` // 时长容差（秒）0..60，默认 2，判定为 <=
}

// Stats 只包含确定性计数，供对拍逐项比较。
// 非确定性的耗时/峰值内存放在 Runtime，不参与对拍。
type Stats struct {
	FilesEnumerated  int64 `json:"filesEnumerated"` // 过滤后参与扫描的文件数（C# R.A_2）
	BytesEnumerated  int64 `json:"bytesEnumerated"` // 上述文件大小之和（C# R.A_3；原版读不到大小时按 -1 累加，Go 不计入，见差异登记）
	FilesHashed      int64 `json:"filesHashed"`     // 实际计算过完整哈希的文件数
	BytesHashed      int64 `json:"bytesHashed"`     // 实际读入哈希器的字节数
	CacheHits        int64 `json:"cacheHits"`       // 命中哈希缓存的文件数
	CacheMisses      int64 `json:"cacheMisses"`
	Groups           int64 `json:"groups"` // 重复组总数
	ExactGroups      int64 `json:"exactGroups"`
	ImageGroups      int64 `json:"imageGroups"`
	AudioGroups      int64 `json:"audioGroups"`
	DuplicateFiles   int64 `json:"duplicateFiles"`   // 组内非保留成员总数
	ReclaimableBytes int64 `json:"reclaimableBytes"` // 删除非保留成员可释放的字节数
	Skipped          int64 `json:"skipped"`          // 与 len(Result.Skipped) 一致
}

// Result 是一次扫描的输出。Groups 与 Skipped 必须已排序（见确定性规范）。
type Result struct {
	SchemaVersion int      `json:"schemaVersion"`
	RequestID     string   `json:"requestId"`
	Request       Request  `json:"request"`
	Stats         Stats    `json:"stats"`
	Groups        []Group  `json:"groups"`
	Skipped       []Skip   `json:"skipped"`
	Runtime       *Runtime `json:"runtime,omitempty"`
}

// Group 是一个重复组。Members 按路径排序；Keep 指向保留成员。
type Group struct {
	ID          string    `json:"id"` // 确定性 ID：<kind>:<key>
	Kind        GroupKind `json:"kind"`
	Key         string    `json:"key"` // exact=sha256；image=保留成员 dHash；audio=扩展名+时长区间标识
	Members     []Member  `json:"members"`
	Keep        string    `json:"keep"` // 保留成员绝对路径
	Reclaimable int64     `json:"reclaimable"`
}

// Member 是组内一个文件。
type Member struct {
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	ModTime     string    `json:"modTime"`               // UTC RFC3339Nano
	Hash        string    `json:"hash,omitempty"`        // SHA-256 小写 hex；相似组默认空，plan 阶段补算
	Fingerprint string    `json:"fingerprint,omitempty"` // 图片 dHash 十六进制
	DurationMS  int64     `json:"durationMs,omitempty"`  // 音频时长（毫秒）
	Keep        bool      `json:"keep"`
	Deletable   bool      `json:"deletable"`
	Reason      string    `json:"reason,omitempty"`
	Snapshot    *Precheck `json:"snapshot,omitempty"` // 扫描期快照，执行前复核用（R9/C-03）
}

// Runtime 是非确定性的运行信息，仅用于诊断，不参与对拍。
type Runtime struct {
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
	ElapsedMS    int64  `json:"elapsedMs"`
	PeakRSSBytes int64  `json:"peakRssBytes,omitempty"`
	Workers      int    `json:"workers"`
	GoVersion    string `json:"goVersion"`
}
