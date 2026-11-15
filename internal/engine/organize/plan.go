package organize

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Plan 是纯函数入口：不读写文件系统，只产出逐步动作与计数。
// 虚拟文件系统会随动作推进（试运行时保持原状，与原版“预演不落盘”一致）。
func Plan(cfg Config, snap Snapshot, providers Providers) Result {
	v := newVFS(cfg, snap, providers)
	res := Result{Order: cfg.Order}
	// 原版扫描过滤只做一次（B/E.cs 的缓存列表），隐藏/系统、最小大小、排除目录
	// 命中的文件统一计入 G（hiddenSkipped），与后续步骤是否改名无关。
	res.Counters.HiddenSkipped = v.countScanFiltered()
	for idx, id := range orderedSteps(cfg.Order) {
		sp := StepPlan{ID: string(id), Title: stepTitles[id], Order: idx + 1}
		if !stepEnabled(cfg.Steps, id) {
			sp.Skipped = true
			sp.SkipReason = "开关未启用"
			res.Steps = append(res.Steps, sp)
			continue
		}
		switch id {
		case StepRenamePrefix:
			v.stepRename(&sp, CleanPrefix)
		case StepRenameSuffix:
			v.stepRename(&sp, CleanSuffix)
		case StepSpecialDelete:
			v.stepSpecialDelete(&sp)
		case StepReadonly:
			v.stepReadonly(&sp)
		case StepContentDedup:
			v.stepContentDedup(&sp)
		case StepImageDedup:
			v.stepImageDedup(&sp)
		case StepAudioDedup:
			v.stepAudioDedup(&sp)
		case StepOrganize:
			v.stepOrganize(&sp)
		case StepEmptyDirs:
			v.stepEmptyDirs(&sp)
		}
		res.Counters.Add(sp.Counters)
		res.Steps = append(res.Steps, sp)
	}
	return res
}

type vfs struct {
	cfg       Config
	providers Providers
	root      string
	target    string
	preview   map[string]string
	files     map[string]*FileEntry // 键为当前相对路径（保留原始大小写）
	dirs      map[string]bool
	foldFiles map[string]string // 折叠键 → 当前路径
	foldDirs  map[string]string
}

func newVFS(cfg Config, snap Snapshot, providers Providers) *vfs {
	v := &vfs{
		cfg: cfg, providers: providers, root: snap.Root, target: snap.Target, preview: snap.PreviewPlan,
		files: map[string]*FileEntry{}, dirs: map[string]bool{},
		foldFiles: map[string]string{}, foldDirs: map[string]string{},
	}
	for _, d := range snap.Dirs {
		v.addDir(d)
	}
	for _, f := range snap.Files {
		e := f
		if v.providers.ContentHash != nil && e.SHA256 == "" {
			if h, ok := v.providers.ContentHash(e); ok {
				e.SHA256 = h
			}
		}
		if v.providers.ImageHash != nil && !e.HasDHash {
			if h, ok := v.providers.ImageHash(e); ok {
				e.DHash, e.HasDHash = h, true
			}
		}
		if v.providers.AudioMsFn != nil && !e.HasAudio {
			if ms, ok := v.providers.AudioMsFn(e); ok {
				e.AudioMs, e.HasAudio = ms, true
			}
		}
		v.files[f.Path] = &e
		v.foldFiles[Fold(f.Path)] = f.Path
		// 确保父目录存在（快照可能省略空目录）
		v.ensureDir(ParentRel(f.Path))
	}
	return v
}

func (v *vfs) addDir(d string) {
	if d == "" || d == "." {
		return
	}
	v.dirs[d] = true
	if _, ok := v.foldDirs[Fold(d)]; !ok {
		v.foldDirs[Fold(d)] = d
	}
}

func (v *vfs) ensureDir(d string) {
	if d == "" || d == "." {
		return
	}
	if _, ok := v.foldDirs[Fold(d)]; ok {
		return
	}
	v.addDir(d)
	v.ensureDir(ParentRel(d))
}

func (v *vfs) fileExists(p string) (string, bool) {
	key, ok := v.foldFiles[Fold(p)]
	if !ok {
		return "", false
	}
	if _, alive := v.files[key]; !alive {
		return "", false
	}
	return key, true
}

func (v *vfs) dirExists(p string) bool {
	_, ok := v.foldDirs[Fold(p)]
	return ok
}

// list 返回参与本步的文件（已应用扫描过滤与固定排序）。
func (v *vfs) list() []FileEntry {
	target := Fold(v.target)
	var out []FileEntry
	for p, e := range v.files {
		if target != "" {
			fp := Fold(p)
			if fp == target || strings.HasPrefix(fp, target+"/") {
				continue
			}
		}
		if v.scanFilteredOut(p, e) {
			continue
		}
		out = append(out, *e)
	}
	SortFilesNTFS(out)
	return out
}

// scanFilteredOut 对齐原版扫描过滤：隐藏/系统、最小大小、排除目录。
// 注意 target 子树在调用方先行排除，不计入这里的过滤。
func (v *vfs) scanFilteredOut(p string, e *FileEntry) bool {
	if v.cfg.SkipHiddenSystem && (e.Hidden || e.System) {
		return true
	}
	if v.cfg.MinSize > 0 && e.Size < v.cfg.MinSize {
		return true
	}
	dir := ParentRel(p)
	for _, ex := range v.cfg.Excludes {
		ex = strings.TrimRight(strings.TrimSpace(ex), `\/`)
		if ex == "" {
			continue
		}
		if strings.ContainsAny(ex, `\:`) {
			// 原版按绝对路径前缀判断；相对快照无法表达，交由调用方预处理
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

// countScanFiltered 对齐原版 A.y.A_m_1 的一次性扫描计数（计数器 G）。
func (v *vfs) countScanFiltered() int {
	target := Fold(v.target)
	n := 0
	for p, e := range v.files {
		if target != "" {
			fp := Fold(p)
			if fp == target || strings.HasPrefix(fp, target+"/") {
				continue
			}
		}
		if v.scanFilteredOut(p, e) {
			n++
		}
	}
	return n
}

func (v *vfs) applyRename(sp *StepPlan, from, to string) {
	if v.cfg.DryRun {
		return
	}
	e, ok := v.files[from]
	if !ok {
		return
	}
	delete(v.files, from)
	oldKey := Fold(from)
	delete(v.foldFiles, oldKey)
	e.Path = to
	v.files[to] = e
	v.foldFiles[Fold(to)] = to
	_ = sp
}

func (v *vfs) applyDelete(from string) {
	if v.cfg.DryRun {
		return
	}
	delete(v.files, from)
	delete(v.foldFiles, Fold(from))
}

func (v *vfs) applyRemoveDir(d string) {
	if v.cfg.DryRun {
		return
	}
	delete(v.dirs, d)
	delete(v.foldDirs, Fold(d))
}

func (v *vfs) dirIsEmpty(d string) bool {
	prefix := Fold(d) + "/"
	for p := range v.files {
		if strings.HasPrefix(Fold(p), prefix) {
			return false
		}
	}
	for x := range v.dirs {
		if strings.HasPrefix(Fold(x), prefix) {
			return false
		}
	}
	return true
}

// ---- 第 1/2 步：改名 ----

func (v *vfs) stepRename(sp *StepPlan, clean func(string) string) {
	words := SplitKeywords(v.cfg.ProtectWords)
	for _, f := range v.list() {
		stem := WinStem(f.Path)
		if ContainsAnyFold(stem, words) {
			sp.Counters.Excluded++
			continue
		}
		newStem := clean(stem)
		if newStem == stem {
			continue
		}
		ext := WinExt(f.Path)
		to := JoinRel(ParentRel(f.Path), newStem+ext)
		if SamePath(to, f.Path) {
			continue
		}
		v.renameWithConflict(sp, f.Path, to)
	}
}

// renameWithConflict 对齐 B/E.cs L748-800。
func (v *vfs) renameWithConflict(sp *StepPlan, from, to string) {
	if existing, ok := v.fileExists(to); ok {
		if SamePath(existing, from) {
			return
		}
		switch v.cfg.Conflict {
		case ConflictSkip:
			sp.Counters.ConflictSkip++
			return
		case ConflictRename:
			to = v.autoRename(to)
		default: // 覆盖：删除源文件，保留已存在目标
			// 原版这里只调用删除辅助方法，成功后不增加任何计数器（实测 base specialDeleted=0），
			// 失败时才计入 conflictSkip（B/E.cs 的 A_2.b++）。
			sp.Actions = append(sp.Actions, Action{Kind: ActionDelete, From: from, Reason: "改名冲突，删除源文件（保留已存在文件）", Rule: "conflict.overwrite"})
			v.applyDelete(from)
			return
		}
	}
	sp.Actions = append(sp.Actions, Action{Kind: ActionRename, From: from, To: to, Rule: "rename"})
	v.applyRename(sp, from, to)
	sp.Counters.Renamed++
}

// autoRename 对齐 a/A.cs E：name (1).ext，最多 9998 次。
func (v *vfs) autoRename(p string) string {
	dir := ParentRel(p)
	base := WinBase(p)
	ext := WinExt(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; i < 9999; i++ {
		cand := JoinRel(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, ok := v.fileExists(cand); !ok {
			return cand
		}
	}
	return JoinRel(dir, fmt.Sprintf("%s (%s)%s", stem, "guid", ext))
}

// deleteAction 对齐 B/E.cs L909-980 的判定部分；返回是否真的发起删除。
func (v *vfs) deleteAction(sp *StepPlan, path string) bool {
	if v.cfg.Conflict == ConflictSkip {
		sp.Counters.ConflictSkip++
		return false
	}
	sp.Actions = append(sp.Actions, Action{Kind: ActionDelete, From: path, Rule: "delete"})
	v.applyDelete(path)
	return true
}

// ---- 第 3 步：删除特殊文件 ----

func (v *vfs) stepSpecialDelete(sp *StepPlan) {
	words := SplitKeywords(v.cfg.ProtectWords)
	opt := SpecialOptions{
		ProtectWords:   v.cfg.ProtectWords,
		Strength:       clampStrength(v.cfg.Strength),
		LocalWhitelist: v.cfg.LocalWhitelist,
	}
	for _, f := range v.list() {
		stem := WinStem(f.Path)
		if ContainsAnyFold(stem, words) {
			sp.Counters.Excluded++
			continue
		}
		res := ClassifySpecial(stem, opt)
		switch res.Verdict {
		case SpecialGarbage:
			if v.deleteAction(sp, f.Path) {
				sp.Counters.SpecialDeleted++
			}
		case SpecialSuspect:
			if v.cfg.AutoDeleteConfirmed && v.cfg.ConfirmedKeys != nil && v.cfg.ConfirmedKeys[Fold(f.Path)] {
				if v.deleteAction(sp, f.Path) {
					sp.Counters.SpecialDeleted++
				}
			} else {
				sp.Counters.SuspectsKept++
			}
		default:
			// 正常：原版只记录“建议改名”，不改文件系统。
			if res.Suggestion != "" && res.Suggestion != "KEEP" {
				sp.Actions = append(sp.Actions, Action{Kind: ActionSuggest, From: f.Path, Reason: res.Reason, Rule: res.Suggestion})
			}
		}
	}
}

func clampStrength(s int) int {
	if s < 0 {
		return 0
	}
	if s > 2 {
		return 2
	}
	return s
}

// ---- 第 4 步：去只读 ----

func (v *vfs) stepReadonly(sp *StepPlan) {
	for _, f := range v.list() {
		if !f.Readonly {
			continue
		}
		sp.Actions = append(sp.Actions, Action{Kind: ActionClearReadonly, From: f.Path, Rule: "readonly"})
		sp.Counters.Readonly++
		if !v.cfg.DryRun {
			if e, ok := v.files[f.Path]; ok {
				e.Readonly = false
			}
		}
	}
}

// ---- 第 5 步：内容去重 ----

func (v *vfs) stepContentDedup(sp *StepPlan) {
	files := v.list()
	bySize := map[int64][]FileEntry{}
	var sizes []int64
	for _, f := range files {
		if _, ok := bySize[f.Size]; !ok {
			sizes = append(sizes, f.Size)
		}
		bySize[f.Size] = append(bySize[f.Size], f)
	}
	for _, size := range sizes {
		group := bySize[size]
		if len(group) < 2 {
			continue
		}
		byHash := map[string][]FileEntry{}
		var hashes []string
		for _, f := range group {
			h := f.SHA256
			if h == "" && v.providers.ContentHash != nil {
				h, _ = v.providers.ContentHash(f)
			}
			if h == "" {
				continue
			}
			if _, ok := byHash[h]; !ok {
				hashes = append(hashes, h)
			}
			byHash[h] = append(byHash[h], f)
		}
		for _, h := range hashes {
			g := byHash[h]
			if len(g) < 2 {
				continue
			}
			keep := g[KeepWinner(g, v.cfg.KeepRule)].Path
			for _, f := range g {
				if v.preview != nil {
					kp, ok := v.preview[Fold(f.Path)]
					if !ok || SamePath(kp, f.Path) {
						continue
					}
					if v.deleteAction(sp, f.Path) {
						sp.Counters.ContentDeleted++
					}
					continue
				}
				if SamePath(f.Path, keep) {
					continue
				}
				if v.deleteAction(sp, f.Path) {
					sp.Counters.ContentDeleted++
				}
			}
		}
	}
}

// ---- 第 6 步：图片去重 ----

func (v *vfs) stepImageDedup(sp *StepPlan) {
	var imgs []FileEntry
	for _, f := range v.list() {
		if IsImage(f.Path) {
			imgs = append(imgs, f)
		}
	}
	if len(imgs) < 2 {
		return
	}
	groups := groupImages(imgs, v.cfg.ImageThreshold, v.providers)
	keepRule := v.cfg.ImageKeepRule
	if keepRule == "" {
		keepRule = KeepLargest
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		keep := g[KeepWinner(g, keepRule)].Path
		for _, f := range g {
			if v.preview != nil {
				kp, ok := v.preview[Fold(f.Path)]
				if !ok || SamePath(kp, f.Path) {
					continue
				}
				if v.deleteAction(sp, f.Path) {
					sp.Counters.ImageDeleted++
				}
				continue
			}
			if SamePath(f.Path, keep) {
				continue
			}
			if v.deleteAction(sp, f.Path) {
				sp.Counters.ImageDeleted++
			}
		}
	}
}

// groupImages 对齐 A/Q.cs：锚点贪心 + 组内直径约束，保持传入顺序。
func groupImages(list []FileEntry, threshold int, p Providers) [][]FileEntry {
	var groups [][]FileEntry
	var anchors []uint64
	for _, f := range list {
		h := f.DHash
		if !f.HasDHash {
			if p.ImageHash == nil {
				continue
			}
			v, ok := p.ImageHash(f)
			if !ok {
				continue
			}
			h = v
		}
		placed := false
		for gi := range groups {
			if hamming(h, anchors[gi]) > threshold {
				continue
			}
			maxD := 0
			for _, m := range groups[gi] {
				mh := m.DHash
				if !m.HasDHash && p.ImageHash != nil {
					mh, _ = p.ImageHash(m)
				}
				if d := hamming(h, mh); d > maxD {
					maxD = d
				}
			}
			if maxD <= threshold {
				groups[gi] = append(groups[gi], f)
				placed = true
				break
			}
		}
		if !placed {
			groups = append(groups, []FileEntry{f})
			anchors = append(anchors, h)
		}
	}
	return groups
}

func hamming(a, b uint64) int {
	x := a ^ b
	n := 0
	for x != 0 {
		x &= x - 1
		n++
	}
	return n
}

// ---- 第 7 步：音频去重 ----

func (v *vfs) stepAudioDedup(sp *StepPlan) {
	var audios []FileEntry
	for _, f := range v.list() {
		if IsAudio(f.Path) {
			audios = append(audios, f)
		}
	}
	if len(audios) < 2 {
		return
	}
	tol := int64(v.cfg.AudioToleranceSec) * 1000
	byExt := map[string][]FileEntry{}
	var exts []string
	for _, f := range audios {
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
			if !g[i].HasAudio && v.providers.AudioMsFn != nil {
				if ms, ok := v.providers.AudioMsFn(g[i]); ok {
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
			if len(grp) < 2 {
				continue
			}
			keep := grp[KeepWinner(grp, v.cfg.KeepRule)].Path
			for _, f := range grp {
				if v.preview != nil {
					kp, ok := v.preview[Fold(f.Path)]
					if !ok || SamePath(kp, f.Path) {
						continue
					}
					if v.deleteAction(sp, f.Path) {
						sp.Counters.AudioDeleted++
					}
					continue
				}
				if SamePath(f.Path, keep) {
					continue
				}
				if v.deleteAction(sp, f.Path) {
					sp.Counters.AudioDeleted++
				}
			}
		}
	}
}

// ---- 第 8 步：文件整理 ----

func (v *vfs) stepOrganize(sp *StepPlan) {
	target := v.target
	if strings.TrimSpace(target) == "" {
		sp.Skipped = true
		sp.SkipReason = "未选择目标文件夹，跳过整理。"
		return
	}
	// 原版进入整理步骤后立刻创建目标目录（B/E.cs L1242），试运行也照建；
	// 这里产出显式建目录动作，由执行器/对拍工具落实。
	sp.Actions = append(sp.Actions, Action{Kind: ActionEnsureDir, To: target, Rule: "organize.target"})
	v.ensureDir(target)
	var re *regexp.Regexp
	keywords := SplitKeywords(v.cfg.Keyword)
	switch v.cfg.OrganizeMode {
	case ModeKeyword:
		if len(keywords) == 0 {
			sp.Skipped = true
			sp.SkipReason = "未填写文件名关键字，跳过整理。"
			return
		}
	case ModeNameFeature:
		if strings.TrimSpace(v.cfg.Regex) == "" {
			sp.Skipped = true
			sp.SkipReason = "未填写正则表达式，跳过整理。"
			return
		}
		compiled, err := regexp.Compile("(?i)" + v.cfg.Regex)
		if err != nil {
			sp.Skipped = true
			sp.SkipReason = "正则表达式无效，跳过整理。"
			return
		}
		re = compiled
	case ModeCategory, ModeExtension, ModeFixedFolder:
	default:
		sp.Skipped = true
		sp.SkipReason = "未知整理方式，跳过整理。"
		return
	}

	for _, f := range v.list() {
		name := WinBase(f.Path)
		var destDir string
		switch v.cfg.OrganizeMode {
		case ModeCategory:
			destDir = JoinRel(target, Category(f.Path))
		case ModeExtension:
			destDir = JoinRel(target, ExtensionUpper(f.Path))
		case ModeFixedFolder:
			// 原版把“指定文件夹”直接当作目标目录（B/E.cs L1322-1327）：
			// 为空时才回退到 target/整理结果；非空时不再与 target 拼接。
			folder := strings.TrimSpace(v.cfg.FixedFolder)
			if folder == "" {
				destDir = JoinRel(target, "整理结果")
			} else {
				destDir = folder
			}
		case ModeKeyword:
			if !ContainsAnyFold(name, keywords) {
				sp.Counters.OrganizeSkip++
				continue
			}
			destDir = target
		case ModeNameFeature:
			if !re.MatchString(name) {
				sp.Counters.OrganizeSkip++
				continue
			}
			destDir = target
		}
		sanitized := SanitizeTargetName(name)
		to := JoinRel(destDir, sanitized)
		if SamePath(to, f.Path) {
			sp.Counters.OrganizeSkip++
			continue
		}
		v.organizeMove(sp, f.Path, to, sanitized != name)
	}
}

// organizeMove 对齐 B/E.cs L1401-1482 的 b_m_1。
func (v *vfs) organizeMove(sp *StepPlan, from, to string, nameChanged bool) {
	destDir := ParentRel(to)
	v.ensureDir(destDir)
	if existing, ok := v.fileExists(to); ok && !SamePath(existing, from) {
		if nameChanged && !v.sameContent(from, existing) {
			to = v.autoRename(to)
		} else {
			switch v.cfg.Conflict {
			case ConflictSkip:
				sp.Counters.OrganizeSkip++
				return
			case ConflictRename:
				to = v.autoRename(to)
			default:
				if v.deleteAction(sp, from) {
					sp.Counters.SpecialDeleted++
				} else {
					sp.Counters.OrganizeSkip++
				}
				return
			}
		}
	}
	sp.Actions = append(sp.Actions, Action{Kind: ActionMove, From: from, To: to, Rule: "organize"})
	v.applyMove(sp, from, to)
	sp.Counters.Moved++
}

func (v *vfs) applyMove(sp *StepPlan, from, to string) {
	if v.cfg.DryRun {
		return
	}
	v.applyRename(sp, from, to)
}

// sameContent 对齐 C_m_1：大小相同且全量哈希相同。
func (v *vfs) sameContent(a, b string) bool {
	ea, ok1 := v.files[a]
	eb, ok2 := v.files[b]
	if !ok1 || !ok2 {
		return false
	}
	if ea.Size != eb.Size {
		return false
	}
	ha, hb := ea.SHA256, eb.SHA256
	if ha == "" && v.providers.ContentHash != nil {
		ha, _ = v.providers.ContentHash(*ea)
	}
	if hb == "" && v.providers.ContentHash != nil {
		hb, _ = v.providers.ContentHash(*eb)
	}
	if ha == "" || hb == "" {
		return false
	}
	return ha == hb
}

// ---- 第 9 步：清空目录 ----

func (v *vfs) stepEmptyDirs(sp *StepPlan) {
	var list []string
	for d := range v.dirs {
		list = append(list, d)
	}
	sort.SliceStable(list, func(i, j int) bool { return len(list[i]) > len(list[j]) })
	rootFold := Fold(v.root)
	targetFold := Fold(v.target)
	for _, d := range list {
		fd := Fold(d)
		if fd == rootFold {
			continue
		}
		if targetFold != "" && (fd == targetFold || strings.HasPrefix(fd, targetFold+"/")) {
			continue
		}
		if !v.dirIsEmpty(d) {
			continue
		}
		sp.Actions = append(sp.Actions, Action{Kind: ActionRemoveDir, From: d, Rule: "empty_dirs"})
		v.applyRemoveDir(d)
		sp.Counters.EmptyDirs++
	}
}
