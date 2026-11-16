package scan

// 64KB 快筛：先读取每个文件的前 64KB 做一次 SHA-256，只有快筛指纹相同的同尺寸文件
// 才进入完整 SHA-256 复核。快筛只用于缩小候选集合，最终分组仍以完整哈希为准，
// 因此开启前后删除决策不会改变（模型层注释要求“删除前必须全量复核”）。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"sync"
)

// quickHashBytes 是快筛读取的字节数。
const quickHashBytes = 64 << 10

// hashAllQuick 是 hashAll 的快筛版本。
func hashAllQuick(ctx context.Context, files []fileEntry, cache *hashCache, workers int,
	open func(string) (io.ReadCloser, error), progress func(Progress)) ([]hashResult, hashStats, error) {
	results := make([]hashResult, len(files))
	base := sizeTargets(files)
	if len(base) == 0 {
		return results, hashStats{}, nil
	}
	fps, errs := quickFingerprints(ctx, files, base, workers, open)
	if ctx.Err() != nil {
		return results, hashStats{}, ctx.Err()
	}
	for i, idx := range base {
		if errs[i] != nil {
			results[idx] = hashResult{err: errs[i], skipped: true}
		}
	}
	// 同尺寸 + 相同快筛指纹 才可能重复；两组都相同仍要做完整哈希确认。
	type key struct {
		size int64
		fp   string
	}
	buckets := make(map[key][]int, len(base))
	for i, idx := range base {
		if errs[i] != nil {
			continue
		}
		k := key{files[idx].size, fps[i]}
		buckets[k] = append(buckets[k], idx)
	}
	var candidates []int
	for _, idxs := range buckets {
		if len(idxs) >= 2 {
			candidates = append(candidates, idxs...)
		}
	}
	sort.Ints(candidates)
	return hashTargetsInto(ctx, files, candidates, results, cache, workers, open, progress)
}

// quickFingerprints 并行计算前 64KB 的 SHA-256。返回的切片与 targets 等长。
func quickFingerprints(ctx context.Context, files []fileEntry, targets []int, workers int,
	open func(string) (io.ReadCloser, error)) ([]string, []error) {
	fps := make([]string, len(targets))
	errs := make([]error, len(targets))
	if len(targets) == 0 {
		return fps, errs
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(targets) {
		workers = len(targets)
	}
	var counter int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		buf := make([]byte, quickHashBytes)
		for {
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			i := int(counter)
			counter++
			mu.Unlock()
			if i >= len(targets) {
				return
			}
			fp, err := quickFingerprint(ctx, files[targets[i]].path, buf, open)
			fps[i] = fp
			errs[i] = err
		}
	}
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go worker()
	}
	wg.Wait()
	return fps, errs
}

func quickFingerprint(ctx context.Context, path string, buf []byte, open func(string) (io.ReadCloser, error)) (string, error) {
	rc, err := open(path)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	n, err := io.ReadFull(rc, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	sum := sha256.Sum256(buf[:n])
	return "q:" + hex.EncodeToString(sum[:]), nil
}
