//go:build windows

package platform

import (
	"errors"
	"path/filepath"
	"strings"
	"syscall"
)

// volumeOf 返回盘符（如 "C:"），统一大写以便跨大小写比较。
func volumeOf(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return strings.ToUpper(filepath.VolumeName(abs)), nil
}

// errorNotSameDevice 是 Windows 的 ERROR_NOT_SAME_DEVICE（17）。
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice 把 Windows 的 ERROR_NOT_SAME_DEVICE 归一为跨卷判定。
func isCrossDevice(err error) bool {
	return errors.Is(err, errorNotSameDevice)
}
