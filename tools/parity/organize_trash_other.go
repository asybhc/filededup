//go:build !windows

package main

// 非 Windows 构建没有平台回收站实现；nil 时执行器退化为永久删除。
var orgTrash func(string) error
