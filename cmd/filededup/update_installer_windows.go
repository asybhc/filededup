//go:build desktop && windows

package main

// 安装器式更新：清单资产是 NSIS 安装包（setup）时使用。
//
// 不能把安装包直接当成新 exe 替换（update.Replace 会把它放到应用路径并以
// 无参数启动，等于弹出图形安装向导）。这里的做法是：下载并验签安装包后，
// 启动一个隐藏窗口的后台 PowerShell，先等当前进程退出，再以 /S 静默运行
// 安装包；由安装器完成 exe 替换、卸载键 DisplayVersion 与快捷方式更新。

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func scheduleSilentInstall(pkg string) error {
	pid := os.Getpid()
	// PowerShell 单引号字符串里，单引号自身要重复一次转义。
	quoted := strings.ReplaceAll(pkg, "'", "''")
	script := fmt.Sprintf(
		"Wait-Process -Id %d -ErrorAction SilentlyContinue; Start-Process -FilePath '%s' -ArgumentList '/S' -Wait",
		pid, quoted)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	// CREATE_NO_WINDOW：不在桌面闪现控制台窗口。
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
