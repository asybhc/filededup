// Package platform 是平台适配层：trash / hardlink / open / instance / machineid。
// 按 GOOS 分文件实现，engine 不得反向依赖本包。
//
// 里程碑：M2 已实现 Windows 端 trash（IFileOperation）、hardlink、open、instance
// 与机器码 v2 采集；非 Windows 端除 hardlink/open/instance 的等价实现外，
// trash 与机器码采集按里程碑顺序后续补齐（未实现时返回 ErrUnsupported）。
package platform

import "errors"

// ErrUnsupported 表示当前平台尚未实现该能力。
var ErrUnsupported = errors.New("platform: 当前平台不支持该能力")
