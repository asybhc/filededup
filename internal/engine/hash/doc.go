// Package hash 负责 SHA-256 流式哈希、64KB 快筛与哈希缓存。
// 缓存键必须与 C# 修复后版本一致：路径 + 长度 + LastWriteTimeUtc.Ticks（不含创建时间）。
// 里程碑：M2。
package hash
