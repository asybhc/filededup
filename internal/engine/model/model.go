// Package model 定义文件去重引擎与外壳（桌面/HTTP/CLI）之间的冻结契约。
//
// 冻结规则（M0）：
//   - 本包只放纯数据类型与校验，不依赖 engine/platform/api/store/UI。
//   - 字段名与 JSON 名一旦冻结，只能通过提升 SchemaVersion 变更，禁止静默改名或删除。
//   - 所有输出字段顺序由结构体定义顺序决定；禁止用 map 承载输出。
//   - 路径一律保存为绝对路径（调用方负责 Clean）；时间一律 UTC RFC3339Nano；大小一律字节整数。
package model

// SchemaVersion 是当前契约版本。任何破坏性字段变更必须 +1，并在 schema/v1 旁新增 v2。
const SchemaVersion = 1

// GroupKind 标识重复组的判定类型。字符串值写入 JSON，冻结。
type GroupKind string

const (
	GroupExact GroupKind = "exact" // SHA-256 完全一致
	GroupImage GroupKind = "image" // 感知哈希（dHash）相似
	GroupAudio GroupKind = "audio" // 时长相近且扩展名相同
)

// KeepPolicy 是保留策略的稳定标识。字符串值写入 JSON，冻结。
//
// C# 基线（B/E.cs#L1188-L1207）支持的 5 种：
//   - newest：LastWriteTime 降序
//   - oldest：LastWriteTime 升序
//   - largest：文件大小降序
//   - nameShortest：文件名长度升序
//   - pathShortest：完整路径长度升序（默认）
//
// 所有分支并列时统一追加 ThenBy(完整路径长度)（原版行为，必须逐字复刻）。
type KeepPolicy string

const (
	KeepNewest       KeepPolicy = "newest"
	KeepOldest       KeepPolicy = "oldest"
	KeepLargest      KeepPolicy = "largest"
	KeepNameShortest KeepPolicy = "nameShortest"
	KeepPathShortest KeepPolicy = "pathShortest"
)

// 扫描（精确查重）只使用 4 种策略：组内大小相同，largest 无意义。
var scanKeepPolicies = map[KeepPolicy]bool{
	KeepNewest:       true,
	KeepOldest:       true,
	KeepNameShortest: true,
	KeepPathShortest: true,
}

// 整理器支持全部 5 种。
var organizeKeepPolicies = map[KeepPolicy]bool{
	KeepNewest:       true,
	KeepOldest:       true,
	KeepLargest:      true,
	KeepNameShortest: true,
	KeepPathShortest: true,
}
