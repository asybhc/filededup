//go:build !windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// AcquireInstance 在非 Windows 平台用配置目录下的排他文件锁实现单实例。
func AcquireInstance() (release func(), alreadyRunning bool, err error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, false, err
	}
	lockDir := filepath.Join(dir, "filededup")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(filepath.Join(lockDir, ".instance.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return func() {}, true, nil
		}
		return nil, false, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, false, nil
}
