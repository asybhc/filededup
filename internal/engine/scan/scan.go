package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"filededup/internal/engine/model"
)

// Options 是引擎的可选注入点。零值即生产默认。
//
// HiddenFile 用于判断路径是否带隐藏/系统属性。info 来自目录枚举（Windows 下已含
// 文件属性），可能为 nil。引擎本身不做系统调用（arch_test 约束）；Windows 外壳在
// cmd/fdedup 注入基于属性的实现。为 nil 时用平台默认：Windows 一律 false
// （与 C# 一致，C# 只看属性），其他平台用 "." 前缀。
type Options struct {
	HiddenFile func(path string, info fs.FileInfo) bool
	// Progress 是只读进度回调（枚举与哈希两个阶段）。为 nil 时不产生任何额外开销。
	// 回调可能来自多个哈希 worker，实现方需自行保证并发安全。
	Progress func(Progress)
}

// Progress 描述扫描进度。计数只增不减，供 UI 显示，不参与结果与对拍。
type Progress struct {
	Phase           string `json:"phase"` // enumerate | hash
	Files           int64  `json:"files"`
	Bytes           int64  `json:"bytes"`
	HashTargets     int64  `json:"hashTargets"`
	HashDone        int64  `json:"hashDone"`
	HashBytes       int64  `json:"hashBytes"`
	HashTotalBytes  int64  `json:"hashTotalBytes"`
	EnumerationDone bool   `json:"enumerationDone"`
}

// Run 执行一次只读精确扫描。被取消时返回已完成的部分结果与 context.Canceled。
func Run(ctx context.Context, req model.Request) (*model.Result, error) {
	return RunWithOptions(ctx, req, Options{})
}

// deps 是测试注入点；生产路径全部走零值默认实现。
type deps struct {
	hidden   func(string, fs.FileInfo) bool
	stat     func(path string, de fs.DirEntry) (fs.FileInfo, error)
	open     func(string) (io.ReadCloser, error)
	progress func(Progress)
}

func defaultDeps(opts Options) deps {
	hidden := opts.HiddenFile
	if hidden == nil {
		hidden = platformHidden
	}
	return deps{
		hidden:   hidden,
		progress: opts.Progress,
		stat: func(path string, de fs.DirEntry) (fs.FileInfo, error) {
			if de != nil {
				return de.Info()
			}
			return os.Stat(path)
		},
		open: func(path string) (io.ReadCloser, error) { return os.Open(path) },
	}
}

// RunWithOptions 是 Run 的可注入版本。
func RunWithOptions(ctx context.Context, req model.Request, opts Options) (*model.Result, error) {
	return run(ctx, req, defaultDeps(opts))
}

func run(ctx context.Context, req model.Request, d deps) (*model.Result, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if !req.Exact && req.SimilarImage == nil && req.SimilarAudio == nil {
		return nil, fmt.Errorf("scan: 未启用任何查重方式（exact/similarImage/similarAudio）")
	}
	if req.KeepPolicy == "" {
		req.KeepPolicy = model.KeepPathShortest
	}
	workers := effectiveWorkers(req.Workers)
	started := time.Now()

	files, skipped, err := enumerate(ctx, req, d)
	if err != nil {
		return nil, err
	}

	cache := newHashCache(0, 0)
	if !req.UseCache {
		cache.disable()
	}
	var results []hashResult
	var stats hashStats
	if req.Exact {
		results, stats, err = hashAll(ctx, files, cache, workers, d.open, d.progress)
		if err != nil {
			// 取消：返回部分结果，由调用方决定是否落盘。
			res := buildResult(req, files, results, skipped, stats, workers, started)
			return res, err
		}
		for i, r := range results {
			if r.err != nil && !errors.Is(r.err, context.Canceled) {
				skipped = append(skipped, model.Skip{
					Path:    files[i].path,
					Code:    classifyError(r.err),
					Message: r.err.Error(),
					Err:     r.err.Error(),
				})
			}
		}
	}

	var groups []model.Group
	if req.Exact {
		groups = append(groups, buildGroups(req, files, results)...)
	}
	if req.SimilarImage != nil && req.SimilarImage.Enabled {
		g, sk := buildImageGroups(req, files, *req.SimilarImage)
		groups = append(groups, g...)
		skipped = append(skipped, sk...)
	}
	if req.SimilarAudio != nil && req.SimilarAudio.Enabled {
		g, sk := buildAudioGroups(req, files, *req.SimilarAudio)
		groups = append(groups, g...)
		skipped = append(skipped, sk...)
	}
	// 与 C# 基线一致：相似组内成员同样计算 SHA-256，并计入哈希统计。
	if len(groups) > 0 && (req.SimilarImage != nil && req.SimilarImage.Enabled ||
		req.SimilarAudio != nil && req.SimilarAudio.Enabled) {
		if results == nil {
			results = make([]hashResult, len(files))
		}
		hashMediaGroups(ctx, files, results, groups, cache, d.open, &stats)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Kind != groups[j].Kind {
			return groups[i].Kind < groups[j].Kind
		}
		return groups[i].Key < groups[j].Key
	})

	res := buildResult(req, files, results, skipped, stats, workers, started)
	if groups == nil {
		groups = []model.Group{}
	}
	res.Groups = groups
	fillGroupStats(&res.Stats, groups)
	res.Stats.Skipped = int64(len(res.Skipped))
	return res, nil
}

func effectiveWorkers(n int) int {
	if n > 0 {
		return n
	}
	w := runtime.NumCPU()
	if w > 4 {
		w = 4
	}
	if w < 1 {
		w = 1
	}
	return w
}

func buildResult(req model.Request, files []fileEntry, results []hashResult, skipped []model.Skip,
	st hashStats, workers int, started time.Time) *model.Result {
	finished := time.Now()
	stats := model.Stats{
		FilesEnumerated: int64(len(files)),
		FilesHashed:     st.filesHashed,
		BytesHashed:     st.bytesHashed,
		CacheHits:       st.cacheHits,
		CacheMisses:     st.cacheMisses,
	}
	var bytes int64
	for _, f := range files {
		if f.size > 0 {
			bytes += f.size
		}
	}
	stats.BytesEnumerated = bytes
	sort.Slice(skipped, func(i, j int) bool {
		if skipped[i].Path != skipped[j].Path {
			return skipped[i].Path < skipped[j].Path
		}
		return skipped[i].Code < skipped[j].Code
	})
	stats.Skipped = int64(len(skipped))
	if skipped == nil {
		skipped = []model.Skip{}
	}
	return &model.Result{
		SchemaVersion: model.SchemaVersion,
		RequestID:     req.RequestID,
		Request:       req,
		Stats:         stats,
		Skipped:       skipped,
		Runtime: &model.Runtime{
			StartedAt:  started.UTC().Format(time.RFC3339Nano),
			FinishedAt: finished.UTC().Format(time.RFC3339Nano),
			ElapsedMS:  finished.Sub(started).Milliseconds(),
			Workers:    workers,
			GoVersion:  runtime.Version(),
		},
	}
}

func fillGroupStats(s *model.Stats, groups []model.Group) {
	s.Groups = int64(len(groups))
	s.ExactGroups = 0
	s.ImageGroups = 0
	s.AudioGroups = 0
	for _, g := range groups {
		switch g.Kind {
		case model.GroupExact:
			s.ExactGroups++
		case model.GroupImage:
			s.ImageGroups++
		case model.GroupAudio:
			s.AudioGroups++
		}
		s.DuplicateFiles += int64(len(g.Members) - 1)
		s.ReclaimableBytes += g.Reclaimable
	}
}

// hashStats 聚合并行哈希期间的非确定性计数。
type hashStats struct {
	filesHashed int64
	bytesHashed int64
	cacheHits   int64
	cacheMisses int64
}

type hashResult struct {
	hash    string
	bytes   int64
	cached  bool
	err     error
	skipped bool // true 表示是读/哈希失败，已登记 Skipped
}

// hashAll 对同尺寸桶（成员数 >= 2）计算 SHA-256。返回与 files 等长的结果切片。
func hashAll(ctx context.Context, files []fileEntry, cache *hashCache, workers int,
	open func(string) (io.ReadCloser, error), progress func(Progress)) ([]hashResult, hashStats, error) {
	results := make([]hashResult, len(files))

	buckets := make(map[int64][]int, len(files))
	for i, f := range files {
		if f.size < 0 {
			continue
		}
		buckets[f.size] = append(buckets[f.size], i)
	}
	var targets []int
	for _, idxs := range buckets {
		if len(idxs) >= 2 {
			targets = append(targets, idxs...)
		}
	}
	sort.Ints(targets)
	if len(targets) == 0 {
		return results, hashStats{}, nil
	}

	var totalBytes int64
	for _, idx := range targets {
		if files[idx].size > 0 {
			totalBytes += files[idx].size
		}
	}
	var totalFiles int64
	var enumeratedBytes int64
	for _, f := range files {
		if f.size > 0 {
			enumeratedBytes += f.size
		}
		totalFiles++
	}

	// 进度上报节流：每 100ms 一次，最后一笔强制上报。多 worker 共享，用互斥保护。
	var progressMu sync.Mutex
	var progressDone, progressBytes int64
	var lastReport time.Time
	report := func(done, bytes int64) {
		if progress == nil {
			return
		}
		progressMu.Lock()
		progressDone += done
		progressBytes += bytes
		now := time.Now()
		final := progressDone >= int64(len(targets))
		if !final && now.Sub(lastReport) < 100*time.Millisecond {
			progressMu.Unlock()
			return
		}
		lastReport = now
		p := Progress{
			Phase:          "hash",
			Files:          totalFiles,
			Bytes:          enumeratedBytes,
			HashTargets:    int64(len(targets)),
			HashDone:       progressDone,
			HashBytes:      progressBytes,
			HashTotalBytes: totalBytes,
		}
		progressMu.Unlock()
		progress(p)
	}

	var counter int64
	var wg sync.WaitGroup
	var mu sync.Mutex
	stop := false

	worker := func() {
		defer wg.Done()
		buf := make([]byte, 1<<20)
		for {
			if ctx.Err() != nil {
				mu.Lock()
				stop = true
				mu.Unlock()
				return
			}
			mu.Lock()
			if stop {
				mu.Unlock()
				return
			}
			i := int(counter)
			counter++
			mu.Unlock()
			if i >= len(targets) {
				return
			}
			idx := targets[i]
			f := files[idx]
			key := cacheKey(f)
			if h, ok := cache.get(key); ok {
				results[idx] = hashResult{hash: h, cached: true}
				report(1, 0)
				continue
			}
			h, n, err := hashFile(ctx, f.path, buf, open)
			if err != nil {
				results[idx] = hashResult{err: err, skipped: true}
				report(1, 0)
				continue
			}
			cache.put(key, h)
			results[idx] = hashResult{hash: h, bytes: n}
			report(1, n)
		}
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return results, hashStats{}, ctx.Err()
	}

	var st hashStats
	for _, idx := range targets {
		r := results[idx]
		if r.err != nil {
			continue
		}
		st.filesHashed++
		st.bytesHashed += r.bytes
		if r.cached {
			st.cacheHits++
		} else {
			st.cacheMisses++
		}
	}
	return results, st, nil
}
