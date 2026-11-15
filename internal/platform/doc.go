// Package platform 是平台适配层：trash / hardlink / open / instance / machineid。
// 按 GOOS 分文件实现，engine 不得反向依赖本包。
//
// 里程碑：Windows 端 trash（IFileOperation）、hardlink、open、instance 与机器码 v2 采集已实现。
// Linux 端 trash 走 XDG Trash（同卷 $XDG_DATA_HOME/Trash，跨卷 .Trash/$uid 或 .Trash-$uid），
// macOS 端 trash 走 ~/.Trash（跨卷 .Trashes/$uid）；两端的机器码采集分别读 /etc/machine-id
// 与 IOPlatformUUID。三者均已通过交叉编译与 go vet 验证，但未在真机运行验证。
// 其余平台在未实现时返回 ErrUnsupported。
package platform

import "errors"

// ErrUnsupported 表示当前平台尚未实现该能力。
var ErrUnsupported = errors.New("platform: 当前平台不支持该能力")
