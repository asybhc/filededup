package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filededup/internal/engine/model"
	"filededup/internal/engine/similar/audio"
	"filededup/internal/engine/similar/image"
)

// buildImageGroups 计算相似图片组。算法见 engine/similar/image。
// 解码失败登记 decode_failed；开启 skipUniform 时退化指纹登记 uniform_fingerprint。
func buildImageGroups(req model.Request, files []fileEntry, opts model.ImageOpts) ([]model.Group, []model.Skip) {
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f.path] = i
	}

	var hashed []image.Hashed
	fingerprints := make(map[string]uint64, len(files))
	var skipped []model.Skip
	for _, f := range files {
		if !image.Supported(f.path) {
			continue
		}
		fp, ok := image.Compute(f.path)
		if !ok {
			skipped = append(skipped, model.Skip{
				Path:    f.path,
				Code:    model.SkipDecodeFailed,
				Message: "图片解码失败",
			})
			continue
		}
		// 可选规则（默认关闭，保持与原版一致）：退化指纹不参与相似分组。
		if opts.SkipUniform && fp.Uniform {
			skipped = append(skipped, model.Skip{
				Path:    f.path,
				Code:    model.SkipUniformFingerprint,
				Message: "图片无行内明暗变化（纯色图/纯色条等退化指纹），按 skipUniform 规则不参与相似分组",
			})
			continue
		}
		hashed = append(hashed, image.Hashed{Path: f.path, Hash: fp.Hash})
		fingerprints[f.path] = fp.Hash
	}
	sort.SliceStable(hashed, func(i, j int) bool {
		li, lj := strings.ToLower(hashed[i].Path), strings.ToLower(hashed[j].Path)
		if li != lj {
			return li < lj
		}
		return hashed[i].Path < hashed[j].Path
	})

	policy := req.ImageKeepPolicy
	if policy == "" {
		policy = model.KeepLargest
	}
	var groups []model.Group
	for _, paths := range image.Group(hashed, opts.Threshold) {
		idxs := make([]int, 0, len(paths))
		var minHash uint64 = ^uint64(0)
		for _, p := range paths {
			i, ok := index[p]
			if !ok {
				continue
			}
			idxs = append(idxs, i)
			if h := fingerprints[p]; h < minHash {
				minHash = h
			}
		}
		if len(idxs) < 2 {
			continue
		}
		members := make([]model.Member, len(idxs))
		for k, i := range idxs {
			f := files[i]
			h := fingerprints[f.path]
			mod := f.modTime.UTC().Format(time.RFC3339Nano)
			members[k] = model.Member{
				Path:        f.path,
				Size:        f.size,
				ModTime:     mod,
				Fingerprint: image.Hex(h),
				Snapshot:    &model.Precheck{Size: f.size, ModTime: mod},
			}
		}
		groups = append(groups, assembleGroup(model.GroupImage, image.Hex(minHash), policy, idxs, files, members))
	}
	return groups, skipped
}

// buildAudioGroups 计算相似音频组。算法见 engine/similar/audio。
func buildAudioGroups(req model.Request, files []fileEntry, opts model.AudioOpts) ([]model.Group, []model.Skip) {
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f.path] = i
	}

	var items []audio.Item
	durations := make(map[string]int64, len(files))
	var skipped []model.Skip
	for _, f := range files {
		if !audio.Supported(f.path) {
			continue
		}
		d, ok := audio.Probe(f.path)
		if !ok {
			skipped = append(skipped, model.Skip{
				Path:    f.path,
				Code:    model.SkipDecodeFailed,
				Message: "音频时长解析失败",
			})
			continue
		}
		items = append(items, audio.Item{Path: f.path, Duration: d})
		durations[f.path] = audio.DurationMS(d)
	}

	policy := req.KeepPolicy
	if policy == "" {
		policy = model.KeepPathShortest
	}
	var groups []model.Group
	for _, paths := range audio.Group(items, opts.Tolerance) {
		idxs := make([]int, 0, len(paths))
		for _, p := range paths {
			if i, ok := index[p]; ok {
				idxs = append(idxs, i)
			}
		}
		if len(idxs) < 2 {
			continue
		}
		members := make([]model.Member, len(idxs))
		for k, i := range idxs {
			f := files[i]
			mod := f.modTime.UTC().Format(time.RFC3339Nano)
			members[k] = model.Member{
				Path:       f.path,
				Size:       f.size,
				ModTime:    mod,
				DurationMS: durations[f.path],
				Snapshot:   &model.Precheck{Size: f.size, ModTime: mod},
			}
		}
		anchor := paths[0]
		key := fmt.Sprintf("%s:%d", strings.TrimPrefix(strings.ToLower(filepath.Ext(anchor)), "."), durations[anchor])
		groups = append(groups, assembleGroup(model.GroupAudio, key, policy, idxs, files, members))
	}
	return groups, skipped
}

// hashMediaGroups 为相似组成员补算 SHA-256。
// C# 基线在相似查重时也会对最终组内成员计算哈希（成员 hash 字段、filesHashed/bytesHashed 均含这部分），
// 已由精确查重算过的文件不重复计数。
func hashMediaGroups(ctx context.Context, files []fileEntry, results []hashResult, groups []model.Group,
	cache *hashCache, open func(string) (io.ReadCloser, error), st *hashStats) {
	index := make(map[string]int, len(files))
	for i, f := range files {
		index[f.path] = i
	}
	buf := make([]byte, 1<<20)
	for gi := range groups {
		if groups[gi].Kind == model.GroupExact {
			continue
		}
		for mi := range groups[gi].Members {
			m := &groups[gi].Members[mi]
			i, ok := index[m.Path]
			if !ok {
				continue
			}
			h := results[i].hash
			if h == "" {
				key := cacheKey(files[i])
				if v, ok := cache.get(key); ok {
					h = v
					st.cacheHits++
				} else {
					v, n, err := hashFile(ctx, files[i].path, buf, open)
					if err != nil {
						if errors.Is(err, context.Canceled) {
							return
						}
						continue
					}
					cache.put(key, v)
					h = v
					st.bytesHashed += n
					st.cacheMisses++
				}
				results[i].hash = h
				st.filesHashed++
			}
			m.Hash = h
			if m.Snapshot != nil {
				m.Snapshot.Hash = h
			}
		}
	}
}

// assembleGroup 选出保留项并按 C# 口径（成员路径 OrdinalIgnoreCase 升序）排序成员。
// reclaimable = 成员大小之和 - 最大成员大小（C# q.WastedBytes）。
func assembleGroup(kind model.GroupKind, key string, policy model.KeepPolicy,
	idxs []int, files []fileEntry, members []model.Member) model.Group {
	keep := keepIndexPolicy(policy, idxs, files)
	keepPath := files[keep].path

	sort.Slice(members, func(i, j int) bool {
		a, b := strings.ToLower(members[i].Path), strings.ToLower(members[j].Path)
		if a != b {
			return a < b
		}
		return members[i].Path < members[j].Path
	})
	for k := range members {
		isKeep := members[k].Path == keepPath
		members[k].Keep = isKeep
		members[k].Deletable = !isKeep
		if isKeep {
			members[k].Reason = "keep:" + string(policy)
		} else {
			members[k].Reason = "duplicate"
		}
	}
	var sum, max int64
	for _, i := range idxs {
		sum += files[i].size
		if files[i].size > max {
			max = files[i].size
		}
	}
	reclaim := sum - max
	if reclaim < 0 {
		reclaim = 0
	}
	return model.Group{
		ID:          string(kind) + ":" + key,
		Kind:        kind,
		Key:         key,
		Members:     members,
		Keep:        keepPath,
		Reclaimable: reclaim,
	}
}
