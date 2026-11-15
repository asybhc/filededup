package organize

import (
	"reflect"
	"testing"
)

// ---- 测试辅助 ----

// fe 构造最小文件条目：只关心路径，其余属性在用例中按需覆盖。
func fe(path string) FileEntry { return FileEntry{Path: path} }

// snap 用固定快照语料构造 Snapshot，全程不触碰真实文件系统。
func snap(files ...FileEntry) Snapshot { return Snapshot{Files: files} }

// stepByID 取出指定步骤；缺失即视为规划器未按九步输出。
func stepByID(t *testing.T, res Result, id StepID) StepPlan {
	t.Helper()
	for _, sp := range res.Steps {
		if sp.ID == string(id) {
			return sp
		}
	}
	t.Fatalf("结果中缺少步骤 %s，实际步骤=%v", id, stepIDs(res))
	return StepPlan{}
}

// stepIDs 返回结果中步骤 ID 的原始顺序，用于断言两套执行顺序。
func stepIDs(res Result) []string {
	out := make([]string, len(res.Steps))
	for i, sp := range res.Steps {
		out[i] = sp.ID
	}
	return out
}

// actionsOf 按动作类型过滤，避免断言被其他类型的动作干扰。
func actionsOf(sp StepPlan, kind ActionKind) []Action {
	var out []Action
	for _, a := range sp.Actions {
		if a.Kind == kind {
			out = append(out, a)
		}
	}
	return out
}

// actionTriples 把动作压成 "kind|from|to" 字符串便于逐条比较。
func actionTriples(actions []Action) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = string(a.Kind) + "|" + a.From + "|" + a.To
	}
	return out
}

// assertActions 断言动作序列（类型 + 源 + 目标）完全一致。
func assertActions(t *testing.T, label string, got []Action, want []string) {
	t.Helper()
	g := actionTriples(got)
	if len(g) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(g, want) {
		t.Errorf("%s 动作序列 = %#v，期望 %#v", label, g, want)
	}
}

// ---- 1. 两套执行顺序（C# B/E.cs L447-516） ----

// TestPlanStandaloneOrder 验证无预览计划时的九步顺序：改名 → 特殊删除 → 去只读 → 去重 → 整理 → 清空目录。
func TestPlanStandaloneOrder(t *testing.T) {
	res := Plan(DefaultConfig(), snap(fe("a.txt")), Providers{})
	if res.Order != OrderStandalone {
		t.Errorf("Order = %q，期望 %q", res.Order, OrderStandalone)
	}
	want := []string{
		"rename_prefix", "rename_suffix", "special_delete", "readonly",
		"content_dedup", "image_dedup", "audio_dedup", "organize", "empty_dirs",
	}
	got := stepIDs(res)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("standalone 步骤序列 = %v，期望 %v", got, want)
	}
	if len(res.Steps) != 9 {
		t.Fatalf("步骤数 = %d，期望 9", len(res.Steps))
	}
	// Order 字段必须为 1..9，对应原版进度显示序号。
	for i, sp := range res.Steps {
		if sp.Order != i+1 {
			t.Errorf("步骤 %s 的 Order = %d，期望 %d", sp.ID, sp.Order, i+1)
		}
	}
}

// TestPlanPreviewOrder 验证有预览计划时先去重、再改名/删除/去只读、最后整理与清空目录。
func TestPlanPreviewOrder(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Order = OrderPreview
	res := Plan(cfg, snap(fe("a.txt")), Providers{})
	if res.Order != OrderPreview {
		t.Errorf("Order = %q，期望 %q", res.Order, OrderPreview)
	}
	want := []string{
		"content_dedup", "image_dedup", "audio_dedup",
		"rename_prefix", "rename_suffix", "special_delete", "readonly",
		"organize", "empty_dirs",
	}
	got := stepIDs(res)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("preview 步骤序列 = %v，期望 %v", got, want)
	}
	for i, sp := range res.Steps {
		if sp.Order != i+1 {
			t.Errorf("步骤 %s 的 Order = %d，期望 %d", sp.ID, sp.Order, i+1)
		}
	}
}

// ---- 2. 默认开关（C# b/D.cs 默认值） ----

// TestPlanDefaultStepSwitchesSkip 验证默认配置下五个步骤被跳过，且跳过原因统一为“开关未启用”。
func TestPlanDefaultStepSwitchesSkip(t *testing.T) {
	cfg := DefaultConfig()
	s := snap(fe("a.txt"))
	s.Target = "target" // 有目标目录，保证 organize 步骤本身不被“未选择目标”跳过
	res := Plan(cfg, s, Providers{})

	wantSkipped := map[string]bool{
		"special_delete": true,
		"content_dedup":  true,
		"image_dedup":    true,
		"audio_dedup":    true,
		"empty_dirs":     true,
	}
	seen := 0
	for _, sp := range res.Steps {
		if wantSkipped[sp.ID] {
			seen++
			if !sp.Skipped {
				t.Errorf("步骤 %s 应被默认开关跳过，实际 Skipped=false", sp.ID)
			}
			if sp.SkipReason != "开关未启用" {
				t.Errorf("步骤 %s 的 SkipReason = %q，期望 %q", sp.ID, sp.SkipReason, "开关未启用")
			}
			continue
		}
		if sp.Skipped {
			t.Errorf("步骤 %s 不应被跳过，实际 SkipReason=%q", sp.ID, sp.SkipReason)
		}
	}
	if seen != 5 {
		t.Errorf("被默认开关跳过的步骤数 = %d，期望 5", seen)
	}
}

// ---- 3. 改名动作（C# B/R.cs L91/L177） ----

// TestPlanRenameActions 验证前缀/后缀清理的 From/To 与 Renamed 计数；无变化时不产生动作。
func TestPlanRenameActions(t *testing.T) {
	t.Run("前缀与后缀", func(t *testing.T) {
		res := Plan(DefaultConfig(), snap(
			fe("01 - 报告.pdf"),
			fe("copy1 文档 (2).docx"),
			fe("报告 副本.txt"),
		), Providers{})

		// 第一步：去掉前导序号/复制标记，以及名称中的括号序号。
		prefix := stepByID(t, res, StepRenamePrefix)
		assertActions(t, "第一步改名", prefix.Actions, []string{
			"rename|01 - 报告.pdf|报告.pdf",
			"rename|copy1 文档 (2).docx|文档.docx",
		})
		if prefix.Counters.Renamed != 2 {
			t.Errorf("第一步 Renamed = %d，期望 2", prefix.Counters.Renamed)
		}

		// 第二步：去掉尾部“副本”等后缀；前一步已清理的文件不再变化。
		suffix := stepByID(t, res, StepRenameSuffix)
		assertActions(t, "第二步改名", suffix.Actions, []string{
			"rename|报告 副本.txt|报告.txt",
		})
		if suffix.Counters.Renamed != 1 {
			t.Errorf("第二步 Renamed = %d，期望 1", suffix.Counters.Renamed)
		}
	})

	t.Run("无变化不产生动作", func(t *testing.T) {
		// 原版在 text2 == 原名（即目标与源同路径）时直接 return，不产生改名动作。
		res := Plan(DefaultConfig(), snap(fe("文档.txt"), fe("copy.txt")), Providers{})
		prefix := stepByID(t, res, StepRenamePrefix)
		suffix := stepByID(t, res, StepRenameSuffix)
		assertActions(t, "第一步改名", prefix.Actions, nil)
		assertActions(t, "第二步改名", suffix.Actions, nil)
		if prefix.Counters.Renamed != 0 || suffix.Counters.Renamed != 0 {
			t.Errorf("Renamed 计数 = %d/%d，期望 0/0",
				prefix.Counters.Renamed, suffix.Counters.Renamed)
		}
	})
}

// ---- 4. 改名冲突三策略（C# B/E.cs L748-800） ----

// TestPlanRenameConflictPolicies 覆盖目标已存在同名文件时的覆盖/跳过/自动重命名三种策略。
func TestPlanRenameConflictPolicies(t *testing.T) {
	const src, target = "01 - 报告.pdf", "报告.pdf"
	cases := []struct {
		name          string
		policy        ConflictPolicy
		wantKind      ActionKind
		wantTo        string
		wantActions   int
		wantRenamed   int
		wantSpecial   int
		wantSkipCount int
	}{
		// A：删除源文件、保留已存在目标；实测原版此分支不增加任何计数器
		// （delete helper 成功后无 A_2.a++，失败才计 A_2.b++）。
		{"覆盖策略删除源文件", ConflictOverwrite, ActionDelete, "", 1, 0, 0, 0},
		// a：跳过并计入冲突跳过（原版 A_2.b++），不产生改名动作。
		{"跳过策略", ConflictSkip, "", "", 0, 0, 0, 1},
		// B：自动重命名 name (1).ext（原版 global::a.a.E）。
		{"自动重命名", ConflictRename, ActionRename, "报告 (1).pdf", 1, 1, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Conflict = c.policy
			res := Plan(cfg, snap(fe(src), fe(target)), Providers{})
			sp := stepByID(t, res, StepRenamePrefix)

			if len(sp.Actions) != c.wantActions {
				t.Fatalf("动作数 = %d，期望 %d（实际动作=%#v）",
					len(sp.Actions), c.wantActions, actionTriples(sp.Actions))
			}
			// 改名/删除动作都应落在源文件上；只有自动重命名带目标路径。
			for _, a := range sp.Actions {
				if a.From != src {
					t.Errorf("动作 From = %q，期望源文件 %q", a.From, src)
				}
				if a.Kind != c.wantKind {
					t.Errorf("动作 Kind = %q，期望 %q", a.Kind, c.wantKind)
				}
				if c.wantTo != "" && a.To != c.wantTo {
					t.Errorf("动作 To = %q，期望 %q", a.To, c.wantTo)
				}
			}
			if sp.Counters.Renamed != c.wantRenamed {
				t.Errorf("Renamed = %d，期望 %d", sp.Counters.Renamed, c.wantRenamed)
			}
			if sp.Counters.SpecialDeleted != c.wantSpecial {
				t.Errorf("SpecialDeleted = %d，期望 %d", sp.Counters.SpecialDeleted, c.wantSpecial)
			}
			if sp.Counters.ConflictSkip != c.wantSkipCount {
				t.Errorf("ConflictSkip = %d，期望 %d", sp.Counters.ConflictSkip, c.wantSkipCount)
			}
		})
	}
}

// ---- 5. 试运行语义（原版“预演不落盘”） ----

// TestPlanDryRun 验证同一份语料的动作列表在试运行/实际模式下一致，
// 但试运行不推进虚拟文件系统，后续步骤仍按原路径处理。
func TestPlanDryRun(t *testing.T) {
	t.Run("动作列表一致", func(t *testing.T) {
		// 语料要求：后续步骤对原始文件名不产生与“已改名结果”不同的动作，
		// 否则预演（不落盘）与实际（落盘）会天然看到不同路径。
		files := []FileEntry{fe("01 - 报告.pdf"), fe("文档 副本.txt")}
		dry := DefaultConfig()
		dry.DryRun = true
		real := DefaultConfig()
		resDry := Plan(dry, snap(files...), Providers{})
		resReal := Plan(real, snap(files...), Providers{})

		if len(resDry.Steps) != len(resReal.Steps) {
			t.Fatalf("步骤数不一致：DryRun=%d，实际=%d", len(resDry.Steps), len(resReal.Steps))
		}
		for i := range resDry.Steps {
			a, b := resDry.Steps[i], resReal.Steps[i]
			if !reflect.DeepEqual(a.Actions, b.Actions) {
				t.Errorf("步骤 %s 动作不一致：DryRun=%#v，实际=%#v",
					a.ID, actionTriples(a.Actions), actionTriples(b.Actions))
			}
			if a.Counters != b.Counters {
				t.Errorf("步骤 %s 计数不一致：DryRun=%+v，实际=%+v", a.ID, a.Counters, b.Counters)
			}
		}
		if resDry.Counters != resReal.Counters {
			t.Errorf("总计数不一致：DryRun=%+v，实际=%+v", resDry.Counters, resReal.Counters)
		}
	})

	t.Run("预演不落盘", func(t *testing.T) {
		// 第一步把 “01 文档副本.txt” 改成 “文档副本.txt”；第二步才能继续去掉“副本”。
		files := []FileEntry{fe("01 文档副本.txt")}
		dry := DefaultConfig()
		dry.DryRun = true
		resDry := Plan(dry, snap(files...), Providers{})
		resReal := Plan(DefaultConfig(), snap(files...), Providers{})

		// 两种模式的第一步动作完全相同。
		wantPrefix := []string{"rename|01 文档副本.txt|文档副本.txt"}
		assertActions(t, "DryRun 第一步", actionsOf(stepByID(t, resDry, StepRenamePrefix), ActionRename), wantPrefix)
		assertActions(t, "实际 第一步", actionsOf(stepByID(t, resReal, StepRenamePrefix), ActionRename), wantPrefix)

		// 试运行模式下 vfs 未推进，第二步仍按原名计算。
		assertActions(t, "DryRun 第二步", actionsOf(stepByID(t, resDry, StepRenameSuffix), ActionRename),
			[]string{"rename|01 文档副本.txt|01 文档.txt"})
		// 实际模式下第二步看到的是第一步改名后的路径。
		assertActions(t, "实际 第二步", actionsOf(stepByID(t, resReal, StepRenameSuffix), ActionRename),
			[]string{"rename|文档副本.txt|文档.txt"})
	})
}

// ---- 6. 第五步：文件整理（C# B/E.cs L1401-1482） ----

// TestPlanOrganizeModes 覆盖按类型/后缀/固定文件夹整理、关键字未命中与未选择目标目录。
func TestPlanOrganizeModes(t *testing.T) {
	cases := []struct {
		name           string
		mode           OrganizeMode
		keyword        string
		fixedFolder    string
		target         string
		file           string
		wantActions    []string
		wantMoved      int
		wantSkip       int
		wantSkipped    bool
		wantSkipReason string
	}{
		{
			// 按类型分类：target/<分类>/文件名。
			name: "按类型分类", mode: ModeCategory, target: "target", file: "note.pdf",
			wantActions: []string{"move|note.pdf|target/PDF/note.pdf"}, wantMoved: 1,
		},
		{
			// 按后缀整理：target/<大写扩展名>/文件名。
			name: "按扩展名整理", mode: ModeExtension, target: "target", file: "clip.mp4",
			wantActions: []string{"move|clip.mp4|target/MP4/clip.mp4"}, wantMoved: 1,
		},
		{
			// 按指定文件夹整理：原版直接把该值当目标目录（不再拼 target），
			// 见 B/E.cs L1322-1327；为空时才回退 target/整理结果。
			name: "按固定文件夹整理", mode: ModeFixedFolder, fixedFolder: "固定名", target: "target", file: "note.pdf",
			wantActions: []string{"move|note.pdf|固定名/note.pdf"}, wantMoved: 1,
		},
		{
			// 关键字未命中：计入 OrganizeSkip，不移动。
			name: "关键字未命中", mode: ModeKeyword, keyword: "报告", target: "target", file: "note.txt",
			wantSkip: 1,
		},
		{
			// 目标为空：整个步骤跳过（原版“未选择目标文件夹，跳过整理。”）。
			name: "未选择目标文件夹", mode: ModeCategory, target: "", file: "note.pdf",
			wantSkipped: true, wantSkipReason: "未选择目标文件夹，跳过整理。",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.OrganizeMode = c.mode
			cfg.Keyword = c.keyword
			cfg.FixedFolder = c.fixedFolder
			s := snap(fe(c.file))
			s.Target = c.target

			sp := stepByID(t, Plan(cfg, s, Providers{}), StepOrganize)
			if sp.Skipped != c.wantSkipped {
				t.Errorf("Skipped = %v，期望 %v（SkipReason=%q）", sp.Skipped, c.wantSkipped, sp.SkipReason)
			}
			if c.wantSkipped {
				if sp.SkipReason != c.wantSkipReason {
					t.Errorf("SkipReason = %q，期望 %q", sp.SkipReason, c.wantSkipReason)
				}
				if len(sp.Actions) != 0 {
					t.Errorf("跳过的步骤不应有动作，实际 %#v", actionTriples(sp.Actions))
				}
				return
			}
			// 整理步骤先产出显式的建目标目录动作（原版进入步骤即 CreateDirectory）。
			ensures := actionsOf(sp, ActionEnsureDir)
			if len(ensures) != 1 || ensures[0].To != c.target {
				t.Errorf("建目录动作 = %#v，期望 1 条指向 %q", actionTriples(ensures), c.target)
			}
			assertActions(t, "整理", actionsOf(sp, ActionMove), c.wantActions)
			if sp.Counters.Moved != c.wantMoved {
				t.Errorf("Moved = %d，期望 %d", sp.Counters.Moved, c.wantMoved)
			}
			if sp.Counters.OrganizeSkip != c.wantSkip {
				t.Errorf("OrganizeSkip = %d，期望 %d", sp.Counters.OrganizeSkip, c.wantSkip)
			}
		})
	}
}

// TestPlanOrganizeExistingTarget 覆盖目标已存在时的两条分支：
// 原名被清洗且内容不同 → 自动重命名；内容相同 → 走冲突策略（C# C_m_1 / b_m_1）。
func TestPlanOrganizeExistingTarget(t *testing.T) {
	const src = "src/报告 (1) (2) (3).txt"
	// 目标名由 SanitizeTargetName 去掉多余括号对得到，因此 flag=true，会进入内容比较分支。
	// 固定文件夹模式的目标目录就是该值本身（不拼 target）。
	const dst = "固定名/报告 1 2 3.txt"
	const renamed = "固定名/报告 1 2 3 (1).txt"

	cases := []struct {
		name        string
		conflict    ConflictPolicy
		destHash    string
		wantActions []string
		wantMoved   int
		wantSkip    int
	}{
		{
			name: "内容不同时自动重命名", conflict: ConflictSkip, destHash: "OTHER",
			// 已在固定文件夹内的目标文件与自身同路径，按原版计入 OrganizeSkip。
			wantActions: []string{"move|" + src + "|" + renamed}, wantMoved: 1, wantSkip: 1,
		},
		{
			name: "内容相同时按冲突策略跳过", conflict: ConflictSkip, destHash: "SAME",
			wantSkip: 2,
		},
		{
			name: "内容相同时按冲突策略重命名", conflict: ConflictRename, destHash: "SAME",
			wantActions: []string{"move|" + src + "|" + renamed}, wantMoved: 1, wantSkip: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.OrganizeMode = ModeFixedFolder
			cfg.FixedFolder = "固定名"
			cfg.Conflict = c.conflict
			// 关闭改名步骤，确保整理步骤输入仍是原始路径。
			cfg.Steps.RenamePrefix = false
			cfg.Steps.RenameSuffix = false

			s := snap(
				FileEntry{Path: src, Size: 100},
				FileEntry{Path: dst, Size: 100},
			)
			s.Target = "target"

			// 注入固定哈希：sameContent 要求大小相同且全量哈希相同。
			providers := Providers{ContentHash: func(e FileEntry) (string, bool) {
				if e.Path == dst {
					return c.destHash, true
				}
				return "SAME", true
			}}

			sp := stepByID(t, Plan(cfg, s, providers), StepOrganize)
			assertActions(t, "整理", actionsOf(sp, ActionMove), c.wantActions)
			if sp.Counters.Moved != c.wantMoved {
				t.Errorf("Moved = %d，期望 %d", sp.Counters.Moved, c.wantMoved)
			}
			if sp.Counters.OrganizeSkip != c.wantSkip {
				t.Errorf("OrganizeSkip = %d，期望 %d", sp.Counters.OrganizeSkip, c.wantSkip)
			}
		})
	}
}

// ---- 7. 空目录清理（C# B/E.cs 步骤 F） ----

// TestPlanEmptyDirs 验证嵌套空目录由深到浅删除，目标目录与子树、非空目录不删除。
func TestPlanEmptyDirs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Steps.EmptyDirs = true
	// 关闭整理步骤，避免示例文件被移入目标目录后让 z/keep 变成空目录。
	cfg.Steps.Organize = false

	s := snap(fe("z/keep/file.txt"))
	s.Target = "target"
	s.Dirs = []string{"z", "z/keep", "空", "空/子", "空/子/更深", "target", "target/空子"}

	sp := stepByID(t, Plan(cfg, s, Providers{}), StepEmptyDirs)
	want := []string{
		"remove_dir|空/子/更深|",
		"remove_dir|空/子|",
		"remove_dir|空|",
	}
	assertActions(t, "清理空目录", sp.Actions, want)
	if sp.Counters.EmptyDirs != 3 {
		t.Errorf("EmptyDirs = %d，期望 3", sp.Counters.EmptyDirs)
	}

	for _, a := range sp.Actions {
		f := Fold(a.From)
		// 根目录（相对根为空串）不得删除。
		if a.From == "" {
			t.Errorf("不应删除根目录，实际动作=%#v", actionTriples(sp.Actions))
		}
		// Target 及其子树不得删除。
		if f == "target" || len(f) > len("target/") && f[:len("target/")] == "target/" {
			t.Errorf("不应删除目标目录及其子树：%q", a.From)
		}
		// 非空目录不得删除。
		if f == "z" || f == "z/keep" {
			t.Errorf("非空目录不应删除：%q", a.From)
		}
	}
}

// ---- 8. 预览计划驱动的内容去重（C# 有预览计划时的映射） ----

// TestPlanContentDedupPreviewPlan 验证预览计划非空时按映射删除非保留项，即使 KeepWinner 会选别人。
func TestPlanContentDedupPreviewPlan(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Order = OrderPreview
	cfg.Steps.ContentDedup = true

	files := []FileEntry{
		{Path: "a.txt", Size: 10, SHA256: "SAME"},
		{Path: "b.txt", Size: 10, SHA256: "SAME"},
		{Path: "c.txt", Size: 10, SHA256: "SAME"},
	}
	// KeepShortestPath 会保留 a.txt（路径最短且并列时取首个），预览计划却指定保留 c.txt。
	if got := files[KeepWinner(files, KeepShortestPath)].Path; got != "a.txt" {
		t.Fatalf("用例前提不成立：KeepWinner 选择了 %q，期望 a.txt", got)
	}
	s := snap(files...)
	s.PreviewPlan = map[string]string{
		"a.txt": "c.txt",
		"b.txt": "c.txt",
		"c.txt": "c.txt",
	}

	sp := stepByID(t, Plan(cfg, s, Providers{}), StepContentDedup)
	assertActions(t, "预览计划内容去重", sp.Actions, []string{
		"delete|a.txt|",
		"delete|b.txt|",
	})
	if sp.Counters.ContentDeleted != 2 {
		t.Errorf("ContentDeleted = %d，期望 2", sp.Counters.ContentDeleted)
	}
}

// ---- 9. 内容去重（C# B/E.cs 内容哈希分组） ----

// TestPlanContentDedupShortestPath 验证同大小同哈希三文件默认保留路径最短者，删除另外两个。
func TestPlanContentDedupShortestPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Steps.ContentDedup = true

	res := Plan(cfg, snap(
		FileEntry{Path: "keep.txt", Size: 10, SHA256: "H"},
		FileEntry{Path: "dup/one.txt", Size: 10, SHA256: "H"},
		FileEntry{Path: "dup/another_long_name.txt", Size: 10, SHA256: "H"},
	), Providers{})

	sp := stepByID(t, res, StepContentDedup)
	assertActions(t, "内容去重", sp.Actions, []string{
		"delete|dup/another_long_name.txt|",
		"delete|dup/one.txt|",
	})
	if sp.Counters.ContentDeleted != 2 {
		t.Errorf("ContentDeleted = %d，期望 2", sp.Counters.ContentDeleted)
	}
}

// ---- 11. 硬链接合并（C# a/K.cs，仅去重删除且带 keepPath 时触发） ----

// TestPlanHardlinkMerge 验证开启硬链接合并后，去重删除改为 hardlink 动作；
// 试运行不产出硬链接，而预览计划仍复用已有计划真实执行硬链接。
func TestPlanHardlinkMerge(t *testing.T) {
	files := []FileEntry{
		{Path: "keep.txt", Size: 10, SHA256: "H"},
		{Path: "dup/one.txt", Size: 10, SHA256: "H"},
		{Path: "dup/another_long_name.txt", Size: 10, SHA256: "H"},
	}
	wantHardlinks := []string{
		"hardlink|dup/another_long_name.txt|keep.txt",
		"hardlink|dup/one.txt|keep.txt",
	}

	t.Run("开启硬链接合并", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Steps.ContentDedup = true
		cfg.HardlinkMerge = true

		sp := stepByID(t, Plan(cfg, snap(files...), Providers{}), StepContentDedup)
		assertActions(t, "硬链接合并", sp.Actions, wantHardlinks)
		// 原版硬链接成功时删除辅助仍返回 true，去重计数照常累加。
		if sp.Counters.ContentDeleted != 2 {
			t.Errorf("ContentDeleted = %d，期望 2", sp.Counters.ContentDeleted)
		}
		if sp.Counters.HardlinkMerged != 2 {
			t.Errorf("HardlinkMerged = %d，期望 2", sp.Counters.HardlinkMerged)
		}
	})

	t.Run("关闭时仍删除", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Steps.ContentDedup = true

		sp := stepByID(t, Plan(cfg, snap(files...), Providers{}), StepContentDedup)
		if got := actionsOf(sp, ActionHardlink); len(got) != 0 {
			t.Errorf("未开启时不应有硬链接动作：%#v", actionTriples(got))
		}
		if sp.Counters.HardlinkMerged != 0 {
			t.Errorf("HardlinkMerged = %d，期望 0", sp.Counters.HardlinkMerged)
		}
	})

	t.Run("试运行不产出硬链接", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Steps.ContentDedup = true
		cfg.HardlinkMerge = true
		cfg.DryRun = true

		sp := stepByID(t, Plan(cfg, snap(files...), Providers{}), StepContentDedup)
		if got := actionsOf(sp, ActionHardlink); len(got) != 0 {
			t.Errorf("试运行不应产出硬链接：%#v", actionTriples(got))
		}
		if sp.Counters.HardlinkMerged != 0 {
			t.Errorf("试运行 HardlinkMerged = %d，期望 0", sp.Counters.HardlinkMerged)
		}
		if sp.Counters.ContentDeleted != 2 {
			t.Errorf("试运行 ContentDeleted = %d，期望 2", sp.Counters.ContentDeleted)
		}
	})

	t.Run("预览计划下仍然产出硬链接", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Order = OrderPreview
		cfg.Steps.ContentDedup = true
		cfg.HardlinkMerge = true

		s := snap(files...)
		s.PreviewPlan = map[string]string{
			"keep.txt":                  "keep.txt",
			"dup/one.txt":               "keep.txt",
			"dup/another_long_name.txt": "keep.txt",
		}
		sp := stepByID(t, Plan(cfg, s, Providers{}), StepContentDedup)
		assertActions(t, "预览计划硬链接合并", sp.Actions, wantHardlinks)
		if sp.Counters.HardlinkMerged != 2 {
			t.Errorf("预览计划 HardlinkMerged = %d，期望 2", sp.Counters.HardlinkMerged)
		}
		if sp.Counters.ContentDeleted != 2 {
			t.Errorf("预览计划 ContentDeleted = %d，期望 2", sp.Counters.ContentDeleted)
		}
	})
}

// ---- 10. 音频相似去重（C# 时长容差分组） ----

// TestPlanAudioDedup 验证同扩展名且时长差在 AudioToleranceSec 内的两个文件成组，删除非保留项。
func TestPlanAudioDedup(t *testing.T) {
	cases := []struct {
		name        string
		useProvider bool
		msA, msB    int64
		wantDeleted bool
	}{
		// 容差 2 秒（2000ms）：差 1800ms 成组。
		{name: "FileEntry 时长在容差内", msA: 1000, msB: 2800, wantDeleted: true},
		// 通过 Providers 注入时长，覆盖 provider 路径；差 2000ms 恰好等于容差，仍成组。
		{name: "Providers 时长在容差边界", useProvider: true, msA: 1000, msB: 3000, wantDeleted: true},
		// 差 2001ms 超出容差，不分组。
		{name: "超出容差不成组", msA: 1000, msB: 3001, wantDeleted: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Steps.AudioDedup = true
			cfg.AudioToleranceSec = 2

			providers := Providers{}
			a, b := fe("a.mp3"), fe("bb.mp3") // 保留路径最短的 a.mp3
			if c.useProvider {
				providers.AudioMsFn = func(e FileEntry) (int64, bool) {
					if e.Path == "a.mp3" {
						return c.msA, true
					}
					return c.msB, true
				}
			} else {
				a.AudioMs, a.HasAudio = c.msA, true
				b.AudioMs, b.HasAudio = c.msB, true
			}

			sp := stepByID(t, Plan(cfg, snap(a, b), providers), StepAudioDedup)
			if c.wantDeleted {
				assertActions(t, "音频去重", sp.Actions, []string{"delete|bb.mp3|"})
				if sp.Counters.AudioDeleted != 1 {
					t.Errorf("AudioDeleted = %d，期望 1", sp.Counters.AudioDeleted)
				}
				return
			}
			assertActions(t, "音频去重", sp.Actions, nil)
			if sp.Counters.AudioDeleted != 0 {
				t.Errorf("AudioDeleted = %d，期望 0", sp.Counters.AudioDeleted)
			}
		})
	}
}
