// Package timeline 按拍摄/修改时间把文件分到日、周、月、年目录，并支持移动或复制。
// 计划阶段只读元数据；执行阶段串行处理并在每个文件回调进度，方便 UI 轮询。
package timeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/orgscan"
)

// Bucket 是分桶粒度。
type Bucket string

const (
	BucketDay   Bucket = "day"
	BucketWeek  Bucket = "week"
	BucketMonth Bucket = "month"
	BucketYear  Bucket = "year"
)

// Action 是目标目录内文件处理方式。
type Action string

const (
	ActionMove Action = "move"
	ActionCopy Action = "copy"
)

// Conflict 是同名目标已存在时的策略。
type Conflict string

const (
	ConflictRename    Conflict = "rename"
	ConflictSkip      Conflict = "skip"
	ConflictOverwrite Conflict = "overwrite"
)

// Item 是计划中的单个文件。
type Item struct {
	Source   string `json:"source"`
	Dest     string `json:"dest"`
	RelPath  string `json:"relPath"`
	Bucket   string `json:"bucket"`
	When     string `json:"when"`
	WhenFrom string `json:"whenFrom"` // exif 或 mtime
	Size     int64  `json:"size"`
	// Status 取 ready / conflict / skipped / error。
	Status string `json:"status"`
	Note   string `json:"note"`
	IsDir  bool   `json:"isDir"`
}

// Plan 是一次预演结果。Items 只包含文件；目录按需建。
type Plan struct {
	Root       string   `json:"root"`
	Target     string   `json:"target"`
	Items      []Item   `json:"items"`
	Total      int      `json:"total"`
	Ready      int      `json:"ready"`
	Conflicts  int      `json:"conflicts"`
	Skipped    int      `json:"skipped"`
	Errored    int      `json:"errored"`
	TotalSize  int64    `json:"totalSize"`
	Truncated  bool     `json:"truncated"`
	Notes      []string `json:"notes"`
	PlannedFor string   `json:"plannedFor"`
}

// Options 是计划与执行共用的参数。
type Options struct {
	Root          string
	Target        string
	Bucket        Bucket
	Action        Action
	Conflict      Conflict
	IncludeHidden bool
	// Limit 大于 0 时只保留前 Limit 条条目（用于预览截断），执行时传 0。
	Limit int
}

// Result 是执行汇总。
type Result struct {
	Moved     int      `json:"moved"`
	Copied    int      `json:"copied"`
	Skipped   int      `json:"skipped"`
	Overwrote int      `json:"overwrote"`
	Failed    int      `json:"failed"`
	Bytes     int64    `json:"bytes"`
	Errors    []string `json:"errors"`
}

func validate(opts Options) error {
	if strings.TrimSpace(opts.Root) == "" {
		return errors.New("未选择源目录")
	}
	if strings.TrimSpace(opts.Target) == "" {
		return errors.New("未选择目标目录")
	}
	switch opts.Bucket {
	case BucketDay, BucketWeek, BucketMonth, BucketYear:
	default:
		return fmt.Errorf("未知分桶粒度：%s", opts.Bucket)
	}
	switch opts.Action {
	case ActionMove, ActionCopy:
	default:
		return fmt.Errorf("未知处理方式：%s", opts.Action)
	}
	switch opts.Conflict {
	case ConflictRename, ConflictSkip, ConflictOverwrite:
	default:
		return fmt.Errorf("未知冲突策略：%s", opts.Conflict)
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("源目录不可读：%w", err)
	}
	if info.IsDir() == false {
		return errors.New("源路径不是目录")
	}
	if _, err := filepath.Abs(opts.Target); err != nil {
		return err
	}
	return nil
}

// BucketDir 返回时间对应的分桶目录名。
func BucketDir(t time.Time, b Bucket) string {
	switch b {
	case BucketDay:
		return t.Format("2006-01-02")
	case BucketWeek:
		y, w := t.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	case BucketMonth:
		return t.Format("2006-01")
	case BucketYear:
		return t.Format("2006")
	default:
		return t.Format("2006-01-02")
	}
}

// PlanTree 遍历源目录并生成计划。目标目录位于源目录内时，目标子树自动跳过，
// 避免把已整理的文件再次搬回去。
func PlanTree(opts Options) (Plan, error) {
	if err := validate(opts); err != nil {
		return Plan{}, err
	}
	root, _ := filepath.Abs(opts.Root)
	target, _ := filepath.Abs(opts.Target)
	p := Plan{
		Root:       root,
		Target:     target,
		PlannedFor: time.Now().Format(time.RFC3339),
	}
	skipTarget := pathWithin(target, root)
	if skipTarget {
		p.Notes = append(p.Notes, "目标目录位于源目录内，计划会跳过目标子树，避免重复整理。")
	}

	// destUsed 记录本次计划已占用的目标路径（不区分大小写），用于预演阶段发现重名。
	destUsed := make(map[string]bool)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipTarget && pathWithin(path, target) && !samePath(path, root) {
				return filepath.SkipDir
			}
			if !opts.IncludeHidden {
				if _, hidden, system := orgscan.FileFlags(path); hidden || system {
					if !samePath(path, root) {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && !opts.IncludeHidden {
			p.Skipped++
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			p.Errored++
			p.addItem(Item{Source: path, Status: "error", Note: ierr.Error()}, opts.Limit)
			return nil
		}
		if !opts.IncludeHidden {
			if _, hidden, system := orgscan.FileFlags(path); hidden || system {
				p.Skipped++
				return nil
			}
		}
		if !info.Mode().IsRegular() {
			p.Skipped++
			return nil
		}
		when, from := fileWhen(path, info.ModTime())
		bucket := BucketDir(when, opts.Bucket)
		rel, _ := filepath.Rel(root, path)
		base := d.Name()
		dest := filepath.Join(target, bucket, base)
		status := "ready"
		note := ""
		key := strings.ToLower(dest)
		if destUsed[key] || fileExists(dest) {
			switch opts.Conflict {
			case ConflictSkip:
				status = "skipped"
				note = "目标已存在，按冲突策略跳过"
				p.Skipped++
				p.Conflicts++
			case ConflictOverwrite:
				status = "conflict"
				note = "目标已存在，执行时覆盖"
				p.Conflicts++
			case ConflictRename:
				dest = uniqueDest(filepath.Join(target, bucket), base, destUsed)
				key = strings.ToLower(dest)
				status = "conflict"
				note = "目标已存在，改名为 " + filepath.Base(dest)
				p.Conflicts++
			}
		}
		if status == "ready" || status == "conflict" {
			destUsed[key] = true
			p.Ready++
			p.TotalSize += info.Size()
		}
		p.Total++
		p.addItem(Item{
			Source:   path,
			Dest:     dest,
			RelPath:  filepath.ToSlash(rel),
			Bucket:   bucket,
			When:     when.Format("2006-01-02 15:04:05"),
			WhenFrom: from,
			Size:     info.Size(),
			Status:   status,
			Note:     note,
		}, opts.Limit)
		return nil
	})
	if err != nil {
		return p, err
	}
	return p, nil
}

func (p *Plan) addItem(it Item, limit int) {
	if limit > 0 && len(p.Items) >= limit {
		p.Truncated = true
		return
	}
	p.Items = append(p.Items, it)
}

// Execute 按计划落盘。move 使用 os.Rename，跨卷时退化为复制 + 删除；
// copy 先写临时文件、校验大小后原子重命名，失败清理半截文件。
func Execute(ctx context.Context, opts Options) (Result, error) {
	plan, err := PlanTree(opts)
	if err != nil {
		return Result{}, err
	}
	return ExecutePlan(ctx, opts, plan, nil)
}

// ExecutePlan 执行已经生成的计划；onFile 在每处理一个计划条目后回调，可为 nil。
func ExecutePlan(ctx context.Context, opts Options, plan Plan, onFile func(idx int, it Item, err error)) (Result, error) {
	var res Result
	for i, it := range plan.Items {
		if ctx != nil {
			select {
			case <-ctx.Done():
				res.Errors = append(res.Errors, "已取消")
				return res, ctx.Err()
			default:
			}
		}
		if it.Status == "skipped" || it.Status == "error" {
			res.Skipped++
			if onFile != nil {
				onFile(i, it, nil)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(it.Dest), 0o755); err != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s：创建目录失败：%v", it.Source, err))
			if onFile != nil {
				onFile(i, it, err)
			}
			continue
		}
		var ferr error
		switch opts.Action {
		case ActionCopy:
			ferr = copyFile(it.Source, it.Dest, opts.Conflict == ConflictOverwrite)
			if ferr == nil {
				res.Copied++
				res.Bytes += it.Size
			}
		default:
			overwrote, mres := moveFile(it.Source, it.Dest, opts.Conflict == ConflictOverwrite)
			if mres != nil {
				ferr = mres
			} else {
				res.Moved++
				res.Bytes += it.Size
				if overwrote {
					res.Overwrote++
				}
			}
		}
		if ferr != nil {
			res.Failed++
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", it.Source, ferr))
		}
		if onFile != nil {
			onFile(i, it, ferr)
		}
	}
	return res, nil
}

func moveFile(src, dst string, overwrite bool) (bool, error) {
	overwrote := false
	if fileExists(dst) {
		if !overwrite {
			return false, fmt.Errorf("目标已存在：%s", dst)
		}
		if err := os.Remove(dst); err != nil {
			return false, err
		}
		overwrote = true
	}
	if err := os.Rename(src, dst); err == nil {
		return overwrote, nil
	}
	// 跨卷：复制后删除源文件；复制失败保留源文件。
	if err := copyFile(src, dst, overwrite); err != nil {
		return overwrote, err
	}
	if err := os.Remove(src); err != nil {
		return overwrote, fmt.Errorf("已复制但源文件删除失败：%w", err)
	}
	return overwrote, nil
}

func copyFile(src, dst string, overwrite bool) error {
	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("目标已存在：%s", dst)
		}
	}
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	sfi, err := sf.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".fdedup-tmp"
	df, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, sfi.Mode().Perm())
	if err != nil {
		return err
	}
	_, cerr := io.Copy(df, sf)
	serr := df.Sync()
	clerr := df.Close()
	if cerr != nil || serr != nil || clerr != nil {
		_ = os.Remove(tmp)
		if cerr != nil {
			return cerr
		}
		if serr != nil {
			return serr
		}
		return clerr
	}
	di, err := os.Stat(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if di.Size() != sfi.Size() {
		_ = os.Remove(tmp)
		return errors.New("复制后尺寸不一致")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func uniqueDest(dir, base string, used map[string]bool) string {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 2; i < 10000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if !used[strings.ToLower(cand)] && !fileExists(cand) {
			return cand
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, time.Now().UnixNano(), ext))
}

// pathWithin 判断 child 是否位于 parent 之内（含相等）。按目录分隔符比较，避免
// "C:\\ab" 被误判为 "C:\\a" 的子路径。
func pathWithin(child, parent string) bool {
	c := filepath.Clean(child)
	p := filepath.Clean(parent)
	if samePath(c, p) {
		return true
	}
	return strings.HasPrefix(strings.ToLower(c), strings.ToLower(p)+string(filepath.Separator))
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
