//go:build desktop && !windows

package main

import "fmt"

// 安装器式更新依赖 Windows 的 NSIS 静默安装与 PowerShell 等待进程退出；
// 其它平台继续使用 update.Replace 的单文件替换。
func scheduleSilentInstall(string) error {
	return fmt.Errorf("安装器式更新仅支持 Windows")
}
