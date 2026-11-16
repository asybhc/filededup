//go:build desktop

package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"filededup/internal/auth"
	"filededup/internal/engine/search"
	"filededup/internal/platform"
)

// SearchRequest 是界面的本地搜索参数。
type SearchRequest struct {
	Roots         []string `json:"roots"`
	Query         string   `json:"query"`
	Mode          string   `json:"mode"`
	Exts          []string `json:"exts"`
	MinSize       int64    `json:"minSize"`
	MaxSize       int64    `json:"maxSize"`
	IncludeHidden bool     `json:"includeHidden"`
	Excludes      []string `json:"excludes"`
	Limit         int      `json:"limit"`
	// Backend 选择检索后端：local（实时遍历，默认）或 everything（es.exe 索引）。
	Backend string `json:"backend"`
}

// SearchState 是搜索任务的进度快照。
type SearchState struct {
	ID        string `json:"id"`
	Running   bool   `json:"running"`
	Done      bool   `json:"done"`
	Cancelled bool   `json:"cancelled"`
	Error     string `json:"error,omitempty"`
	Visited   int64  `json:"visited"`
	Dirs      int64  `json:"dirs"`
	Matched   int    `json:"matched"`
	Truncated bool   `json:"truncated"`
	ElapsedMS int64  `json:"elapsedMs"`
	Current   string `json:"current,omitempty"`
	// Backend 是本任务实际使用的检索后端，供界面显示对应的统计口径。
	Backend string `json:"backend"`
}

// SearchResultPage 是一页搜索结果。
type SearchResultPage struct {
	Total     int            `json:"total"`
	Offset    int            `json:"offset"`
	Limit     int            `json:"limit"`
	Rows      []search.Match `json:"rows"`
	Truncated bool           `json:"truncated"`
	SortKey   string         `json:"sortKey"`
	SortDesc  bool           `json:"sortDesc"`
}

type searchJob struct {
	id     string
	cancel context.CancelFunc
	start  time.Time

	mu       sync.Mutex
	state    SearchState
	res      *search.Result
	sortKey  string
	sortDesc bool
}

func (j *searchJob) snapshot() SearchState {
	j.mu.Lock()
	defer j.mu.Unlock()
	st := j.state
	st.ElapsedMS = time.Since(j.start).Milliseconds()
	return st
}

// StartSearch 启动异步搜索并立即返回任务 ID。
func (a *App) StartSearch(req SearchRequest) (string, error) {
	if err := requireLicense(auth.FeatureSearch); err != nil {
		return "", err
	}
	ereq := search.Request{
		Roots:         req.Roots,
		Query:         req.Query,
		Mode:          req.Mode,
		Exts:          req.Exts,
		MinSize:       req.MinSize,
		MaxSize:       req.MaxSize,
		IncludeHidden: req.IncludeHidden,
		Excludes:      req.Excludes,
		Limit:         req.Limit,
	}
	if err := ereq.Validate(); err != nil {
		return "", err
	}

	backend := strings.ToLower(strings.TrimSpace(req.Backend))
	switch backend {
	case "", "local":
		backend = "local"
	case "everything":
		// 后端不可用时立刻报原因，避免建出一个注定失败的任务。
		if _, err := findEverythingCLI(); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("不支持的搜索后端：%s", req.Backend)
	}

	id := fmt.Sprintf("search-%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	job := &searchJob{
		id:      id,
		cancel:  cancel,
		start:   time.Now(),
		state:   SearchState{ID: id, Running: true, Backend: backend},
		sortKey: "path",
	}

	a.searchMu.Lock()
	// 只保留最近 8 个任务，避免长时间运行后内存里堆积大结果集。
	if len(a.searches) >= 8 {
		for key, old := range a.searches {
			if old.snapshot().Done && len(a.searches) > 4 {
				delete(a.searches, key)
			}
		}
	}
	a.searches[id] = job
	a.searchMu.Unlock()

	go func() {
		onProgress := func(p search.Progress) {
			job.mu.Lock()
			job.state.Visited = p.Visited
			job.state.Dirs = p.Dirs
			job.state.Matched = int(p.Matched)
			job.state.Truncated = p.Truncated
			job.state.Current = p.Current
			job.mu.Unlock()
		}
		var res *search.Result
		var err error
		if backend == "everything" {
			res, err = runEverythingSearch(ctx, ereq, onProgress)
		} else {
			res, err = search.Run(ctx, ereq, search.Options{
				HiddenFile: searchHiddenFile,
				Progress:   onProgress,
			})
		}
		job.mu.Lock()
		defer job.mu.Unlock()
		job.state.Running = false
		job.state.Done = true
		if res != nil {
			job.res = res
			job.state.Visited = res.Visited
			job.state.Dirs = res.Dirs
			job.state.Matched = len(res.Matches)
			job.state.Truncated = res.Truncated
		}
		switch {
		case err == context.Canceled:
			job.state.Cancelled = true
		case err != nil:
			job.state.Error = err.Error()
		}
		job.state.Current = ""
	}()
	return id, nil
}

// GetSearchState 返回搜索进度。
func (a *App) GetSearchState(id string) (SearchState, error) {
	job, err := a.searchJob(id)
	if err != nil {
		return SearchState{}, err
	}
	return job.snapshot(), nil
}

// CancelSearch 取消搜索；已完成的任务调用也安全。
func (a *App) CancelSearch(id string) error {
	job, err := a.searchJob(id)
	if err != nil {
		return err
	}
	job.cancel()
	return nil
}

// GetSearchResults 返回排好序的一页结果。排序字段变化时只重排一次。
func (a *App) GetSearchResults(id, sortKey string, desc bool, offset, limit int) (SearchResultPage, error) {
	job, err := a.searchJob(id)
	if err != nil {
		return SearchResultPage{}, err
	}
	switch sortKey {
	case "name", "size", "modTime", "path":
	default:
		sortKey = "path"
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	job.mu.Lock()
	defer job.mu.Unlock()
	if job.res == nil {
		return SearchResultPage{Offset: offset, Limit: limit, SortKey: sortKey, SortDesc: desc}, nil
	}
	res := job.res
	if job.sortKey != sortKey || job.sortDesc != desc {
		search.Sort(res.Matches, sortKey, desc)
		job.sortKey = sortKey
		job.sortDesc = desc
	}
	page := SearchResultPage{
		Total:     len(res.Matches),
		Offset:    offset,
		Limit:     limit,
		Truncated: res.Truncated,
		SortKey:   sortKey,
		SortDesc:  desc,
	}
	if offset >= len(res.Matches) {
		return page, nil
	}
	end := offset + limit
	if end > len(res.Matches) {
		end = len(res.Matches)
	}
	page.Rows = append([]search.Match{}, res.Matches[offset:end]...)
	return page, nil
}

// RevealPath 在资源管理器中定位文件。
func (a *App) RevealPath(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	return platform.RevealInExplorer(path)
}

// OpenPath 用默认程序打开文件或目录。
func (a *App) OpenPath(path string) error {
	if path == "" {
		return fmt.Errorf("路径为空")
	}
	return platform.OpenPath(path)
}

func (a *App) searchJob(id string) (*searchJob, error) {
	a.searchMu.Lock()
	job, ok := a.searches[id]
	a.searchMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("搜索任务不存在：%s", id)
	}
	return job, nil
}
