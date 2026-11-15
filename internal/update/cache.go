package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PruneResult 记录一次缓存清理的结果。
type PruneResult struct {
	Kept    int   // 保留下来的安装包数量
	Removed int   // 删除的文件数量（含残留临时文件）
	Freed   int64 // 释放的字节数
}

// PruneCache 清理更新缓存目录。
//
// 规则（目录通常是 os.UserCacheDir()/FileDedup/updates）：
//   - 残留临时文件一律删除：*.part（下载未完成）、*.new / *.old（替换中断的副本与备份）；
//   - 其余普通文件按修改时间从新到旧排序，保留前 keep 个（keep < 1 时按 1 处理），更旧的删除；
//   - 子目录与其他非普通文件不动；
//   - 单个文件删除失败不中断整体清理，错误合并后返回，调用方只记日志即可。
func PruneCache(dir string, keep int) (PruneResult, error) {
	var res PruneResult
	if keep < 1 {
		keep = 1
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return res, nil
		}
		return res, err
	}

	type cachedFile struct {
		name string
		mod  time.Time
		size int64
	}
	var files []cachedFile
	var errs []error

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s：%w", e.Name(), err))
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if hasTempSuffix(e.Name()) {
			if err := os.Remove(path); err != nil {
				errs = append(errs, fmt.Errorf("%s：%w", e.Name(), err))
				continue
			}
			res.Removed++
			res.Freed += info.Size()
			continue
		}
		files = append(files, cachedFile{name: e.Name(), mod: info.ModTime(), size: info.Size()})
	}

	sort.Slice(files, func(i, j int) bool {
		if !files[i].mod.Equal(files[j].mod) {
			return files[i].mod.After(files[j].mod)
		}
		return files[i].name > files[j].name
	})
	for i, f := range files {
		if i < keep {
			res.Kept++
			continue
		}
		if err := os.Remove(filepath.Join(dir, f.name)); err != nil {
			errs = append(errs, fmt.Errorf("%s：%w", f.name, err))
			continue
		}
		res.Removed++
		res.Freed += f.size
	}
	return res, errors.Join(errs...)
}

func hasTempSuffix(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".part", ".new", ".old"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}
