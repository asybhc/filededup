package main

// 基准归档（golden）：把 C# 参考实现的导出结果固化成 JSON，CI 不再需要 C# 可执行文件。
//
// 归档时做两件事：把绝对路径相对化、统一 '/'，并清空 root/target，
// 这样同一份基准可以在任意机器、任意工作目录下重放。
// 已知有意偏离单独登记（orgGoldenDeviation），只允许登记过的字段差异；
// 其它任何差异仍判 DIFF，避免“为了让数字好看”而放宽。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// orgGoldenKnownDeviations 是已登记的有意偏离：场景 → 允许差异的字段 → 原因。
// 新增条目必须在验收文档中写明原因；只允许 counters 字段。
var orgGoldenKnownDeviations = map[string]map[string]string{
	"hidden": {
		"counters.hiddenSkipped": "Go 单次计数，C# 每个步骤重复累加（M4.1 有意偏离）",
	},
}

func orgGoldenPath(dir, scene, tag string) string {
	return filepath.Join(dir, scene, tag+".json")
}

// saveOrgGolden 把 C# 导出归一化后写入 dir/<scene>/<tag>.json。
func saveOrgGolden(dir, scene, tag string, d *orgDump) error {
	norm := normalizeOrgDumpForGolden(*d)
	raw, err := json.MarshalIndent(norm, "", "  ")
	if err != nil {
		return err
	}
	path := orgGoldenPath(dir, scene, tag)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func loadOrgGolden(dir, scene, tag string) (*orgDump, error) {
	raw, err := os.ReadFile(orgGoldenPath(dir, scene, tag))
	if err != nil {
		return nil, err
	}
	var d orgDump
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// normalizeOrgDumpForGolden 把一份 C# 导出转为与机器无关的形式。
func normalizeOrgDumpForGolden(d orgDump) orgDump {
	out := d
	out.Root = ""
	out.Target = ""
	out.Dirs = make([]string, 0, len(d.Dirs))
	for _, p := range d.Dirs {
		out.Dirs = append(out.Dirs, orgRelPath(p, d.Root))
	}
	sort.Strings(out.Dirs)
	out.Files = make([]orgDumpFile, 0, len(d.Files))
	for _, f := range d.Files {
		f.Path = orgRelPath(f.Path, d.Root)
		out.Files = append(out.Files, f)
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	if d.PreviewPlan != nil {
		out.PreviewPlan = make(map[string]string, len(d.PreviewPlan))
		for k, v := range d.PreviewPlan {
			out.PreviewPlan[orgRelPath(k, d.Root)] = orgRelPath(v, d.Root)
		}
	}
	return out
}

// orgGoldenDeviation 判断一组差异是否全部落在已登记的有意偏离内。
func orgGoldenDeviation(scene string, diffs []diffItem) (string, bool) {
	allowed := orgGoldenKnownDeviations[scene]
	if len(allowed) == 0 || len(diffs) == 0 {
		return "", false
	}
	reasons := map[string]bool{}
	for _, d := range diffs {
		reason, ok := allowed[d.Field]
		if !ok {
			return "", false
		}
		reasons[reason] = true
	}
	out := make([]string, 0, len(reasons))
	for r := range reasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return strings.Join(out, "；"), true
}
