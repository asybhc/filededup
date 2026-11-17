package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/engine/model"
)

func writeApplyPlan(t *testing.T, p model.Plan) string {
	t.Helper()
	p.SchemaVersion = model.SchemaVersion
	if p.RequestID == "" {
		p.RequestID = "test-plan"
	}
	if p.CreatedAt == "" {
		p.CreatedAt = "2026-01-01T00:00:00Z"
	}
	if p.Source == "" {
		p.Source = model.PlanFromScan
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化计划失败：%v", err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("写入计划失败：%v", err)
	}
	return path
}

func clearReadonlyAction(seq int, src string) model.Action {
	return model.Action{
		Seq:      seq,
		Kind:     model.ActionClearReadonly,
		Src:      src,
		Rollback: model.RollbackRestoreAttr,
		Status:   model.StatusPlanned,
	}
}

// TestApplyRootGuardAllowsInsideRoot 根内动作正常执行。
func TestApplyRootGuardAllowsInsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	sub := filepath.Join(root, "empty")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := model.Plan{
		Roots: []string{root},
		Actions: []model.Action{{
			Seq:      1,
			Kind:     model.ActionRemoveDir,
			Src:      sub,
			Rollback: model.RollbackRemoveDir,
			Status:   model.StatusPlanned,
		}},
	}
	planPath := writeApplyPlan(t, p)
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 0 {
		t.Fatalf("根内动作应执行成功，退出码 %d", code)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("空目录未被删除：%v", err)
	}
}

// TestApplyRootGuardRejectsDotDot 含 .. 逃出根目录的动作被整批拒绝，磁盘无改动。
func TestApplyRootGuardRejectsDotDot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "..", "outside", "victim.txt")
	p := model.Plan{
		Roots:   []string{root},
		Actions: []model.Action{clearReadonlyAction(1, escape)},
	}
	planPath := writeApplyPlan(t, p)
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 2 {
		t.Fatalf("越界动作应被拒绝（退出码 2），实际 %d", code)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep-me" {
		t.Fatalf("越界目标被改动：content=%q err=%v", got, err)
	}
}

// TestApplyRootGuardRejectsAbsoluteOutside 绝对路径越界被拒。
func TestApplyRootGuardRejectsAbsoluteOutside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := model.Plan{
		Roots:   []string{root},
		Actions: []model.Action{clearReadonlyAction(1, victim)},
	}
	planPath := writeApplyPlan(t, p)
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 2 {
		t.Fatalf("绝对路径越界应被拒绝（退出码 2），实际 %d", code)
	}
	if got, err := os.ReadFile(victim); err != nil || string(got) != "keep-me" {
		t.Fatalf("越界目标被改动：content=%q err=%v", got, err)
	}
}

// TestApplyRootGuardRejectsSymlinkOutside 根内符号链接指向根外时按真实路径拒绝。
func TestApplyRootGuardRejectsSymlinkOutside(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境无法创建符号链接，跳过：%v", err)
	}
	p := model.Plan{
		Roots:   []string{root},
		Actions: []model.Action{clearReadonlyAction(1, filepath.Join(link, "victim.txt"))},
	}
	planPath := writeApplyPlan(t, p)
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 2 {
		t.Fatalf("符号链接指向根外应被拒绝（退出码 2），实际 %d", code)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != "keep-me" {
		t.Fatalf("越界目标被改动：content=%q err=%v", got, err)
	}
}

// TestApplyRootGuardRequiresRoots 计划未声明根目录且未给 -root 时拒绝；
// -root 覆盖后根内动作可正常执行。
func TestApplyRootGuardRequiresRoots(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	sub := filepath.Join(root, "empty")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := model.Plan{
		Actions: []model.Action{{
			Seq:      1,
			Kind:     model.ActionRemoveDir,
			Src:      sub,
			Rollback: model.RollbackRemoveDir,
			Status:   model.StatusPlanned,
		}},
	}
	planPath := writeApplyPlan(t, p)

	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 2 {
		t.Fatalf("缺少根目录时应拒绝（退出码 2），实际 %d", code)
	}
	if _, err := os.Stat(sub); err != nil {
		t.Fatalf("拒绝时不应改动磁盘：%v", err)
	}
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal", "-root", root}); code != 0 {
		t.Fatalf("-root 覆盖后根内动作应执行成功，实际 %d", code)
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatalf("空目录未被删除：%v", err)
	}
}

// TestApplyRootGuardReportsOffendingAction 错误信息必须指出越界的是第几条动作。
func TestApplyRootGuardReportsOffendingAction(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "ok.txt")
	if err := os.WriteFile(inside, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := model.Plan{
		Roots: []string{root},
		Actions: []model.Action{
			clearReadonlyAction(1, inside),
			clearReadonlyAction(2, victim),
		},
	}
	planPath := writeApplyPlan(t, p)
	// 直接调用守卫，确错误文案包含序号与路径。
	if err := validateActionRoots(&p, nil); err == nil {
		t.Fatal("越界计划应返回错误")
	} else if !strings.Contains(err.Error(), "第 2 条动作") || !strings.Contains(err.Error(), filepath.Base(victim)) {
		t.Fatalf("错误信息未指出越界动作：%v", err)
	}
	// 整批拒绝：第 1 条也不应执行。
	if code := runApply([]string{"-plan", planPath, "-yes", "-no-journal"}); code != 2 {
		t.Fatalf("越界计划应整批拒绝，实际退出码 %d", code)
	}
}
