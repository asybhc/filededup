package model

// SkipCode 是跳过/失败原因码。字符串值写入 JSON，冻结。
// 只登记"异常或需要用户知晓"的跳过；正常的排除/过小文件不逐条登记，避免结果膨胀。
type SkipCode string

const (
	SkipSizeFailed        SkipCode = "size_failed"        // 读不到文件大小（C# C-25/R1）
	SkipPermissionDenied  SkipCode = "permission_denied"  // 权限不足
	SkipPathTooLong       SkipCode = "path_too_long"      // 路径超过平台上限
	SkipReadFailed        SkipCode = "read_failed"        // 读取失败
	SkipVanished          SkipCode = "vanished"           // 枚举后文件消失
	SkipDecodeFailed      SkipCode = "decode_failed"      // 图片/音频解码失败
	SkipUnsupportedFormat SkipCode = "unsupported_format" // 格式不在支持清单
	SkipHardlinkAlias     SkipCode = "hardlink_alias"     // 同一 inode 的多个路径，已折叠
	SkipPrecheckFailed    SkipCode = "precheck_failed"    // 执行前复核不通过（大小/时间/哈希变化）
	SkipConflict          SkipCode = "conflict"           // 目标已存在且策略为跳过
	SkipDryRun            SkipCode = "dry_run"            // 试运行，未实际落盘
)

// Skip 是一条跳过记录。必须能在结果页"未处理项"中看到。
type Skip struct {
	Path    string   `json:"path"`
	Code    SkipCode `json:"code"`
	Message string   `json:"message"`
	Err     string   `json:"error,omitempty"` // 原始错误文本，仅诊断用
}
