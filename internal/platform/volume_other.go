//go:build !windows

package platform

import (
	"errors"
	"syscall"
)

// volumeOf 在非 Windows 平台没有盘符概念，返回空串表示"未知卷"，
// 由 os.Link 实际执行时以 EXDEV 判定跨卷。
func volumeOf(path string) (string, error) {
	return "", nil
}

func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
