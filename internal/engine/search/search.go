// Package search 对多个根目录做文件名 / 路径搜索。
//
// 设计取舍：
//   - 不做落盘索引：索引需要文件系统监视或定期重建，否则结果可能过期；一旦过期，
//     “搜不到”比“慢一点”更难解释。这里每次搜索实时遍历，用 context 取消与结果上限
//     控制开销，保证结果与磁盘状态一致。
//   - 不跟随符号链接：避免目录环与跨盘遍历，和查重引擎的默认行为保持一致。
//   - 结果在内存中按上限截断；达到上限立即停止遍历并标记 Truncated，而不是继续扫完
//     再丢弃，避免大目录把一次搜索拖成分钟级。
package search

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 搜索模式。
const (
	ModeSubstring = "substring"
	ModeWildcard  = "wildcard"
)

// 结果上限：默认 20 万条，最多 100 万条。
const (
	DefaultLimit = 200000
	MaxLimit     = 1000000
)

// Request 是一次搜索的条件。零值不安全，调用方应先走 Validate。
type Request struct {
	Roots         []string `json:"roots"`
	Query         string   `json:"query"` // 空表示不过滤，返回全部文件
	Mode          string   `json:"mode"`  // substring | wildcard
	Exts          []string `json:"exts"`  // 例如 ["jpg","png"]；空表示不限
	MinSize       int64    `json:"minSize"`
	MaxSize       int64    `json:"maxSize"` // 0 表示不限
	IncludeHidden bool     `json:"includeHidden"`
	Excludes      []string `json:"excludes"`
	Limit         int      `json:"limit"` // 0 用 DefaultLimit
}

// Match 是一条命中结果。
type Match struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"` // RFC3339
	Root    string `json:"root"`
}

// Skip 记录被跳过的目录或文件及原因（最多保留前 200 条）。
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Result 是一次搜索的完整结果。
type Result struct {
	Matches   []Match
	Skipped   []Skip
	Visited   int64
	Dirs      int64
	Truncated bool
	Limit     int
}

// Options 是可选注入点；零值即生产默认。
type Options struct {
	// HiddenFile 判断路径是否带隐藏/系统属性。为 nil 时 Windows 一律 false
	//（与查重引擎一致），其他平台用 "." 前缀。
	HiddenFile func(path string, info fs.FileInfo) bool
	// Progress 是只读进度回调，可能被单线程调用；实现方不必考虑并发。
	Progress func(Progress)
	// MaxSkips 限制跳过记录条数，0 用 200。
	MaxSkips int
}

// Progress 描述搜索进度。
type Progress struct {
	Visited   int64  `json:"visited"`
	Dirs      int64  `json:"dirs"`
	Matched   int64  `json:"matched"`
	Current   string `json:"current"`
	Truncated bool   `json:"truncated"`
}

// Validate 检查并规范化请求。
func (r *Request) Validate() error {
	if len(r.Roots) == 0 {
		return errors.New("请至少选择一个搜索目录")
	}
	seen := make(map[string]bool, len(r.Roots))
	roots := make([]string, 0, len(r.Roots))
	for _, raw := range r.Roots {
		root := strings.TrimSpace(raw)
		if root == "" {
			continue
		}
		if !filepath.IsAbs(root) {
			return fmt.Errorf("目录必须是绝对路径：%s", root)
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("无法访问目录 %s：%w", root, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("不是目录：%s", root)
		}
		key := strings.ToLower(filepath.Clean(root))
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, filepath.Clean(root))
	}
	if len(roots) == 0 {
		return errors.New("请至少选择一个搜索目录")
	}
	r.Roots = roots

	switch r.Mode {
	case "", ModeSubstring:
		r.Mode = ModeSubstring
	case ModeWildcard:
	default:
		return fmt.Errorf("不支持的搜索模式：%s", r.Mode)
	}
	if r.Mode == ModeWildcard {
		if _, err := path.Match(normalizePattern(r.Query), ""); err != nil {
			return fmt.Errorf("通配符写法有误：%w", err)
		}
	}
	if r.MinSize < 0 {
		return errors.New("最小大小不能为负")
	}
	if r.MaxSize < 0 {
		return errors.New("最大大小不能为负")
	}
	if r.MaxSize > 0 && r.MinSize > r.MaxSize {
		return errors.New("最小大小不能大于最大大小")
	}
	if r.Limit <= 0 {
		r.Limit = DefaultLimit
	}
	if r.Limit > MaxLimit {
		r.Limit = MaxLimit
	}
	r.Exts = normalizeExts(r.Exts)
	return nil
}

// Run 执行一次搜索。被取消时返回已完成的部分结果与 context.Canceled。
func Run(ctx context.Context, req Request, opts Options) (*Result, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	hidden := opts.HiddenFile
	if hidden == nil {
		hidden = platformHidden
	}
	maxSkips := opts.MaxSkips
	if maxSkips <= 0 {
		maxSkips = 200
	}
	matcher, err := newMatcher(&req)
	if err != nil {
		return nil, err
	}
	res := &Result{Limit: req.Limit}
	patterns := splitExcludes(req.Excludes)
	seenDirs := make(map[string]bool)

	report := func(current string) {
		if opts.Progress == nil {
			return
		}
		opts.Progress(Progress{
			Visited:   res.Visited,
			Dirs:      res.Dirs,
			Matched:   int64(len(res.Matches)),
			Current:   current,
			Truncated: res.Truncated,
		})
	}

	for _, root := range req.Roots {
		stack := []string{root}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return res, err
			}
			dir := stack[len(stack)-1]
			stack = stack[:len(stack)-1]

			key := strings.ToLower(dir)
			if seenDirs[key] {
				continue
			}
			seenDirs[key] = true
			res.Dirs++

			entries, rerr := os.ReadDir(dir)
			if rerr != nil {
				if len(res.Skipped) < maxSkips {
					res.Skipped = append(res.Skipped, Skip{Path: dir, Reason: rerr.Error()})
				}
				continue
			}
			for _, de := range entries {
				if err := ctx.Err(); err != nil {
					return res, err
				}
				name := de.Name()
				full := filepath.Join(dir, name)
				if de.Type()&fs.ModeSymlink != 0 {
					if len(res.Skipped) < maxSkips {
						res.Skipped = append(res.Skipped, Skip{Path: full, Reason: "符号链接，不跟随"})
					}
					continue
				}
				if de.IsDir() {
					if excluded(full, patterns) {
						continue
					}
					if !req.IncludeHidden {
						info, ierr := de.Info()
						if ierr != nil || hidden(full, info) {
							continue
						}
					}
					stack = append(stack, full)
					continue
				}

				res.Visited++
				report(full)
				info, ierr := de.Info()
				if ierr != nil {
					if len(res.Skipped) < maxSkips {
						res.Skipped = append(res.Skipped, Skip{Path: full, Reason: ierr.Error()})
					}
					continue
				}
				if !req.IncludeHidden && hidden(full, info) {
					continue
				}
				if excluded(full, patterns) {
					continue
				}
				size := info.Size()
				if size < req.MinSize {
					continue
				}
				if req.MaxSize > 0 && size > req.MaxSize {
					continue
				}
				if !matcher.matches(name, root, full) {
					continue
				}
				res.Matches = append(res.Matches, Match{
					Path:    full,
					Name:    name,
					Dir:     dir,
					Size:    size,
					ModTime: info.ModTime().UTC().Format(time.RFC3339),
					Root:    root,
				})
				if len(res.Matches) >= req.Limit {
					res.Truncated = true
					report(full)
					return res, nil
				}
			}
		}
	}
	report("")
	return res, nil
}

// matcher 封装匹配条件，避免在遍历里反复归一化。
type matcher struct {
	req     *Request
	query   string
	pattern string
	exts    map[string]bool
}

func newMatcher(req *Request) (*matcher, error) {
	m := &matcher{
		req:     req,
		query:   strings.ToLower(strings.TrimSpace(req.Query)),
		pattern: normalizePattern(req.Query),
	}
	if len(req.Exts) > 0 {
		m.exts = make(map[string]bool, len(req.Exts))
		for _, e := range req.Exts {
			m.exts[e] = true
		}
	}
	if req.Mode == ModeWildcard && m.pattern != "" && !strings.ContainsAny(m.pattern, "*?") {
		// 用户选通配符但没写 * ?：按子串处理，避免必须写成 *jpg* 才能命中。
		m.pattern = "*" + m.pattern + "*"
	}
	if req.Mode == ModeWildcard {
		if _, err := path.Match(m.pattern, ""); err != nil {
			return nil, fmt.Errorf("通配符写法有误：%w", err)
		}
	}
	return m, nil
}

// matches 判断单个文件是否命中。root 用于计算相对路径，保证多根目录下路径匹配正确。
func (m *matcher) matches(name, root, full string) bool {
	if m.exts != nil {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if !m.exts[ext] {
			return false
		}
	}
	if m.query == "" {
		return true
	}
	lowerName := strings.ToLower(name)
	relPath := func() string {
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return full
		}
		return strings.ToLower(filepath.ToSlash(rel))
	}
	if m.req.Mode == ModeWildcard {
		target := lowerName
		if strings.Contains(m.pattern, "/") {
			target = relPath()
		}
		ok, err := path.Match(m.pattern, target)
		return err == nil && ok
	}
	if strings.Contains(lowerName, m.query) {
		return true
	}
	return strings.Contains(relPath(), m.query)
}

// Sort 按指定字段排序。key 取 name / path / size / modTime，未知值按 path。
// 名称与路径忽略大小写，数值按原值。
func Sort(matches []Match, key string, desc bool) {
	less := func(i, j int) bool { return strings.ToLower(matches[i].Path) < strings.ToLower(matches[j].Path) }
	switch key {
	case "name":
		less = func(i, j int) bool {
			a, b := strings.ToLower(matches[i].Name), strings.ToLower(matches[j].Name)
			if a == b {
				return strings.ToLower(matches[i].Path) < strings.ToLower(matches[j].Path)
			}
			return a < b
		}
	case "size":
		less = func(i, j int) bool {
			if matches[i].Size == matches[j].Size {
				return strings.ToLower(matches[i].Path) < strings.ToLower(matches[j].Path)
			}
			return matches[i].Size < matches[j].Size
		}
	case "modTime":
		less = func(i, j int) bool {
			if matches[i].ModTime == matches[j].ModTime {
				return strings.ToLower(matches[i].Path) < strings.ToLower(matches[j].Path)
			}
			return matches[i].ModTime < matches[j].ModTime
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if desc {
			return less(j, i)
		}
		return less(i, j)
	})
}

func normalizePattern(q string) string {
	q = strings.ToLower(strings.TrimSpace(q))
	return strings.ReplaceAll(q, `\`, "/")
}

func normalizeExts(raw []string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, chunk := range raw {
		for _, part := range strings.FieldsFunc(chunk, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == ' ' || r == '\t' || r == '\n'
		}) {
			e := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(part)), ".")
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

// splitExcludes 拆分排除项（逗号/分号/空白），并去掉尾部路径分隔符。
func splitExcludes(raw []string) []string {
	var out []string
	for _, chunk := range raw {
		for _, part := range strings.FieldsFunc(chunk, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || r == ' ' || r == '\t' || r == '\n'
		}) {
			part = strings.TrimSpace(part)
			part = strings.TrimRight(part, `\/`)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// excluded 与查重引擎的排除语义一致：带分隔符/盘符按路径前缀，纯名称按父目录链比较。
func excluded(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	slashPath := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	dir := slashPath
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i]
	} else {
		dir = ""
	}
	var comps []string
	if dir != "" && dir != "." {
		comps = strings.Split(strings.Trim(dir, "/"), "/")
	}
	for _, p := range patterns {
		if strings.ContainsAny(p, `\/`) || strings.Contains(p, ":") {
			want := strings.ToLower(strings.ReplaceAll(p, `\`, "/"))
			want = strings.TrimRight(want, "/")
			if slashPath == want || strings.HasPrefix(slashPath, want+"/") {
				return true
			}
			continue
		}
		want := strings.ToLower(p)
		for _, c := range comps {
			if c == want {
				return true
			}
		}
	}
	return false
}
