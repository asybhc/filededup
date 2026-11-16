//go:build desktop && !windows

package main

import "syscall"

// diskFreeImpl 用 Statfs 取路径所在卷的可用空间（Bfree * Bsize）。
func diskFreeImpl(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
