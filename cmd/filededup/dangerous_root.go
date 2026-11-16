//go:build desktop

package main

// 危险源拒绝：扫描与整理都拒绝整盘根目录与系统目录作为源。
//
// 判定只做路径字符串比较，不访问磁盘；拒绝发生在任务创建之前，界面直接拿到错误文案。
// 全盘查重（req.FullDisk）由 fullDiskExcludes 统一排除系统目录，因此不经过这里。

import (
	"fmt"
	"path/filepath"
	"strings"
)

// dangerousRootDirs 是按路径分段（小写）匹配的系统目录名。
// 只拦源目录本身位于这些目录之下，不拦普通目录里恰好包含这些词的文件。
var dangerousRootDirs = map[string]bool{
	"windows":                 true,
	"program files":           true,
	"program files (x86)":     true,
	"programdata":             true,
	"system volume information": true,
	"$recycle.bin":            true,
	"recovery":                true,
	"boot":                    true,
	"perflogs":                true,
	"windows.old":             true,
	"config.msi":              true,
	"$winreagent":             true,
	"system32":                true,
	"syswow64":                true,
	"winsxs":                  true,
}

// isVolumeRoot 判断路径是否就是整盘根目录（C:\ 或 /）。
func isVolumeRoot(p string) bool {
	clean := filepath.Clean(p)
	vol := filepath.VolumeName(clean)
	if vol != "" {
		return clean == vol || clean == vol+string(filepath.Separator)
	}
	// Windows 上 Clean("/") 会得到 "\"，两个都算根。
	return clean == "/" || clean == `\`
}

// dangerousRootComponent 返回命中的系统目录名（小写），未命中返回空串。
func dangerousRootComponent(p string) string {
	clean := filepath.Clean(p)
	parts := strings.FieldsFunc(clean, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if dangerousRootDirs[strings.ToLower(part)] {
			return strings.ToLower(part)
		}
	}
	return ""
}

// rejectDangerousRoot 在任务创建前拒绝危险源；op 用于文案（“扫描”或“整理”）。
func rejectDangerousRoot(p string, op string) error {
	if isVolumeRoot(p) {
		if op == "扫描" {
			return fmt.Errorf("拒绝扫描整盘根目录：%s。请选择具体子目录；整盘查重请使用“全盘查重”（会自动排除系统目录）。", p)
		}
		return fmt.Errorf("拒绝整理整盘根目录：%s。请选择具体子目录。", p)
	}
	if name := dangerousRootComponent(p); name != "" {
		return fmt.Errorf("拒绝%s系统目录：%s（命中 %s；系统目录不在本工具的处理范围内）。", op, p, name)
	}
	return nil
}
