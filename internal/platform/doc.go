// Package platform 是平台适配层：trash / hardlink / secret / proc / search / tray / instance。
// 按 GOOS 分文件实现，engine 不得反向依赖本包。
// 里程碑：M2 起逐步实现。
package platform
