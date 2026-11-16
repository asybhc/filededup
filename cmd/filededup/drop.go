//go:build desktop

package main

// 拖放目录的落点解析：前端从 Wails 原生文件拖放事件拿到绝对路径后，
// 交给这里统一校验，避免前端对符号链接/相对路径做出错误判断。

import (
	"os"
	"path/filepath"
	"strings"
)

// DropPathResult 描述一次拖放落点的解析结果。
type DropPathResult struct {
	// Path 是最终采用的目录绝对路径。
	Path string `json:"path"`
	// IsDir 为 true 表示拖入的就是目录。
	IsDir bool `json:"isDir"`
	// FromFile 为 true 表示拖入的是文件，Path 是它所在目录。
	FromFile bool `json:"fromFile"`
	// Message 是给用户看的一句说明。
	Message string `json:"message"`
}

// InspectDroppedPath 校验拖放路径：目录直接使用；单个文件退回其所在目录；
// 多个路径或不存在时给出原因，不抛错。
func (a *App) InspectDroppedPath(raw string) DropPathResult {
	p := strings.TrimSpace(raw)
	if p == "" {
		return DropPathResult{Message: "没有收到有效路径"}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return DropPathResult{Message: "路径无法解析：" + err.Error()}
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return DropPathResult{Message: "路径不存在：" + abs}
		}
		return DropPathResult{Message: "无法读取路径：" + err.Error()}
	}
	if info.IsDir() {
		return DropPathResult{Path: abs, IsDir: true, Message: "已添加文件夹：" + abs}
	}
	parent := filepath.Dir(abs)
	if parent == "" || parent == abs {
		return DropPathResult{Message: "拖入的是文件，且无法确定所在目录"}
	}
	return DropPathResult{Path: parent, FromFile: true, Message: "拖入的是文件，已改用所在目录：" + parent}
}
