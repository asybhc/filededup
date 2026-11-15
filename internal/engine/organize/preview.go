package organize

// 预览计划：把“哪些重复文件删除、保留哪一份”固化成可保存、可编辑、可复用的映射。
//
// 原版对应“预览/确认计划”（A_1.A_8 != null）：用户在界面上确认后，去重步骤按确认结果
// 执行，而不是按内置保留规则重新裁决。Go 版没有界面，因此由本文件生成初始计划，
// 允许人工编辑 JSON 后再喂回去重步骤。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PreviewPlanSchemaVersion 是预览计划文件的格式版本；字段变化必须递增。
const PreviewPlanSchemaVersion = 1

// PreviewEntry 是一条“删除候选 → 保留项”。两者都是相对根目录、'/' 分隔的路径。
type PreviewEntry struct {
	From string `json:"from"`
	Keep string `json:"keep"`
}

// PreviewPlan 是预览计划的持久化形式。
//
// Entries 保留原始大小写，便于人工核对与编辑；规划器内部按 Fold 折叠匹配，
// 因此大小写差异不影响执行。
type PreviewPlan struct {
	SchemaVersion int            `json:"schemaVersion"`
	Root          string         `json:"root,omitempty"`
	Entries       []PreviewEntry `json:"entries"`
}

// Map 把计划转成规划器使用的映射（折叠路径 → 折叠保留路径）。
// 与旧实现一致：同一原路径命中多组时，后写入的组覆盖先写入的组。
func (p PreviewPlan) Map() map[string]string {
	m := make(map[string]string, len(p.Entries))
	for _, e := range p.Entries {
		if e.From == "" || e.Keep == "" {
			continue
		}
		m[Fold(e.From)] = Fold(e.Keep)
	}
	return m
}

// BuildPreviewPlan 复刻“预览/确认计划”的分组：内容 → 图片 → 音频。
// 组顺序与扫描一致；同一文件命中多组时后组覆盖前组（与 C# 侧确认界面一致）。
func BuildPreviewPlan(cfg Config, snap Snapshot, providers Providers) PreviewPlan {
	plan := PreviewPlan{SchemaVersion: PreviewPlanSchemaVersion, Root: snap.Root}
	byFrom := map[string]PreviewEntry{}
	items := previewEligible(cfg, snap.Target, snap.Files)

	addGroup := func(group []FileEntry, rule KeepRule) {
		if len(group) < 2 {
			return
		}
		idx := KeepWinner(group, rule)
		if idx < 0 {
			return
		}
		keep := group[idx].Path
		for _, f := range group {
			byFrom[Fold(f.Path)] = PreviewEntry{From: f.Path, Keep: keep}
		}
	}

	if cfg.Steps.ContentDedup {
		// 按“大小 + SHA-256”分组：同大小但内容不同的文件不能配对；空 hash 不参与。
		byContent := map[string][]FileEntry{}
		var keys []string
		for _, f := range items {
			h := f.SHA256
			if h == "" && providers.ContentHash != nil {
				h, _ = providers.ContentHash(f)
			}
			if h == "" {
				continue
			}
			key := fmt.Sprintf("%d|%s", f.Size, h)
			if _, ok := byContent[key]; !ok {
				keys = append(keys, key)
			}
			byContent[key] = append(byContent[key], f)
		}
		for _, key := range keys {
			addGroup(byContent[key], cfg.KeepRule)
		}
	}

	if cfg.Steps.ImageDedup {
		var imgs []FileEntry
		for _, f := range items {
			if IsImage(f.Path) {
				imgs = append(imgs, f)
			}
		}
		keepRule := cfg.ImageKeepRule
		if keepRule == "" {
			keepRule = KeepLargest
		}
		for _, g := range groupImages(imgs, cfg.ImageThreshold, providers) {
			addGroup(g, keepRule)
		}
	}

	if cfg.Steps.AudioDedup {
		tol := int64(cfg.AudioToleranceSec) * 1000
		byExt := map[string][]FileEntry{}
		var exts []string
		for _, f := range items {
			if !IsAudio(f.Path) {
				continue
			}
			ext := strings.ToLower(WinExt(f.Path))
			if _, ok := byExt[ext]; !ok {
				exts = append(exts, ext)
			}
			byExt[ext] = append(byExt[ext], f)
		}
		for _, ext := range exts {
			g := byExt[ext]
			if len(g) < 2 {
				continue
			}
			for i := range g {
				if !g[i].HasAudio && providers.AudioMsFn != nil {
					if ms, ok := providers.AudioMsFn(g[i]); ok {
						g[i].AudioMs, g[i].HasAudio = ms, true
					}
				}
			}
			sort.SliceStable(g, func(i, j int) bool { return g[i].AudioMs < g[j].AudioMs })
			var groups [][]FileEntry
			var anchor int64
			for i, f := range g {
				if i == 0 || f.AudioMs-anchor > tol {
					groups = append(groups, []FileEntry{f})
					anchor = f.AudioMs
					continue
				}
				groups[len(groups)-1] = append(groups[len(groups)-1], f)
			}
			for _, grp := range groups {
				addGroup(grp, cfg.KeepRule)
			}
		}
	}

	plan.Entries = make([]PreviewEntry, 0, len(byFrom))
	for _, e := range byFrom {
		plan.Entries = append(plan.Entries, e)
	}
	sort.Slice(plan.Entries, func(i, j int) bool {
		a, b := Fold(plan.Entries[i].From), Fold(plan.Entries[j].From)
		if a != b {
			return a < b
		}
		return plan.Entries[i].From < plan.Entries[j].From
	})
	return plan
}

// previewEligible 复刻 vfs.list() 的扫描过滤（不做虚拟文件系统的逐步演进），
// 用于在真正规划前生成“候选分组”。
func previewEligible(cfg Config, target string, files []FileEntry) []FileEntry {
	tf := Fold(target)
	var out []FileEntry
	for _, e := range files {
		if tf != "" {
			fp := Fold(e.Path)
			if fp == tf || strings.HasPrefix(fp, tf+"/") {
				continue
			}
		}
		if cfg.SkipHiddenSystem && (e.Hidden || e.System) {
			continue
		}
		if cfg.MinSize > 0 && e.Size < cfg.MinSize {
			continue
		}
		if previewExcluded(cfg.Excludes, e.Path) {
			continue
		}
		out = append(out, e)
	}
	SortFilesNTFS(out)
	return out
}

func previewExcluded(excludes []string, rel string) bool {
	dir := ParentRel(rel)
	for _, ex := range excludes {
		ex = strings.TrimRight(strings.TrimSpace(ex), `\/`)
		if ex == "" || strings.ContainsAny(ex, `\:`) {
			continue
		}
		for _, seg := range strings.Split(dir, "/") {
			if seg != "" && strings.EqualFold(seg, ex) {
				return true
			}
		}
	}
	return false
}

// SavePreviewPlan 把预览计划写入 path（2 空格缩进，UTF-8，父目录自动创建）。
func SavePreviewPlan(path string, p PreviewPlan) error {
	if p.SchemaVersion == 0 {
		p.SchemaVersion = PreviewPlanSchemaVersion
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

// LoadPreviewPlan 读取并校验预览计划。
// 外部文件属于系统边界，因此这里拒绝绝对路径、越界路径与空字段。
func LoadPreviewPlan(path string) (PreviewPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return PreviewPlan{}, err
	}
	var p PreviewPlan
	if err := json.Unmarshal(raw, &p); err != nil {
		return PreviewPlan{}, fmt.Errorf("%s: %w", path, err)
	}
	if p.SchemaVersion != PreviewPlanSchemaVersion {
		return PreviewPlan{}, fmt.Errorf("%s: 不支持的预览计划版本 %d（期望 %d）",
			path, p.SchemaVersion, PreviewPlanSchemaVersion)
	}
	for i, e := range p.Entries {
		if err := validatePreviewPath(e.From); err != nil {
			return PreviewPlan{}, fmt.Errorf("%s: 第 %d 条 from %w", path, i+1, err)
		}
		if err := validatePreviewPath(e.Keep); err != nil {
			return PreviewPlan{}, fmt.Errorf("%s: 第 %d 条 keep %w", path, i+1, err)
		}
	}
	if p.Entries == nil {
		p.Entries = []PreviewEntry{}
	}
	return p, nil
}

func validatePreviewPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("为空")
	}
	if filepath.IsAbs(p) || filepath.VolumeName(p) != "" || windowsDrivePath(p) ||
		strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return fmt.Errorf("必须是根目录内的相对路径：%q", p)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("逃逸根目录：%q", p)
	}
	return nil
}

// windowsDrivePath 识别 "C:..." / "C:\..." 形式。预览计划可能在另一个平台生成，
// 因此在 Unix 上也要拒绝 Windows 绝对路径，而不是等执行时才失败。
func windowsDrivePath(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
