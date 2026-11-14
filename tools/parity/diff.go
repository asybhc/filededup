package main

import (
	"fmt"
	"sort"
	"strings"

	"filededup/internal/engine/model"
)

type diffItem struct {
	Field string `json:"field"`
	Base  string `json:"base"`
	Cand  string `json:"cand"`
}

func (d diffItem) String() string {
	return fmt.Sprintf("DIFF %s: base=%s cand=%s", d.Field, d.Base, d.Cand)
}

type summary struct {
	Kind      string `json:"kind"`
	BaseCount int    `json:"baseCount"`
	CandCount int    `json:"candCount"`
	Matched   int    `json:"matched"`
	Compared  int    `json:"compared"`
	DiffCount int    `json:"diffCount"`
	ExtraNote string `json:"extraNote,omitempty"`
}

func (s summary) String() string {
	unit := "组"
	if s.Kind == "plan" {
		unit = "动作"
	}
	return fmt.Sprintf("%s base=%d cand=%d matched=%d compared=%d diffs=%d", unit, s.BaseCount, s.CandCount, s.Matched, s.Compared, s.DiffCount)
}

type differ struct {
	items    []diffItem
	total    int
	max      int
	matched  int
	compared int
}

func newDiffer(max int) *differ { return &differ{max: max} }

func (d *differ) add(field, base, cand string) {
	d.total++
	if d.max > 0 && len(d.items) >= d.max {
		return
	}
	d.items = append(d.items, diffItem{Field: field, Base: base, Cand: cand})
}

func (d *differ) addMissing(field string) { d.add(field, "present", "missing_in_cand") }
func (d *differ) addExtra(field string)   { d.add(field, "absent", "extra_in_cand") }

func (d *differ) finish(kind string, baseCount, candCount int) result {
	res := "MATCH"
	if d.total > 0 {
		res = "DIFF"
	}
	return result{
		Result:  res,
		Diffs:   d.items,
		Dropped: d.total - len(d.items),
		Summary: summary{
			Kind:      kind,
			BaseCount: baseCount,
			CandCount: candCount,
			Matched:   d.matched,
			Compared:  d.compared,
			DiffCount: d.total,
			ExtraNote: "路径已相对化并 NFC 归一；时间按 UnixNano 比较",
		},
	}
}

func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "…"
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func sameInt(a, b int64) bool { return a == b }

// compareScan 是比较入口：Stats → 组集合 → 组内成员 → Skipped 包含性。
func compareScan(d *differ, b, c normScan, ig map[string]bool) {
	if b.Version != c.Version {
		d.add("schemaVersion", fmt.Sprint(b.Version), fmt.Sprint(c.Version))
	}
	compareStats(d, b.Stats, c.Stats, ig)

	used := make([]bool, len(c.Groups))
	for _, bg := range b.Groups {
		idx := -1
		for j, cg := range c.Groups {
			if !used[j] && cg.Kind == bg.Kind && cg.Key == bg.Key {
				idx = j
				break
			}
		}
		// 图片/音频组的 Key 派生自实现细节（C# 内部标识 vs Go dHash），按成员集合匹配。
		if idx < 0 && ig["imageKey"] && (bg.Kind == model.GroupImage || bg.Kind == model.GroupAudio) {
			sig := groupSig(bg)
			for j, cg := range c.Groups {
				if !used[j] && cg.Kind == bg.Kind && groupSig(cg) == sig {
					idx = j
					break
				}
			}
		}
		if idx < 0 {
			d.addMissing(fmt.Sprintf("group[%s:%s]", bg.Kind, short(bg.Key)))
			continue
		}
		used[idx] = true
		d.matched++
		compareGroup(d, bg, c.Groups[idx], ig)
	}
	for j, cg := range c.Groups {
		if !used[j] {
			d.addExtra(fmt.Sprintf("group[%s:%s]", cg.Kind, short(cg.Key)))
		}
	}

	compareSkipped(d, b.Skipped, c.Skipped, ig)
}

func groupSig(g normGroup) string {
	paths := make([]string, 0, len(g.Members))
	for _, m := range g.Members {
		paths = append(paths, m.Path)
	}
	sort.Strings(paths)
	return string(g.Kind) + "|" + strings.Join(paths, "\x00")
}

func compareStats(d *differ, b, c model.Stats, ig map[string]bool) {
	type item struct {
		name string
		b, c int64
	}
	items := []item{
		{"filesEnumerated", b.FilesEnumerated, c.FilesEnumerated},
		{"bytesEnumerated", b.BytesEnumerated, c.BytesEnumerated},
		{"filesHashed", b.FilesHashed, c.FilesHashed},
		{"bytesHashed", b.BytesHashed, c.BytesHashed},
		{"cacheHits", b.CacheHits, c.CacheHits},
		{"cacheMisses", b.CacheMisses, c.CacheMisses},
		{"groups", b.Groups, c.Groups},
		{"exactGroups", b.ExactGroups, c.ExactGroups},
		{"imageGroups", b.ImageGroups, c.ImageGroups},
		{"audioGroups", b.AudioGroups, c.AudioGroups},
		{"duplicateFiles", b.DuplicateFiles, c.DuplicateFiles},
		{"reclaimableBytes", b.ReclaimableBytes, c.ReclaimableBytes},
		{"skipped", b.Skipped, c.Skipped},
	}
	for _, it := range items {
		d.compared++
		switch it.name {
		case "bytesEnumerated":
			if ig["totalBytes"] {
				continue
			}
		case "skipped":
			if ig["skipped"] {
				continue
			}
		case "cacheHits", "cacheMisses":
			if ig["cache"] {
				continue
			}
		}
		if !sameInt(it.b, it.c) {
			d.add("stats."+it.name, fmt.Sprint(it.b), fmt.Sprint(it.c))
		}
	}
}

func compareGroup(d *differ, b, c normGroup, ig map[string]bool) {
	prefix := fmt.Sprintf("group[%s:%s]", b.Kind, short(b.Key))
	d.compared++
	if b.Keep != c.Keep {
		d.add(prefix+".keep", b.Keep, c.Keep)
	}
	d.compared++
	if b.Reclaimable != c.Reclaimable {
		d.add(prefix+".reclaimable", fmt.Sprint(b.Reclaimable), fmt.Sprint(c.Reclaimable))
	}

	cm := make(map[string]normMember, len(c.Members))
	for _, m := range c.Members {
		cm[m.Path] = m
	}
	for _, bm := range b.Members {
		cmMember, ok := cm[bm.Path]
		if !ok {
			d.addMissing(prefix + ".member[" + bm.Path + "]")
			continue
		}
		delete(cm, bm.Path)
		d.matched++
		mp := prefix + ".member[" + bm.Path + "]."
		check := func(name, bv, cv string, skip bool) {
			d.compared++
			if !skip && bv != cv {
				d.add(mp+name, bv, cv)
			}
		}
		check("size", fmt.Sprint(bm.Size), fmt.Sprint(cmMember.Size), false)
		check("hash", bm.Hash, cmMember.Hash, false)
		check("fingerprint", bm.Fingerprint, cmMember.Fingerprint, ig["fingerprint"])
		check("durationMs", fmt.Sprint(bm.DurationMS), fmt.Sprint(cmMember.DurationMS), false)
		check("keep", boolStr(bm.Keep), boolStr(cmMember.Keep), false)
		check("deletable", boolStr(bm.Deletable), boolStr(cmMember.Deletable), false)
		// 时间：可解析时按 UnixNano 比较，否则退回原始字符串；跨文件系统精度差异可用 -ignore modTime 关闭。
		if bm.ModTimeRaw != "" || cmMember.ModTimeRaw != "" {
			check("modTime", bm.ModTimeRaw, cmMember.ModTimeRaw, ig["modTime"])
		} else {
			check("modTime", fmt.Sprint(bm.ModTimeNS), fmt.Sprint(cmMember.ModTimeNS), ig["modTime"])
		}
	}
	for p := range cm {
		d.addExtra(prefix + ".member[" + p + "]")
	}
}

func compareSkipped(d *differ, b, c []model.Skip, ig map[string]bool) {
	if ig["skipped"] {
		return
	}
	// 只做包含性检查：Go 版会显式记录 C# 静默丢弃的项，不要求两侧相等。
	have := make(map[string]bool, len(c))
	for _, s := range c {
		have[s.Path] = true
	}
	for _, s := range b {
		d.compared++
		if !have[s.Path] {
			d.addMissing("skipped[" + s.Path + "]")
		}
	}
}

// comparePlan 比较两个 Plan 的头部、汇总与动作（逐字段 + 顺序）。
// createdAt 属于生成时刻，不参与比较；requestId 可用 -ignore requestId 关闭。
func comparePlan(d *differ, b, c normPlan, ig map[string]bool) {
	if b.Version != c.Version {
		d.add("schemaVersion", fmt.Sprint(b.Version), fmt.Sprint(c.Version))
	}
	if !ig["requestId"] {
		d.compared++
		if b.RequestID != c.RequestID {
			d.add("requestId", b.RequestID, c.RequestID)
		}
	}
	if !ig["source"] {
		d.compared++
		if b.Source != c.Source {
			d.add("source", string(b.Source), string(c.Source))
		}
	}
	comparePlanSummary(d, b.Summary, c.Summary, ig)
	key := func(a model.Action) string {
		return fmt.Sprintf("%s|%s|%s|%s|%s", a.Kind, a.Src, a.Dst, a.Reason, boolStr(a.Irreversible))
	}
	pool := make(map[string][]int, len(c.Actions))
	for i, a := range c.Actions {
		pool[key(a)] = append(pool[key(a)], i)
	}
	used := make([]bool, len(c.Actions))
	for i, a := range b.Actions {
		k := key(a)
		idxs := pool[k]
		pick := -1
		for _, j := range idxs {
			if !used[j] {
				pick = j
				break
			}
		}
		if pick < 0 {
			d.addMissing(fmt.Sprintf("actions[%d](%s)", i+1, a.Kind))
			continue
		}
		used[pick] = true
		d.matched++
		d.compared++
		if pick != i && !ig["planOrder"] {
			d.add(fmt.Sprintf("actions[%d].order", i+1), string(b.Actions[i].Kind), string(c.Actions[pick].Kind))
		}
		comparePlanAction(d, i, b.Actions[i], c.Actions[pick], ig)
	}
	for j, a := range c.Actions {
		if !used[j] {
			d.addExtra(fmt.Sprintf("actions[%d](%s)", j+1, a.Kind))
		}
	}
}

// comparePlanSummary 比较计划汇总的全部计数。
func comparePlanSummary(d *differ, b, c model.PlanSummary, ig map[string]bool) {
	if ig["summary"] {
		return
	}
	items := []struct {
		name string
		b, c int64
	}{
		{"actions", int64(b.Actions), int64(c.Actions)},
		{"deletes", int64(b.Deletes), int64(c.Deletes)},
		{"trash", int64(b.Trash), int64(c.Trash)},
		{"permanent", int64(b.Permanent), int64(c.Permanent)},
		{"moves", int64(b.Moves), int64(c.Moves)},
		{"renames", int64(b.Renames), int64(c.Renames)},
		{"hardlinks", int64(b.Hardlinks), int64(c.Hardlinks)},
		{"clearReadonly", int64(b.ClearReadonly), int64(c.ClearReadonly)},
		{"removeDirs", int64(b.RemoveDirs), int64(c.RemoveDirs)},
		{"irreversible", int64(b.Irreversible), int64(c.Irreversible)},
		{"reclaimableBytes", b.ReclaimableBytes, c.ReclaimableBytes},
	}
	for _, it := range items {
		d.compared++
		if it.b != it.c {
			d.add("summary."+it.name, fmt.Sprint(it.b), fmt.Sprint(it.c))
		}
	}
}

// comparePlanAction 比较单条动作的序号、预检、回滚与状态。
func comparePlanAction(d *differ, i int, b, c model.Action, ig map[string]bool) {
	prefix := fmt.Sprintf("actions[%d]", i+1)
	d.compared++
	if b.Seq != c.Seq {
		d.add(prefix+".seq", fmt.Sprint(b.Seq), fmt.Sprint(c.Seq))
	}
	d.compared++
	if string(b.Rollback) != string(c.Rollback) {
		d.add(prefix+".rollback", string(b.Rollback), string(c.Rollback))
	}
	d.compared++
	if string(b.Status) != string(c.Status) {
		d.add(prefix+".status", string(b.Status), string(c.Status))
	}
	if ig["precheck"] {
		return
	}
	if b.Precheck == nil || c.Precheck == nil {
		d.compared++
		if (b.Precheck == nil) != (c.Precheck == nil) {
			d.add(prefix+".precheck", boolStr(b.Precheck != nil), boolStr(c.Precheck != nil))
		}
		return
	}
	d.compared++
	if b.Precheck.Size != c.Precheck.Size {
		d.add(prefix+".precheck.size", fmt.Sprint(b.Precheck.Size), fmt.Sprint(c.Precheck.Size))
	}
	d.compared++
	if b.Precheck.Hash != c.Precheck.Hash && !ig["precheckHash"] {
		d.add(prefix+".precheck.hash", b.Precheck.Hash, c.Precheck.Hash)
	}
	d.compared++
	if !ig["precheckModTime"] {
		bns, braw := normTime(b.Precheck.ModTime)
		cns, craw := normTime(c.Precheck.ModTime)
		if braw != "" || craw != "" {
			if b.Precheck.ModTime != c.Precheck.ModTime {
				d.add(prefix+".precheck.modTime", b.Precheck.ModTime, c.Precheck.ModTime)
			}
		} else if bns != cns {
			d.add(prefix+".precheck.modTime", b.Precheck.ModTime, c.Precheck.ModTime)
		}
	}
}
