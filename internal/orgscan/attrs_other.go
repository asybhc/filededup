//go:build !windows

package orgscan

import (
	"os"
	"path"
	"strings"
)

// FileFlags 在非 Windows 平台用权限位与 "." 前缀近似只读/隐藏。
func FileFlags(p string) (readonly, hidden, system bool) {
	info, err := os.Stat(p)
	if err != nil {
		return false, false, false
	}
	return info.Mode().Perm()&0o200 == 0, strings.HasPrefix(path.Base(strings.ReplaceAll(p, `\`, "/")), "."), false
}
