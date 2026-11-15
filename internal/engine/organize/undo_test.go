package organize

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPlanSuspectsStructuredAndConfirmed 验证存疑项会结构化输出，且只有确认键才进入删除动作。
func TestPlanSuspectsStructuredAndConfirmed(t *testing.T) {
	snap := Snapshot{
		Root:  "root",
		Files: []FileEntry{{Path: "quarTerlyReport.txt"}},
	}
	cfg := DefaultConfig()
	cfg.Steps = StepSwitches{SpecialDelete: true}
	cfg.Strength = 1
	cfg.LocalWhitelist = true

	first := Plan(cfg, snap, Providers{})
	var found *Suspect
	for _, sp := range first.Steps {
		if sp.ID != string(StepSpecialDelete) {
			continue
		}
		if len(sp.Suspects) != 1 {
			t.Fatalf("存疑项数量 = %d, 期望 1（%+v）", len(sp.Suspects), sp.Suspects)
		}
		found = &sp.Suspects[0]
	}
	if found == nil {
		t.Fatal("未找到特殊删除步骤")
	}
	if found.Path != "quarTerlyReport.txt" {
		t.Fatalf("存疑项路径 = %q", found.Path)
	}
	if found.Reason == "" || found.Suggestion == "" {
		t.Fatalf("存疑项缺少原因/建议：%+v", *found)
	}
	if first.Counters.SuspectsKept != 1 || first.Counters.SpecialDeleted != 0 {
		t.Fatalf("未确认时计数 = %+v，期望 suspectsKept=1 specialDeleted=0", first.Counters)
	}

	cfg.ConfirmedKeys = map[string]bool{Fold(found.Path): true}
	cfg.AutoDeleteConfirmed = true
	second := Plan(cfg, snap, Providers{})
	for _, sp := range second.Steps {
		if sp.ID != string(StepSpecialDelete) {
			continue
		}
		if len(sp.Suspects) != 0 {
			t.Fatalf("确认后不应再列为待确认项：%+v", sp.Suspects)
		}
	}
	if second.Counters.SpecialDeleted != 1 || second.Counters.SuspectsKept != 0 {
		t.Fatalf("确认后计数 = %+v，期望 specialDeleted=1 suspectsKept=0", second.Counters)
	}
}

// TestUndoRestoresFilesystem 验证改名/移动/只读/新建目录/空目录都能回滚。
func TestUndoRestoresFilesystem(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "a", "x.txt"), "x")
	execWriteFile(t, filepath.Join(root, "ro.txt"), "ro")
	execMakeReadonly(t, filepath.Join(root, "ro.txt"))
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("创建空目录失败: %v", err)
	}

	res := Result{Steps: []StepPlan{
		{ID: string(StepRenamePrefix), Actions: []Action{
			{Kind: ActionRename, From: "a/x.txt", To: "a/y.txt", Rule: "rename"},
		}},
		{ID: string(StepOrganize), Actions: []Action{
			{Kind: ActionMove, From: "ro.txt", To: "docs/ro.txt", Rule: "organize"},
		}},
		{ID: string(StepReadonly), Actions: []Action{
			{Kind: ActionClearReadonly, From: "docs/ro.txt", Rule: "readonly"},
		}},
		{ID: string(StepEmptyDirs), Actions: []Action{
			{Kind: ActionRemoveDir, From: "empty", Rule: "empty_dirs"},
		}},
	}}

	var records []UndoRecord
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{Undo: func(r UndoRecord) {
		records = append(records, r)
	}})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Applied != 4 || rep.Failed != 0 {
		t.Fatalf("执行统计 = %+v，期望 applied=4 failed=0（错误=%v）", rep, rep.Errors)
	}
	if len(records) == 0 {
		t.Fatal("未记录任何撤销动作")
	}

	undo, err := Undo(root, records, ExecOptions{})
	if err != nil {
		t.Fatalf("Undo 失败: %v", err)
	}
	if undo.Failed != 0 || undo.Unrestorable != 0 {
		t.Fatalf("撤销统计 = %+v，期望 failed=0 unrestorable=0", undo)
	}
	if undo.Restored != len(records) {
		t.Fatalf("恢复数 = %d，期望 %d（记录=%+v）", undo.Restored, len(records), records)
	}

	execAssertContent(t, filepath.Join(root, "a", "x.txt"), "x")
	if _, err := os.Stat(filepath.Join(root, "a", "y.txt")); !os.IsNotExist(err) {
		t.Fatalf("改名后的文件仍存在: %v", err)
	}
	execAssertContent(t, filepath.Join(root, "ro.txt"), "ro")
	info, err := os.Stat(filepath.Join(root, "ro.txt"))
	if err != nil {
		t.Fatalf("读取还原文件失败: %v", err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("只读属性未恢复: %o", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(root, "docs")); !os.IsNotExist(err) {
		t.Fatalf("执行时新建的 docs 目录未删除: %v", err)
	}
	fi, err := os.Stat(filepath.Join(root, "empty"))
	if err != nil || !fi.IsDir() {
		t.Fatalf("被清理的空目录未恢复: err=%v", err)
	}
}

// TestUndoReportsUnrestorable 删除动作无法还原时必须如实计数，而不是假装成功。
func TestUndoReportsUnrestorable(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "gone.txt"), "x")

	res := Result{Steps: []StepPlan{{ID: string(StepSpecialDelete), Actions: []Action{
		{Kind: ActionDelete, From: "gone.txt", Rule: "delete"},
	}}}}
	var records []UndoRecord
	if _, err := Apply(root, res, ExecOptions{}, ExecHooks{Undo: func(r UndoRecord) {
		records = append(records, r)
	}}); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	undo, err := Undo(root, records, ExecOptions{})
	if err != nil {
		t.Fatalf("Undo 失败: %v", err)
	}
	if undo.Unrestorable != 1 || undo.Restored != 0 {
		t.Fatalf("撤销统计 = %+v，期望 unrestorable=1 restored=0", undo)
	}
	if _, err := os.Stat(filepath.Join(root, "gone.txt")); !os.IsNotExist(err) {
		t.Fatalf("删除的文件不应被假装恢复: %v", err)
	}
}

// TestUndoRestoreModeNoop 执行时文件已可写则不应产生恢复权限记录。
func TestUndoRestoreModeNoop(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "w.txt"), "w")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionClearReadonly, From: "w.txt", Rule: "readonly"},
	}}}}
	var records []UndoRecord
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{Undo: func(r UndoRecord) {
		records = append(records, r)
	}})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Skipped != 1 {
		t.Fatalf("统计 = %+v，期望 skipped=1", rep)
	}
	if len(records) != 0 {
		t.Fatalf("已可写文件不应记录撤销动作：%+v", records)
	}
}
