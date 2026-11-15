//go:build linux

package platform

// Linux 回收站：按 XDG Trash 规范实现。
//
//   1. 与 $XDG_DATA_HOME 同卷时，移入 $XDG_DATA_HOME/Trash（默认 ~/.local/share/Trash）；
//   2. 跨卷时使用卷根目录下的 .Trash/$uid（需 sticky 位）或 .Trash-$uid；
//   3. 拒绝回退为永久删除：任何一步失败都返回错误，由调用方按动作失败处理。
//
// 本实现只在交叉编译与 Linux CI 上验证过，尚未在桌面发行版真机运行。

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Trash 把文件或目录移入符合 XDG 规范的回收站。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("platform: 解析路径失败：%w", err)
	}
	if _, err := os.Lstat(abs); err != nil {
		return fmt.Errorf("platform: 待删除项不可访问：%w", err)
	}
	trashDir, err := linuxTrashDir(abs)
	if err != nil {
		return err
	}
	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")
	if err := os.MkdirAll(filesDir, 0o700); err != nil {
		return fmt.Errorf("platform: 创建回收站目录失败：%w", err)
	}
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return fmt.Errorf("platform: 创建回收站信息目录失败：%w", err)
	}

	target, name, err := linuxUniqueName(filesDir, infoDir, filepath.Base(abs))
	if err != nil {
		return err
	}
	infoPath := filepath.Join(infoDir, name+".trashinfo")
	body := "[Trash Info]\nPath=" + url.PathEscape(abs) +
		"\nDeletionDate=" + time.Now().Format("2006-01-02T15:04:05") + "\n"
	if err := os.WriteFile(infoPath, []byte(body), 0o600); err != nil {
		return fmt.Errorf("platform: 写入 .trashinfo 失败：%w", err)
	}
	if err := os.Rename(abs, target); err != nil {
		_ = os.Remove(infoPath)
		return fmt.Errorf("platform: 移入回收站失败：%w", err)
	}
	return nil
}

// linuxTrashDir 选择与待删除项同卷的回收站目录。
func linuxTrashDir(abs string) (string, error) {
	if homeDir := linuxHomeTrash(); homeDir != "" {
		if dev, err := linuxDeviceOfExisting(homeDir); err == nil {
			if fileDev, err := linuxDevice(abs); err == nil && dev == fileDev {
				return homeDir, nil
			}
		}
	}

	mount, err := linuxMountPoint(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	uid := os.Getuid()
	shared := filepath.Join(mount, ".Trash")
	if info, err := os.Stat(shared); err == nil && info.IsDir() && info.Mode()&os.ModeSticky != 0 {
		userDir := filepath.Join(shared, fmt.Sprintf("%d", uid))
		if err := os.MkdirAll(userDir, 0o700); err == nil {
			return userDir, nil
		}
	}
	userDir := filepath.Join(mount, fmt.Sprintf(".Trash-%d", uid))
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		return "", fmt.Errorf("platform: 当前卷没有可写回收站：%w", err)
	}
	return userDir, nil
}

// linuxHomeTrash 返回 $XDG_DATA_HOME/Trash（默认 ~/.local/share/Trash）。
func linuxHomeTrash() string {
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Join(dataHome, "Trash")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "Trash")
}

// linuxDeviceOfExisting 从 path 向上找到第一个存在的祖先并读取设备号。
func linuxDeviceOfExisting(path string) (uint64, error) {
	cur := path
	for {
		if _, err := os.Lstat(cur); err == nil {
			return linuxDevice(cur)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return 0, fmt.Errorf("platform: 找不到可访问的祖先目录：%s", path)
		}
		cur = parent
	}
}

func linuxDevice(path string) (uint64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("platform: 无法读取设备号：%s", path)
	}
	return uint64(st.Dev), nil
}

// linuxMountPoint 返回 dir 所在卷的挂载点。
func linuxMountPoint(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	dev, err := linuxDevice(abs)
	if err != nil {
		return "", err
	}
	cur := abs
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur, nil
		}
		parentDev, err := linuxDevice(parent)
		if err != nil {
			return "", err
		}
		if parentDev != dev {
			return cur, nil
		}
		cur = parent
	}
}

// linuxUniqueName 在 files/info 两个目录里都不冲突时返回目标路径与文件名。
func linuxUniqueName(filesDir, infoDir, name string) (string, string, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 10000; i++ {
		cand := name
		if i > 0 {
			cand = fmt.Sprintf("%s.%d%s", stem, i, ext)
		}
		if _, err := os.Lstat(filepath.Join(filesDir, cand)); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(infoDir, cand+".trashinfo")); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return filepath.Join(filesDir, cand), cand, nil
	}
	return "", "", errors.New("platform: 回收站同名文件过多")
}
