//go:build windows

package main

import "filededup/internal/platform"

// orgTrash 复用 platform 已有的回收站实现（IFileOperation + SHFileOperation 兜底），
// 不在对拍工具或整理器里重写。
var orgTrash func(string) error = platform.Trash
