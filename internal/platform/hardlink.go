package platform

import (
	"errors"
	"os"
	"strings"
)

var (
	// ErrCrossVolume 表示跨卷硬链接（原版语义：直接失败并提示，不静默复制）。
	ErrCrossVolume = errors.New("platform: 跨卷硬链接不受支持")
	// ErrLinkExists 表示目标路径已存在，拒绝覆盖。
	ErrLinkExists = errors.New("platform: 硬链接目标已存在")
)

// SameVolume 判断两个路径是否位于同一卷。
func SameVolume(a, b string) (bool, error) {
	va, err := volumeOf(a)
	if err != nil {
		return false, err
	}
	vb, err := volumeOf(b)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(va, vb), nil
}

// Link 创建硬链接 Dst → Src（两者指向同一份数据）。
// 跨卷返回 ErrCrossVolume；目标已存在返回 ErrLinkExists。
func Link(src, dst string) error {
	same, err := SameVolume(src, dst)
	if err != nil {
		return err
	}
	if !same {
		return ErrCrossVolume
	}
	if _, err := os.Lstat(dst); err == nil {
		return ErrLinkExists
	}
	if err := os.Link(src, dst); err != nil {
		if isCrossDevice(err) {
			return ErrCrossVolume
		}
		return err
	}
	return nil
}
