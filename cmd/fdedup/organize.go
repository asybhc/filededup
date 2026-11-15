package main

// organize 子命令：把整理器（internal/engine/organize）接到真实文件系统。
//
// 与对拍工具 tools/parity 共用枚举（internal/orgscan）与纯规划器，唯一差异是
// 这里注入 Windows 平台能力（回收站、占用检测、解除占用、硬链接、同卷判定）。
// 默认不执行：必须显式 --dry-run 或 --yes，避免误改用户目录。

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"filededup/internal/engine/organize"
	"filededup/internal/orgscan"
	"filededup/internal/platform"
)

// stdinReader 复用同一个缓冲读取器，避免多次确认时吞掉后续输入。
var stdinReader = bufio.NewReader(os.Stdin)

func runOrganize(args []string) int {
	fs := flag.NewFlagSet("organize", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "整理根目录（必填）")
	target := fs.String("target", "", "整理目标目录（root 内相对路径，或 root 外绝对路径）")
	steps := fs.String("steps", "prefix,suffix,special,readonly,content,image,audio,organize,empty",
		"启用的步骤：prefix,suffix,special,readonly,content,image,audio,organize,empty")
	mode := fs.String("mode", "category", "整理方式：category|extension|namefeature|keyword|fixedfolder")
	keyword := fs.String("keyword", "", "关键字模式的关键字（逗号/分号/空白分隔）")
	regex := fs.String("regex", "", "文件名字符特征模式的正则")
	fixed := fs.String("fixed", "", "指定文件夹模式的子目录名")
	conflict := fs.String("conflict", "rename", "冲突策略：rename|overwrite|skip（默认 rename；skip 会同时跳过去重删除，与原版一致）")
	deleteMode := fs.String("delete", "trash", "删除方式：trash|permanent（默认回收站）")
	protect := fs.String("protect", "", "保护词（含这些词的文件在改名/特殊删除中跳过）")
	keep := fs.String("keep", "pathShortest", "去重保留规则：pathShortest|newest|oldest|largest|nameShortest")
	imageKeep := fs.String("image-keep", "largest", "相似图片保留规则")
	imageThreshold := fs.Int("image-threshold", 8, "图片 dHash 汉明阈值（0-16，默认 8；调大更宽松）")
	audioTolerance := fs.Int("audio-tolerance", 2, "音频时长容差（秒）")
	strength := fs.Int("strength", 1, "垃圾识别强度：0 低 / 1 中 / 2 高")
	minSize := fs.Int64("min-size", 0, "最小文件大小（字节），0=不限")
	includeHidden := fs.Bool("include-hidden", false, "包含隐藏/系统文件；默认跳过隐藏与系统文件")
	exclude := fs.String("exclude", "", "排除目录（逗号/分号/空白分隔）")
	autoConfirm := fs.Bool("auto-confirm", false, "与 --confirmed 配合：删除清单内的存疑项（缺任一项则存疑项一律保留）")
	confirmedFile := fs.String("confirmed", "", "已确认删除清单（每行一个根内相对路径）")
	hardlink := fs.Bool("hardlink", false, "内容重复文件用硬链接合并而不是删除")
	dryRun := fs.Bool("dry-run", false, "只规划并打印（仍会像原版一样创建目标目录）")
	yes := fs.Bool("yes", false, "确认执行（必须与 --dry-run 二选一）")
	continueOnError := fs.Bool("continue-on-error", false, "单条失败后继续执行（默认中止后续）")
	releaseLock := fs.Bool("release-lock", false, "允许关闭占用文件的程序（有可见窗口时需人工确认）")
	preview := fs.Bool("preview", false, "生成并使用预览计划（先去重后整理）")
	previewPlan := fs.String("preview-plan", "", "读取持久化的预览计划 JSON（隐含 --preview 顺序）")
	savePreview := fs.String("save-preview", "", "把本次使用的预览计划写入 JSON 文件")
	out := fs.String("out", "", "结果写入文件（2 空格缩进）")
	jsonOut := fs.Bool("json", false, "结果输出到 stdout")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*root) == "" {
		fmt.Fprintln(os.Stderr, "缺少 --root")
		return 2
	}
	if *dryRun == *yes {
		fmt.Fprintln(os.Stderr, "必须且只能指定 --dry-run 或 --yes 之一")
		return 2
	}
	if *preview && *previewPlan != "" {
		fmt.Fprintln(os.Stderr, "--preview 与 --preview-plan 不能同时使用（前者现场生成，后者读取文件）")
		return 2
	}

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无效根目录 %q：%v\n", *root, err)
		return 2
	}
	info, err := os.Stat(absRoot)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "根目录不可用或不是目录：%s\n", absRoot)
		return 2
	}
	absRoot = filepath.Clean(absRoot)

	targetSpec, err := organizeTarget(absRoot, *target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}

	cfg := organize.DefaultConfig()
	cfg.Steps = organize.ParseSteps(*steps)
	cfg.DryRun = *dryRun
	cfg.Conflict = organize.ParseConflictPolicy(*conflict)
	cfg.Delete = organize.ParseDeleteMode(*deleteMode)
	cfg.HardlinkMerge = *hardlink
	cfg.Excludes = organize.SplitKeywords(*exclude)
	cfg.MinSize = *minSize
	cfg.SkipHiddenSystem = !*includeHidden
	cfg.ProtectWords = *protect
	cfg.Strength = organize.ParseStrength(*strength)
	cfg.AutoDeleteConfirmed = *autoConfirm
	cfg.KeepRule = organize.ParseKeepRule(*keep)
	cfg.ImageKeepRule = organize.ParseKeepRule(*imageKeep)
	if cfg.ImageKeepRule == "" {
		cfg.ImageKeepRule = organize.KeepLargest
	}
	cfg.ImageThreshold = *imageThreshold
	cfg.AudioToleranceSec = *audioTolerance
	cfg.OrganizeMode = organize.ParseOrganizeMode(*mode)
	cfg.Keyword = *keyword
	cfg.Regex = *regex
	cfg.FixedFolder = *fixed
	if *confirmedFile != "" {
		keys, err := loadConfirmedKeys(*confirmedFile, absRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
		cfg.ConfirmedKeys = keys
	}

	tree, err := orgscan.Collect(absRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "枚举失败：%v\n", err)
		return 3
	}
	providers := orgscan.PrepareProviders(tree.Entries, absRoot, cfg)
	snap := organize.Snapshot{Root: absRoot, Target: targetSpec.Rel, Files: tree.Entries, Dirs: tree.Dirs}

	// 预览计划：文件优先，其次现场生成；两者都要求“先去重后整理”的顺序。
	var usedPlan *organize.PreviewPlan
	if *previewPlan != "" {
		p, err := organize.LoadPreviewPlan(*previewPlan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取预览计划失败：%v\n", err)
			return 2
		}
		if p.Root != "" && !samePath(p.Root, absRoot) {
			fmt.Fprintf(os.Stderr, "警告：预览计划的 root（%s）与 --root（%s）不同，按根内相对路径匹配\n", p.Root, absRoot)
		}
		if err := checkPreviewRefs(p, tree.Entries); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 2
		}
		snap.PreviewPlan = p.Map()
		usedPlan = &p
	} else if *preview || *savePreview != "" {
		p := organize.BuildPreviewPlan(cfg, snap, providers)
		snap.PreviewPlan = p.Map()
		usedPlan = &p
	}
	if usedPlan != nil {
		cfg.Order = organize.OrderPreview
		if *savePreview != "" {
			if err := organize.SavePreviewPlan(*savePreview, *usedPlan); err != nil {
				fmt.Fprintf(os.Stderr, "写入预览计划失败：%v\n", err)
				return 3
			}
			fmt.Fprintf(os.Stderr, "预览计划已写入 %s（%d 条）\n", *savePreview, len(usedPlan.Entries))
		}
	}

	res := organize.Plan(cfg, snap, providers)
	if targetSpec.External {
		organize.RewriteExternalTarget(&res, targetSpec.Abs)
	}
	report, err := organize.Apply(absRoot, res, organize.ExecOptions{
		DryRun:          *dryRun,
		ContinueOnError: *continueOnError,
		ReleaseLock:     *releaseLock,
	}, organizeHooks(cfg.Delete))
	if err != nil {
		fmt.Fprintf(os.Stderr, "执行失败：%v\n", err)
		return 3
	}

	payload := organizeOutput{
		Root: absRoot, Target: targetDisplay(targetSpec), DryRun: *dryRun, Order: cfg.Order,
		PreviewPlan: usedPlan, Result: res, Exec: report,
	}
	if *out != "" {
		if err := writeJSON(*out, payload); err != nil {
			fmt.Fprintf(os.Stderr, "写入 %s 失败：%v\n", *out, err)
			return 3
		}
	}
	if *jsonOut {
		if err := writeJSON("", payload); err != nil {
			fmt.Fprintf(os.Stderr, "输出失败：%v\n", err)
			return 3
		}
	}
	if !*jsonOut {
		printOrganizeSummary(payload)
	}
	if report.Failed > 0 {
		return 3
	}
	return 0
}

type organizeOutput struct {
	Root        string                `json:"root"`
	Target      string                `json:"target,omitempty"`
	DryRun      bool                  `json:"dryRun"`
	Order       organize.OrderMode    `json:"order"`
	PreviewPlan *organize.PreviewPlan `json:"previewPlan,omitempty"`
	Result      organize.Result       `json:"result"`
	Exec        organize.ExecReport   `json:"exec"`
}

func printOrganizeSummary(o organizeOutput) {
	mode := "执行"
	if o.DryRun {
		mode = "试运行"
	}
	fmt.Printf("整理%s：总动作 %d，成功 %d，失败 %d，跳过 %d（顺序 %s）\n",
		mode, o.Exec.Total, o.Exec.Applied, o.Exec.Failed, o.Exec.Skipped, o.Order)
	c := o.Result.Counters
	fmt.Printf("计数：改名 %d，移动 %d，只读 %d，特殊删除 %d，内容去重 %d，图片去重 %d，音频去重 %d，硬链接合并 %d，空目录 %d\n",
		c.Renamed, c.Moved, c.Readonly, c.SpecialDeleted, c.ContentDeleted, c.ImageDeleted, c.AudioDeleted,
		c.HardlinkMerged, c.EmptyDirs)
	fmt.Printf("跳过：冲突 %d，整理 %d，排除 %d，隐藏/系统 %d，存疑保留 %d\n",
		c.ConflictSkip, c.OrganizeSkip, c.Excluded, c.HiddenSkipped, c.SuspectsKept)
	for _, step := range o.Result.Steps {
		if step.Skipped {
			fmt.Printf("  [跳过] %s（%s）\n", step.Title, step.SkipReason)
			continue
		}
		fmt.Printf("  [%2d 条] %s\n", len(step.Actions), step.Title)
	}
	for _, line := range o.Exec.Errors {
		fmt.Fprintf(os.Stderr, "失败：%s\n", line)
	}
}

// organizeTarget 解析 --target：根内相对路径，或根目录之外的绝对路径。
func organizeTarget(root, target string) (organize.TargetSpec, error) {
	return organize.ResolveTarget(root, target)
}

// targetDisplay 返回报告里的整理目标：根内相对路径或根外绝对路径。
func targetDisplay(spec organize.TargetSpec) string {
	if spec.External {
		return spec.Abs
	}
	return spec.Rel
}

func loadConfirmedKeys(path, root string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 --confirmed 失败：%w", err)
	}
	keys := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		rel := p
		if filepath.IsAbs(p) {
			if r, rerr := filepath.Rel(root, p); rerr == nil {
				rel = r
			}
		}
		rel = strings.TrimRight(filepath.ToSlash(rel), "/")
		keys[organize.Fold(rel)] = true
	}
	return keys, nil
}

// checkPreviewRefs 拒绝引用已不存在文件的预览计划：过期计划应重新生成，
// 而不是带着旧映射去执行（否则保留项可能指向已经不在的文件）。
func checkPreviewRefs(p organize.PreviewPlan, entries []organize.FileEntry) error {
	exists := make(map[string]bool, len(entries))
	for _, e := range entries {
		exists[organize.Fold(e.Path)] = true
	}
	var missing []string
	for _, e := range p.Entries {
		if !exists[organize.Fold(e.From)] {
			missing = append(missing, e.From)
		}
		if !exists[organize.Fold(e.Keep)] {
			missing = append(missing, e.Keep)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	shown := missing
	if len(shown) > 5 {
		shown = shown[:5]
	}
	return fmt.Errorf("预览计划已过期：引用了不存在的文件 %s（共 %d 个），请重新生成",
		strings.Join(shown, "、"), len(missing))
}

// organizeHooks 注入平台能力；引擎本身不依赖 filededup/internal/platform。
func organizeHooks(deleteMode organize.DeleteMode) organize.ExecHooks {
	hooks := organize.ExecHooks{
		IsLocked:   platform.IsLocked,
		SameVolume: platform.SameVolume,
		Link:       platform.Link,
		ConfirmLock: func(absPath string, holderNames []string) bool {
			fmt.Fprintf(os.Stderr, "文件被占用：%s\n占用程序：%s\n关闭这些程序以继续？[y/N] ",
				absPath, strings.Join(holderNames, "、"))
			line, err := stdinReader.ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(os.Stderr)
				return false
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y", "yes", "是":
				return true
			}
			return false
		},
		ReleaseLocks: func(absPath string, confirm func(absPath string, names []string) bool) error {
			if confirm == nil {
				return platform.ReleaseLocks(absPath, nil)
			}
			return platform.ReleaseLocks(absPath, func(p string, holders []platform.LockHolder) bool {
				names := make([]string, 0, len(holders))
				for _, h := range holders {
					names = append(names, h.Name)
				}
				return confirm(p, names)
			})
		},
	}
	if deleteMode == organize.DeleteTrash {
		// 非 Windows 平台由 platform.Trash 返回“未实现”，执行器按失败记录，
		// 不会静默退化成永久删除。
		hooks.Trash = platform.Trash
	}
	return hooks
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
