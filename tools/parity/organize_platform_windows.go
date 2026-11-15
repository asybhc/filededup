//go:build windows

package main

import (
	"filededup/internal/engine/organize"
	"filededup/internal/platform"
)

// orgExecHooks 把平台能力注入整理执行器；引擎本身不依赖 filededup/internal/platform。
func orgExecHooks() organize.ExecHooks {
	return organize.ExecHooks{
		IsLocked:   platform.IsLocked,
		SameVolume: platform.SameVolume,
		Link:       platform.Link,
		ReleaseLocks: func(absPath string, confirm func(absPath string, holderNames []string) bool) error {
			if confirm == nil {
				// 保留平台层“无确认回调”的语义（有可见窗口时不结束进程）。
				return platform.ReleaseLocks(absPath, nil)
			}
			return platform.ReleaseLocks(absPath, func(p string, holders []platform.LockHolder) bool {
				names := make([]string, 0, len(holders))
				for _, h := range holders {
					names = append(names, h.Name)
				}
				return confirm(p, names)
			})
		},
	}
}
