//go:build !windows

package main

import "filededup/internal/engine/organize"

// orgExecHooks 在非 Windows 构建下不注入平台能力：占用检测视为未占用、
// 同卷判定按盘符兜底、建链接退化为 os.Link。
func orgExecHooks() organize.ExecHooks {
	return organize.ExecHooks{}
}
