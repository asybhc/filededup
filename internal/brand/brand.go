// Package brand 集中存放产品的显示名与落盘标识，避免改名时各包各写一份字符串。
//
// 注意：这里的名称会同时用于窗口标题、安装目录、数据目录、注册表与单实例互斥体，
// 修改前要确认迁移逻辑（cmd/filededup/migrate.go）仍能识别旧目录。
package brand

import (
	"os"
	"path/filepath"
)

const (
	// DisplayName 是界面、安装向导与版本资源里展示的产品名。
	DisplayName = "文件整理.一杯好茶"
	// Publisher 是公司名，保持不变。
	Publisher = "一杯好茶"

	// ExeName 是可执行文件名（安装包内的主程序与快捷方式指向它）。
	ExeName = DisplayName + ".exe"
	// SetupBaseName 是安装包文件名的前缀，完整名为 "<前缀>-<version>.exe"。
	SetupBaseName = DisplayName + "-Setup"

	// DataDirName 是 %AppData% 下的数据目录名（配置、凭据、日志、更新缓存）。
	DataDirName = DisplayName
	// InstallDirName 是 %LOCALAPPDATA%\Programs 下的安装目录名。
	InstallDirName = DisplayName
	// InstallID 是安装注册表基址 HKCU\Software\<InstallID> 的键名，
	// 以及卸载列表项 Software\Microsoft\Windows\CurrentVersion\Uninstall\<InstallID>。
	InstallID = DisplayName

	// SingleInstanceName 是单实例互斥体名（Local\ 前缀：同一登录会话内唯一）。
	SingleInstanceName = `Local\` + DisplayName + `.SingleInstance`

	// LegacyDataDirName 是改名前桌面版的数据目录（%AppData%\FileDedup）。
	LegacyDataDirName = "FileDedup"
	// LegacyToolDirName 是改名前与 C# 版共用的试用记录目录（%AppData%\FileDedupTool）。
	LegacyToolDirName = "FileDedupTool"
)

// DataDir 返回新数据目录的绝对路径（Windows 即 %AppData%\<DisplayName>）。
func DataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DataDirName), nil
}

// DataSub returns a path under the data directory.
func DataSub(elem ...string) (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elem...)...), nil
}
