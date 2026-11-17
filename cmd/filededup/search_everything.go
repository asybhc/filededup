//go:build desktop

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"filededup/internal/engine/search"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// SearchBackendInfo 是搜索页可选的检索后端。
type SearchBackendInfo struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Message   string `json:"message,omitempty"`
}

type SearchBackends struct {
	Backends []SearchBackendInfo `json:"backends"`
}

// GetSearchBackends 返回本地后端与 Everything 后端的可用状态。
// 不可用时给出原因与修复方式，前端据此禁用选项，不出现点了没反应的按钮。
func (a *App) GetSearchBackends() SearchBackends {
	local := SearchBackendInfo{
		Key:       "local",
		Name:      "本地文件系统",
		Available: true,
		Message:   "实时遍历所选目录，不依赖外部工具。",
	}
	everything := SearchBackendInfo{
		Key:  "everything",
		Name: "Everything（es.exe）",
	}
	if p, err := findEverythingCLI(); err == nil {
		everything.Available = true
		everything.Path = p
		everything.Message = "由 Everything 索引返回候选，再按本页的扩展名、大小、隐藏与排除条件过滤。"
		if !everythingProcessRunning() {
			everything.Message += " 当前未检测到运行中的 Everything，查询前请先启动它。"
		}
	} else {
		everything.Message = err.Error()
	}
	return SearchBackends{Backends: []SearchBackendInfo{local, everything}}
}

// findEverythingCLI 定位 Everything 的命令行客户端 es.exe。
// Everything 1.5 安装包不再附带 es.exe，位置不受安装目录约束，
// 因此按「PATH → 注册表/服务/常见安装目录 → 程序目录有界搜索」逐级探测。
func findEverythingCLI() (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("Everything 后端仅支持 Windows")
	}
	if p, err := exec.LookPath("es.exe"); err == nil {
		return p, nil
	}
	for _, p := range everythingCandidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("未找到 es.exe：Everything 1.5 安装包不再自带该命令行工具。请从 voidtools 下载 ES，并把 es.exe 放入 PATH 或 Everything 安装目录，然后在搜索页点“重新检测”")
}

// everythingCandidatePaths 列出 PATH 之外的 es.exe 候选路径（注册表、服务、常见目录、有界搜索）。
func everythingCandidatePaths() []string {
	var out []string
	for _, dir := range everythingWindowsExtraDirs() {
		if dir != "" {
			out = append(out, filepath.Join(dir, "es.exe"))
		}
	}
	if p := everythingScannedES(); p != "" {
		out = append(out, p)
	}
	return out
}

// runEverythingSearch 用 es.exe 查询每个根目录，再按与本地后端一致的界面条件过滤。
// es.exe 只负责给出候选路径；扩展名、大小、隐藏/系统属性与排除目录仍由这里判断，
// 因此两个后端对同一条件的最终筛选口径一致。
func runEverythingSearch(ctx context.Context, cli string, req search.Request, progress func(search.Progress)) (*search.Result, error) {
	if cli == "" {
		var err error
		cli, err = findEverythingCLI()
		if err != nil {
			return nil, err
		}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = search.DefaultLimit
	}
	filter := newEverythingFilter(req)
	res := &search.Result{Limit: limit}
	seen := make(map[string]bool, 16)
	var visited int64
	var unusable []string

	for _, root := range req.Roots {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		// es.exe 对不存在的路径会静默返回空结果并以 0 退出，
		// 这里先自查目录，避免界面显示「0 条结果」却没有任何原因。
		st, statErr := os.Stat(root)
		if statErr != nil {
			unusable = append(unusable, fmt.Sprintf("%s（%v）", root, statErr))
			res.Skipped = append(res.Skipped, search.Skip{Path: root, Reason: "目录不可访问：" + statErr.Error()})
			continue
		}
		if !st.IsDir() {
			unusable = append(unusable, root+"（不是目录）")
			res.Skipped = append(res.Skipped, search.Skip{Path: root, Reason: "不是目录"})
			continue
		}
		remain := limit - len(res.Matches)
		if remain <= 0 {
			res.Truncated = true
			break
		}
		args := everythingArgs(remain, root, req.Query)
		cmd := exec.CommandContext(ctx, cli, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		lines := everythingLines(out)
		visited += int64(len(lines))
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				reason := strings.TrimSpace(decodeConsole(stderr.Bytes()))
				if reason == "" {
					reason = err.Error()
				}
				// 一个根查询失败时保留已得结果，只登记跳过原因。
				if len(res.Matches) == 0 && len(res.Skipped) == 0 {
					return nil, fmt.Errorf("Everything 查询失败：%s", reason)
				}
				res.Skipped = append(res.Skipped, search.Skip{Path: root, Reason: reason})
				continue
			}
			return nil, fmt.Errorf("无法运行 Everything（es.exe）：%w", err)
		}
		// es.exe 在 Everything 未运行时不会报错，只会输出空结果；
		// 这种情况下必须把真实原因透出，而不是让界面显示 0 条。
		if len(lines) == 0 && len(res.Matches) == 0 && !everythingProcessRunning() {
			return nil, fmt.Errorf("es.exe 没有返回任何结果：未检测到运行中的 Everything（%s）", cli)
		}

		hitLimit := len(lines) >= remain
		for _, p := range lines {
			if len(res.Matches) >= limit {
				res.Truncated = true
				break
			}
			key := strings.ToLower(p)
			if seen[key] {
				continue
			}
			seen[key] = true
			m, ok := filter.match(p, root)
			if ok {
				res.Matches = append(res.Matches, m)
			}
		}
		if hitLimit {
			res.Truncated = true
		}
		if progress != nil {
			progress(search.Progress{
				Visited:   visited,
				Matched:   int64(len(res.Matches)),
				Truncated: res.Truncated,
			})
		}
		if res.Truncated && len(res.Matches) >= limit {
			break
		}
	}

	res.Visited = visited
	// 所有根目录都不可访问时给出明确原因，而不是交出一份空结果。
	if len(req.Roots) > 0 && len(unusable) == len(req.Roots) {
		return nil, fmt.Errorf("搜索目录不可用：%s", strings.Join(unusable, "；"))
	}
	res.Truncated = res.Truncated || len(res.Matches) >= limit
	sort.Slice(res.Matches, func(i, j int) bool {
		a, b := strings.ToLower(res.Matches[i].Path), strings.ToLower(res.Matches[j].Path)
		if a != b {
			return a < b
		}
		return res.Matches[i].Path < res.Matches[j].Path
	})
	return res, nil
}

// everythingArgs 组装 es.exe 的查询参数：-n 限制条数，-path 限定根目录，空关键词表示列出该目录全部条目。
func everythingArgs(remain int, root, query string) []string {
	args := []string{"-n", strconv.Itoa(remain), "-path", root}
	if q := strings.TrimSpace(query); q != "" {
		args = append(args, q)
	}
	return args
}

// everythingLines 解码 es.exe 输出并按行拆成绝对路径。控制台输出在中文 Windows
// 上可能是 GBK，因此先按 UTF-8 判断，失败再按 GBK 解码。
func everythingLines(out []byte) []string {
	text := decodeConsole(out)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := make([]string, 0, 64)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			// 不是路径的行（提示/统计）直接忽略，避免污染结果表。
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func decodeConsole(b []byte) string {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(b), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return string(b)
	}
	return string(decoded)
}

// everythingExcludeSplit 与引擎排除项的拆分规则保持一致：[,，;；\s]+。
var everythingExcludeSplit = regexp.MustCompile(`[,，;；\s]+`)

type everythingFilter struct {
	req      search.Request
	exts     map[string]bool
	excludes []string
}

func newEverythingFilter(req search.Request) *everythingFilter {
	f := &everythingFilter{req: req}
	if len(req.Exts) > 0 {
		f.exts = make(map[string]bool, len(req.Exts))
		for _, e := range req.Exts {
			e = strings.ToLower(strings.TrimSpace(e))
			e = strings.TrimPrefix(e, ".")
			if e != "" {
				f.exts[e] = true
			}
		}
	}
	for _, chunk := range req.Excludes {
		for _, part := range everythingExcludeSplit.Split(chunk, -1) {
			part = strings.TrimSpace(part)
			part = strings.TrimRight(part, `\/`)
			if part != "" {
				f.excludes = append(f.excludes, part)
			}
		}
	}
	return f
}

// match 把一条候选路径转成结果行；任一界面条件不满足就丢弃。
func (f *everythingFilter) match(path, root string) (search.Match, bool) {
	if f.excluded(path) {
		return search.Match{}, false
	}
	if f.exts != nil {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
		if ext == "" || !f.exts[ext] {
			return search.Match{}, false
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		// Everything 索引里包含目录；本页只列文件。
		return search.Match{}, false
	}
	size := info.Size()
	if size < f.req.MinSize {
		return search.Match{}, false
	}
	if f.req.MaxSize > 0 && size > f.req.MaxSize {
		return search.Match{}, false
	}
	if !f.req.IncludeHidden && searchHiddenFile(path, info) {
		return search.Match{}, false
	}
	return search.Match{
		Path:    path,
		Name:    info.Name(),
		Dir:     filepath.Dir(path),
		Size:    size,
		ModTime: info.ModTime().UTC().Format(time.RFC3339),
		Root:    root,
	}, true
}

// excluded 只按目录匹配，语义与引擎排除项一致：带分隔符/盘符的按路径前缀，
// 纯名称项与父目录链的任一层比较。
func (f *everythingFilter) excluded(path string) bool {
	if len(f.excludes) == 0 {
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
	for _, p := range f.excludes {
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
