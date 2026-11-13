package scan

import (
	"sort"
	"strings"
	"time"

	"filededup/internal/engine/model"
)

// buildGroups 把哈希结果聚合为精确重复组，并选出保留项。
func buildGroups(req model.Request, files []fileEntry, results []hashResult) []model.Group {
	buckets := make(map[int64][]int, len(files))
	for i, f := range files {
		if f.size < 0 || results[i].err != nil || results[i].hash == "" {
			continue
		}
		buckets[f.size] = append(buckets[f.size], i)
	}
	sizes := make([]int64, 0, len(buckets))
	for size, idxs := range buckets {
		if len(idxs) >= 2 {
			sizes = append(sizes, size)
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] < sizes[j] })

	var groups []model.Group
	for _, size := range sizes {
		idxs := buckets[size]
		byHash := make(map[string][]int, 4)
		var order []string
		for _, i := range idxs {
			h := results[i].hash
			if _, ok := byHash[h]; !ok {
				order = append(order, h)
			}
			byHash[h] = append(byHash[h], i)
		}
		for _, h := range order {
			members := byHash[h]
			if len(members) < 2 {
				continue
			}
			groups = append(groups, makeGroup(req, size, h, members, files, results))
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Kind != groups[j].Kind {
			return groups[i].Kind < groups[j].Kind
		}
		return groups[i].Key < groups[j].Key
	})
	return groups
}

func makeGroup(req model.Request, size int64, hash string, idxs []int, files []fileEntry, results []hashResult) model.Group {
	keep := keepIndex(req.KeepPolicy, idxs, files)
	keepPath := files[keep].path
	policy := req.KeepPolicy
	if policy == "" {
		policy = model.KeepPathShortest
	}

	members := make([]model.Member, 0, len(idxs))
	for _, i := range idxs {
		f := files[i]
		isKeep := i == keep
		reason := "duplicate"
		if isKeep {
			reason = "keep:" + string(policy)
		}
		mod := f.modTime.UTC().Format(time.RFC3339Nano)
		members = append(members, model.Member{
			Path:      f.path,
			Size:      f.size,
			ModTime:   mod,
			Hash:      results[i].hash,
			Keep:      isKeep,
			Deletable: !isKeep,
			Reason:    reason,
			Snapshot:  &model.Precheck{Size: f.size, ModTime: mod, Hash: results[i].hash},
		})
	}
	sort.Slice(members, func(i, j int) bool {
		a, b := strings.ToLower(members[i].Path), strings.ToLower(members[j].Path)
		if a != b {
			return a < b
		}
		return members[i].Path < members[j].Path
	})

	return model.Group{
		ID:          "exact:" + hash,
		Kind:        model.GroupExact,
		Key:         hash,
		Members:     members,
		Keep:        keepPath,
		Reclaimable: size * int64(len(members)-1),
	}
}
