//go:build darwin

package platform

// macOS 回收站：同卷移入 ~/.Trash；跨卷移入卷根目录的 .Trashes/$uid。
//
// 只做文件系统层面的移动，不调用 Finder/AppleScript，因此不依赖自动化授权。
// 本实现只在交叉编译与 macOS CI 上验证过，尚未在真机运行。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Trash 把文件或目录移入 macOS 回收站。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("platform: 解析路径失败：%w", err)
	}
	if _, err := os.Lstat(abs); err != nil {
		return fmt.Errorf("platform: 待删除项不可访问：%w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("platform: 无法定位用户主目录：%w", err)
	}
	err = darwinMoveToTrash(filepath.Join(home, ".Trash"), abs)
	if err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EXDEV) {
		return fmt.Errorf("platform: 移入回收站失败：%w", err)
	}

	// 跨卷：使用该卷根目录的 .Trashes/<uid>。
	mount, err := darwinMountPoint(filepath.Dir(abs))
	if err != nil {
		return err
	}
	volTrash := filepath.Join(mount, ".Trashes", fmt.Sprintf("%d", os.Getuid()))
	if err := os.MkdirAll(volTrash, 0o700); err != nil {
		return fmt.Errorf("platform: 创建卷回收站失败：%w", err)
	}
	if err := darwinMoveToTrash(volTrash, abs); err != nil {
		return fmt.Errorf("platform: 移入卷回收站失败：%w", err)
	}
	return nil
}

func darwinMoveToTrash(dir, abs string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	name, err := darwinUniqueName(dir, filepath.Base(abs))
	if err != nil {
		return err
	}
	return os.Rename(abs, filepath.Join(dir, name))
}

func darwinUniqueName(dir, name string) (string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s %d%s", stem, i, ext)
		}
		if _, err := os.Lstat(filepath.Join(dir, cand)); errors.Is(err, fs.ErrNotExist) {
			return cand, nil
		}
	}
	return "", errors.New("platform: 回收站同名文件过多")
}

// darwinMountPoint 返回 dir 所在卷的挂载点。
func darwinMountPoint(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dev, err := darwinDevice(abs)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur, nil
		}
		parentDev, err := darwinDevice(parent)
		if err != nil {
			return "", err
		}
		if parentDev != dev {
			return cur, nil
		}
		cur = parent
	}
}

func darwinDevice(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("platform: 无法读取设备号：%s", path)
	}
	return int64(st.Dev), nil
}
