package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// parityCorpusRoot 是扫描对拍夹具的绝对根；用 TempDir 之外的固定子目录即可，
// 只要求“绝对”且带盘符/前导分隔符，避免把 Windows 路径写死。
var parityCorpusRoot = filepath.Join(os.TempDir(), "corpus")

func sampleResult() map[string]any {
	corpus := func(name string) string { return filepath.Join(parityCorpusRoot, name) }
	return map[string]any{
		"schemaVersion": 1,
		"requestId":     "r1",
		"request": map[string]any{
			"exact":           true,
			"roots":           []string{parityCorpusRoot},
			"excludes":        []string{},
			"keepPolicy":      "pathShortest",
			"imageKeepPolicy": "largest",
		},
		"stats": map[string]any{
			"filesEnumerated":  3,
			"bytesEnumerated":  30,
			"filesHashed":      3,
			"bytesHashed":      30,
			"cacheHits":        0,
			"cacheMisses":      3,
			"groups":           1,
			"exactGroups":      1,
			"imageGroups":      0,
			"audioGroups":      0,
			"duplicateFiles":   1,
			"reclaimableBytes": 10,
			"skipped":          0,
		},
		"groups": []any{
			map[string]any{
				"id":          "exact:aaa",
				"kind":        "exact",
				"key":         "aaa",
				"keep":        corpus("a.txt"),
				"reclaimable": 10,
				"members": []any{
					map[string]any{
						"path":      corpus("a.txt"),
						"size":      10,
						"modTime":   "2024-01-01T00:00:00Z",
						"hash":      "aaa",
						"keep":      true,
						"deletable": false,
					},
					map[string]any{
						"path":      corpus("b.txt"),
						"size":      10,
						"modTime":   "2024-01-01T00:01:00Z",
						"hash":      "aaa",
						"keep":      false,
						"deletable": true,
					},
				},
			},
		},
		"skipped": []any{},
	}
}

func writeJSON(t *testing.T, dir, name string, v any) string {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runScanCompare(t *testing.T, base, cand map[string]any, ignore map[string]bool) result {
	t.Helper()
	dir := t.TempDir()
	bp := writeJSON(t, dir, "base.json", base)
	cp := writeJSON(t, dir, "cand.json", cand)
	br, err := loadScan(bp)
	if err != nil {
		t.Fatalf("load base: %v", err)
	}
	cr, err := loadScan(cp)
	if err != nil {
		t.Fatalf("load cand: %v", err)
	}
	nb := normalizeScan(br, parityCorpusRoot)
	nc := normalizeScan(cr, parityCorpusRoot)
	d := newDiffer(0)
	compareScan(d, nb, nc, ignore)
	return d.finish("scan", len(nb.Groups), len(nc.Groups))
}

func cloneMap(v map[string]any) map[string]any {
	raw, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestScanIdenticalIsMatch(t *testing.T) {
	res := runScanCompare(t, sampleResult(), sampleResult(), nil)
	if res.Result != "MATCH" {
		t.Fatalf("应 MATCH，实际 diffs=%v", res.Diffs)
	}
}

// 输入顺序与时间格式不同、路径绝对/相对不同，归一化后必须 MATCH。
func TestScanNormalization(t *testing.T) {
	base := sampleResult()
	cand := cloneMap(base)
	cand["groups"] = []any{
		map[string]any{
			"id": "exact:aaa", "kind": "exact", "key": "aaa",
			"keep":        "a.txt",
			"reclaimable": 10,
			"members": []any{
				map[string]any{"path": "b.txt", "size": 10, "modTime": "2024-01-01T08:01:00+08:00", "hash": "aaa", "keep": false, "deletable": true},
				map[string]any{"path": "a.txt", "size": 10, "modTime": "2024-01-01T00:00:00.000000000Z", "hash": "aaa", "keep": true, "deletable": false},
			},
		},
	}
	res := runScanCompare(t, base, cand, nil)
	if res.Result != "MATCH" {
		t.Fatalf("归一化后应 MATCH，实际 %v", res.Diffs)
	}
}

func TestScanMemberFieldDiff(t *testing.T) {
	cand := cloneMap(sampleResult())
	groups := cand["groups"].([]any)
	members := groups[0].(map[string]any)["members"].([]any)
	members[1].(map[string]any)["size"] = float64(11)
	res := runScanCompare(t, sampleResult(), cand, nil)
	if res.Result != "DIFF" {
		t.Fatal("应 DIFF")
	}
	found := false
	for _, d := range res.Diffs {
		if d.Field == "group[exact:aaa].member[b.txt].size" && d.Base == "10" && d.Cand == "11" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少 size 差异: %v", res.Diffs)
	}
}

func TestScanMissingGroupAndMember(t *testing.T) {
	cand := cloneMap(sampleResult())
	cand["groups"] = []any{}
	res := runScanCompare(t, sampleResult(), cand, nil)
	if res.Result != "DIFF" {
		t.Fatal("应 DIFF")
	}
	if len(res.Diffs) == 0 || res.Diffs[0].Cand != "missing_in_cand" {
		t.Fatalf("缺少 missing_in_cand 差异: %v", res.Diffs)
	}

	cand2 := cloneMap(sampleResult())
	g := cand2["groups"].([]any)[0].(map[string]any)
	g["members"] = []any{g["members"].([]any)[0]}
	res2 := runScanCompare(t, sampleResult(), cand2, nil)
	found := false
	for _, d := range res2.Diffs {
		if d.Field == "group[exact:aaa].member[b.txt]" && d.Cand == "missing_in_cand" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少成员缺失差异: %v", res2.Diffs)
	}
}

func TestScanKnownDifferencesIgnorable(t *testing.T) {
	cand := cloneMap(sampleResult())
	stats := cand["stats"].(map[string]any)
	stats["bytesEnumerated"] = float64(29) // C# 把读不到大小的 -1 计入
	stats["skipped"] = float64(2)
	cand["skipped"] = []any{
		map[string]any{"path": "c.txt", "code": "permission_denied"},
		map[string]any{"path": "d.txt", "code": "read_failed"},
	}
	res := runScanCompare(t, sampleResult(), cand, nil)
	if res.Result != "DIFF" {
		t.Fatal("未忽略时应 DIFF")
	}
	res = runScanCompare(t, sampleResult(), cand, map[string]bool{"totalBytes": true, "skipped": true})
	if res.Result != "MATCH" {
		t.Fatalf("忽略已知差异后应 MATCH: %v", res.Diffs)
	}
}

// Go 多出的 skipped 项不算差异（只做包含性检查）。
func TestSkippedContainmentOnly(t *testing.T) {
	cand := cloneMap(sampleResult())
	cand["skipped"] = []any{map[string]any{"path": "x.txt", "code": "vanished"}}
	res := runScanCompare(t, sampleResult(), cand, nil)
	if res.Result != "MATCH" {
		t.Fatalf("cand 多出 skipped 不应算差异: %v", res.Diffs)
	}
}

// 图片组 key 无法逐字相等时，可用 imageKey 按成员集合匹配。
func TestImageGroupKeyMapping(t *testing.T) {
	mk := func(key string) map[string]any {
		r := cloneMap(sampleResult())
		stats := r["stats"].(map[string]any)
		stats["groups"], stats["exactGroups"], stats["imageGroups"] = float64(1), float64(0), float64(1)
		r["groups"] = []any{
			map[string]any{
				"id": "image:" + key, "kind": "image", "key": key,
				"keep": "a.txt", "reclaimable": 10,
				"members": []any{
					map[string]any{"path": "a.txt", "size": 10, "modTime": "2024-01-01T00:00:00Z", "keep": true, "deletable": false},
					map[string]any{"path": "b.txt", "size": 10, "modTime": "2024-01-01T00:01:00Z", "keep": false, "deletable": true},
				},
			},
		}
		return r
	}
	base, cand := mk("csharp-internal-id"), mk("9f1e2d3c4b5a6978")
	if res := runScanCompare(t, base, cand, nil); res.Result != "DIFF" {
		t.Fatal("未忽略 key 时应 DIFF")
	}
	if res := runScanCompare(t, base, cand, map[string]bool{"imageKey": true}); res.Result != "MATCH" {
		t.Fatalf("忽略 key 后应按成员集合 MATCH: %v", res.Diffs)
	}
}

func TestPlanCompare(t *testing.T) {
	mk := func(actions []any) map[string]any {
		return map[string]any{
			"schemaVersion": 1,
			"requestId":     "p1",
			"createdAt":     "2024-01-01T00:00:00Z",
			"source":        "scan",
			"summary":       map[string]any{"actions": len(actions)},
			"actions":       actions,
		}
	}
	act := func(seq int, kind, src, dst string) map[string]any {
		return map[string]any{
			"seq": seq, "kind": kind, "src": src, "dst": dst,
			"rollback": "none", "status": "planned", "irreversible": true,
		}
	}
	base := mk([]any{
		act(1, "delete_trash", `F:\c\b.txt`, ""),
		act(2, "delete_trash", `F:\c\d.txt`, ""),
	})
	candSame := mk([]any{
		act(1, "delete_trash", "b.txt", ""),
		act(2, "delete_trash", "d.txt", ""),
	})
	candSwapped := mk([]any{
		act(1, "delete_trash", "d.txt", ""),
		act(2, "delete_trash", "b.txt", ""),
	})

	diff := func(b, c map[string]any) result {
		dir := t.TempDir()
		bp := writeJSON(t, dir, "b.json", b)
		cp := writeJSON(t, dir, "c.json", c)
		bp2, err := loadPlan(bp)
		if err != nil {
			t.Fatal(err)
		}
		cp2, err := loadPlan(cp)
		if err != nil {
			t.Fatal(err)
		}
		d := newDiffer(0)
		nb, nc := normalizePlan(bp2, `F:\c`), normalizePlan(cp2, `F:\c`)
		comparePlan(d, nb, nc, nil)
		return d.finish("plan", len(nb.Actions), len(nc.Actions))
	}

	if res := diff(base, candSame); res.Result != "MATCH" {
		t.Fatalf("同集合应 MATCH: %v", res.Diffs)
	}
	res := diff(base, candSwapped)
	if res.Result != "DIFF" {
		t.Fatal("顺序不同应 DIFF")
	}
	found := false
	for _, d := range res.Diffs {
		if d.Field == "actions[1].order" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少顺序差异: %v", res.Diffs)
	}
}
