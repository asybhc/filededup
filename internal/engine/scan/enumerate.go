package scan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/engine/model"
)

// fileEntry 是枚举后的一个候选文件。size < 0 表示大小读取失败。
type fileEntry struct {
	path    string
	size    int64
	modTime time.Time
}

// enumerate 递归枚举全部根目录，应用 hidden / excludes / minSize / 链接过滤。
// 返回值 files 保持确定性的路径序（根内按目录逐层，ReadDir 已排序）。
func enumerate(ctx context.Context, req model.Request, d deps) ([]fileEntry, []model.Skip, error) {
	patterns := splitExcludes(req.Excludes)
	hidden := d.hidden

	var files []fileEntry
	var skipped []model.Skip

	for _, root := range req.Roots {
		info, err := os.Stat(root)
		if err != nil {
			return nil, skipped, err
		}
		if !info.IsDir() {
			return nil, skipped, &fs.PathError{Op: "scan", Path: root, Err: errNotDir}
		}

		stack := []string{root}
		visited := map[string]bool{}
		if req.FollowLinks {
			if real, err := filepath.EvalSymlinks(root); err == nil {
				visited[real] = true
			}
		}

		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return files, skipped, err
			}
			dir := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			entries, err := os.ReadDir(dir)
			if err != nil {
				skipped = append(skipped, skipFor(dir, err))
				continue
			}
			for _, de := range entries {
				if err := ctx.Err(); err != nil {
					return files, skipped, err
				}
				path := filepath.Join(dir, de.Name())
				isLink := de.Type()&fs.ModeSymlink != 0

				if isLink && !req.FollowLinks {
					continue // 与 C# 一致：ReparsePoint 一律不跟随
				}

				if de.IsDir() {
					// 目录也要按属性过滤；Info() 复用目录枚举结果，无额外系统调用。
					if !req.IncludeHidden {
						fi, err := d.stat(path, de)
						if err != nil || hidden(path, fi) {
							continue
						}
					}
					stack = append(stack, path)
					continue
				}
				if isLink {
					// FollowLinks=true 时才走到这里，解析目标类型。
					ti, err := d.stat(path, nil)
					if err != nil {
						skipped = append(skipped, sizeSkipFor(path, err))
						continue
					}
					if ti.IsDir() {
						real, err := filepath.EvalSymlinks(path)
						if err != nil || visited[real] {
							continue // 断链或环
						}
						visited[real] = true
						stack = append(stack, path)
						continue
					}
					if !req.IncludeHidden && hidden(path, ti) {
						continue
					}
					if excluded(path, patterns) || ti.Size() < req.MinSize {
						continue
					}
					files = append(files, fileEntry{path: path, size: ti.Size(), modTime: ti.ModTime()})
					continue
				}

				// 先取 FileInfo：Windows 属性随目录枚举一起返回，hidden 判定不再额外系统调用。
				fi, err := d.stat(path, de)
				if err != nil {
					files = append(files, fileEntry{path: path, size: -1})
					skipped = append(skipped, sizeSkipFor(path, err))
					continue
				}
				if !req.IncludeHidden && hidden(path, fi) {
					continue
				}
				if excluded(path, patterns) || fi.Size() < req.MinSize {
					continue
				}
				files = append(files, fileEntry{path: path, size: fi.Size(), modTime: fi.ModTime()})
			}
		}
	}
	return files, skipped, nil
}

var errNotDir = errors.New("not a directory")

func skipFor(path string, err error) model.Skip {
	return model.Skip{
		Path:    path,
		Code:    classifyError(err),
		Message: err.Error(),
		Err:     err.Error(),
	}
}

// sizeSkipFor 把"读不到大小"归到 size_failed，同时保留权限/路径超限的精确分类。
func sizeSkipFor(path string, err error) model.Skip {
	code := classifyError(err)
	if code == model.SkipReadFailed || code == model.SkipVanished {
		code = model.SkipSizeFailed
	}
	return model.Skip{Path: path, Code: code, Message: err.Error(), Err: err.Error()}
}

// classifyError 把系统错误映射到冻结的 SkipCode。
func classifyError(err error) model.SkipCode {
	if os.IsPermission(err) {
		return model.SkipPermissionDenied
	}
	if os.IsNotExist(err) {
		return model.SkipVanished
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "too long") ||
		strings.Contains(msg, "filename or extension") ||
		strings.Contains(err.Error(), "文件名或扩展名太长") ||
		strings.Contains(err.Error(), "路径过长") {
		return model.SkipPathTooLong
	}
	return model.SkipReadFailed
}
