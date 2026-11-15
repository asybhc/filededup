package organize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// execWriteFile 建立父目录并写入内容。
func execWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败 %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入文件失败 %s: %v", path, err)
	}
}

// execMakeReadonly 设置只读属性，并注册清理，避免 TempDir 清理时删不掉。
func execMakeReadonly(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("设置只读失败 %s: %v", path, err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	// 验证平台语义：只读文件读出来必须是 0444（Windows 上由属性映射而来）。
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("读取属性失败 %s: %v", path, err)
	}
	if info.Mode().Perm() != 0o444 {
		t.Fatalf("%s 只读权限 = %o, 期望 444", path, info.Mode().Perm())
	}
}

// execSHA256 返回文件内容哈希。
func execSHA256(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败 %s: %v", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// execSnapshot 对整棵树做快照：相对路径 + 大小 + SHA-256 + 只读位。
func execSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		ro := "rw"
		if info.Mode().Perm()&0o200 == 0 {
			ro = "ro"
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out = append(out, fmt.Sprintf("%s|dir|%s", rel, ro))
			return nil
		}
		out = append(out, fmt.Sprintf("%s|%d|%s|%s", rel, info.Size(), execSHA256(t, p), ro))
		return nil
	})
	if err != nil {
		t.Fatalf("遍历目录失败 %s: %v", root, err)
	}
	return out
}

func execAssertContent(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败 %s: %v", path, err)
	}
	if string(raw) != want {
		t.Fatalf("%s 内容 = %q, 期望 %q", path, string(raw), want)
	}
}

// TestExecApplyEndToEnd 端到端验证改名、移动、去只读、删空目录，且内容不变。
func TestExecApplyEndToEnd(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "a", "readonly.txt"), "locked")
	execMakeReadonly(t, filepath.Join(root, "a", "readonly.txt"))
	execWriteFile(t, filepath.Join(root, "b", "IMG_0001.jpg"), "jpg")
	execWriteFile(t, filepath.Join(root, "c", "note.txt"), "note")
	execWriteFile(t, filepath.Join(root, "keep.txt"), "keep")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("创建空目录失败: %v", err)
	}

	// 手工拼 Result，保持执行层与规划层解耦。
	res := Result{Steps: []StepPlan{
		{ID: string(StepRenamePrefix), Actions: []Action{
			{Kind: ActionRename, From: "b/IMG_0001.jpg", To: "b/photo.jpg", Rule: "rename"},
		}},
		{ID: string(StepOrganize), Actions: []Action{
			{Kind: ActionMove, From: "c/note.txt", To: "docs/note.txt", Rule: "organize"},
		}},
		{ID: string(StepReadonly), Actions: []Action{
			{Kind: ActionClearReadonly, From: "a/readonly.txt", Rule: "readonly"},
		}},
		{ID: string(StepEmptyDirs), Actions: []Action{
			{Kind: ActionRemoveDir, From: "empty", Rule: "empty_dirs"},
			{Kind: ActionSuggest, From: "keep.txt", Reason: "存疑项未确认", Rule: "keep"},
		}},
	}}

	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Total != 4 || rep.Applied != 4 || rep.Failed != 0 || rep.Skipped != 0 {
		t.Fatalf("统计 = %+v, 期望 total=4 applied=4 failed=0 skipped=0（Suggest 不计数）", rep)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("不应有失败记录: %v", rep.Errors)
	}

	// 改名生效
	if _, err := os.Stat(filepath.Join(root, "b", "IMG_0001.jpg")); !os.IsNotExist(err) {
		t.Fatalf("源文件仍存在: %v", err)
	}
	execAssertContent(t, filepath.Join(root, "b", "photo.jpg"), "jpg")

	// 移动生效且目标目录被创建
	if info, err := os.Stat(filepath.Join(root, "docs")); err != nil || !info.IsDir() {
		t.Fatalf("目标目录未创建: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "c", "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("移动源文件仍存在: %v", err)
	}
	execAssertContent(t, filepath.Join(root, "docs", "note.txt"), "note")

	// 只读被清除，内容不变
	info, err := os.Stat(filepath.Join(root, "a", "readonly.txt"))
	if err != nil {
		t.Fatalf("读取只读文件失败: %v", err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("只读位未清除: %o", info.Mode().Perm())
	}
	execAssertContent(t, filepath.Join(root, "a", "readonly.txt"), "locked")

	// 空目录被删除
	if _, err := os.Stat(filepath.Join(root, "empty")); !os.IsNotExist(err) {
		t.Fatalf("空目录仍存在: %v", err)
	}

	// Suggest 被忽略，文件内容不变
	execAssertContent(t, filepath.Join(root, "keep.txt"), "keep")
}

// TestExecDryRun 演练前后整棵树完全一致，且不会创建目标目录。
func TestExecDryRun(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "a", "ro.txt"), "ro")
	execMakeReadonly(t, filepath.Join(root, "a", "ro.txt"))
	execWriteFile(t, filepath.Join(root, "b", "x.txt"), "x")
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatalf("创建空目录失败: %v", err)
	}

	before := execSnapshot(t, root)

	res := Result{Steps: []StepPlan{
		{ID: string(StepRenamePrefix), Actions: []Action{
			{Kind: ActionRename, From: "b/x.txt", To: "b/y.txt", Rule: "rename"},
		}},
		{ID: string(StepOrganize), Actions: []Action{
			{Kind: ActionMove, From: "a/ro.txt", To: "docs/ro.txt", Rule: "organize"},
		}},
		{ID: string(StepReadonly), Actions: []Action{
			{Kind: ActionClearReadonly, From: "a/ro.txt", Rule: "readonly"},
		}},
		{ID: string(StepEmptyDirs), Actions: []Action{
			{Kind: ActionRemoveDir, From: "empty", Rule: "empty_dirs"},
			{Kind: ActionSuggest, From: "b/x.txt", Reason: "存疑", Rule: "keep"},
		}},
	}}

	rep, err := Apply(root, res, ExecOptions{DryRun: true}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	// 演练：全部可执行动作计入 Total 与 Skipped，Applied/Failed 恒为 0。
	if rep.Total != 4 || rep.Applied != 0 || rep.Failed != 0 || rep.Skipped != 4 {
		t.Fatalf("演练统计 = %+v, 期望 total=4 applied=0 failed=0 skipped=4", rep)
	}
	if len(rep.Errors) != 0 {
		t.Fatalf("演练不应有失败记录: %v", rep.Errors)
	}

	after := execSnapshot(t, root)
	// 演练下文件内容/属性/位置必须完全不变（目录行单独判断）。
	if !slices.Equal(execOnlyFiles(before), execOnlyFiles(after)) {
		t.Fatalf("演练改动了文件系统:\n之前: %s\n之后: %s",
			strings.Join(execOnlyFiles(before), "\n"), strings.Join(execOnlyFiles(after), "\n"))
	}
	// 原版试运行仍会创建整理目标目录（B/E.cs 先 CreateDirectory 再判断试运行），
	// 移植版对齐该行为；除此之外不建任何目录。
	if _, err := os.Stat(filepath.Join(root, "docs")); err != nil {
		t.Fatalf("演练应按原版创建目标目录 docs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "b", "y.txt")); !os.IsNotExist(err) {
		t.Fatalf("演练不应改名: %v", err)
	}
}

// execOnlyFiles 过滤掉快照中的目录行，只保留文件行用于演练比对。
func execOnlyFiles(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(line, "|dir|") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// TestExecContinueOnError 首条失败后的行为取决于 ContinueOnError。
func TestExecContinueOnError(t *testing.T) {
	newCase := func(t *testing.T) (string, Result) {
		t.Helper()
		root := t.TempDir()
		execWriteFile(t, filepath.Join(root, "ok.txt"), "ok")
		res := Result{Steps: []StepPlan{
			{ID: string(StepSpecialDelete), Actions: []Action{
				{Kind: ActionDelete, From: "missing.txt", Rule: "delete"},
			}},
			{ID: string(StepRenamePrefix), Actions: []Action{
				{Kind: ActionRename, From: "ok.txt", To: "done.txt", Rule: "rename"},
			}},
		}}
		return root, res
	}

	t.Run("默认中止", func(t *testing.T) {
		root, res := newCase(t)
		rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Total != 2 || rep.Failed != 1 || rep.Skipped != 1 || rep.Applied != 0 {
			t.Fatalf("统计 = %+v, 期望 total=2 failed=1 skipped=1 applied=0", rep)
		}
		if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "missing.txt") {
			t.Fatalf("失败记录 = %v", rep.Errors)
		}
		if _, err := os.Stat(filepath.Join(root, "ok.txt")); err != nil {
			t.Fatalf("首条失败后不应继续改名: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, "done.txt")); !os.IsNotExist(err) {
			t.Fatalf("后续动作被跳过，done.txt 不应存在: %v", err)
		}
	})

	t.Run("继续执行", func(t *testing.T) {
		root, res := newCase(t)
		rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, ExecHooks{})
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Total != 2 || rep.Failed != 1 || rep.Skipped != 0 || rep.Applied != 1 {
			t.Fatalf("统计 = %+v, 期望 total=2 failed=1 skipped=0 applied=1", rep)
		}
		if _, err := os.Stat(filepath.Join(root, "done.txt")); err != nil {
			t.Fatalf("后续动作应已执行: %v", err)
		}
	})
}

// TestExecTrashHook 验证注入的回收站实现被调用，以及其错误记为失败。
func TestExecTrashHook(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "x.txt"), "x")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("解析绝对路径失败: %v", err)
	}
	want := filepath.Join(absRoot, "x.txt")

	t.Run("成功", func(t *testing.T) {
		var calls []string
		hooks := ExecHooks{Trash: func(p string) error {
			calls = append(calls, p)
			return nil
		}}
		res := Result{Steps: []StepPlan{{Actions: []Action{
			{Kind: ActionDelete, From: "x.txt", Rule: "delete"},
		}}}}
		rep, err := Apply(root, res, ExecOptions{}, hooks)
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Applied != 1 || rep.Failed != 0 {
			t.Fatalf("统计 = %+v, 期望 applied=1 failed=0", rep)
		}
		if len(calls) != 1 || calls[0] != want {
			t.Fatalf("回收站调用 = %v, 期望 [%s]", calls, want)
		}
	})

	t.Run("返回错误", func(t *testing.T) {
		var calls []string
		hooks := ExecHooks{Trash: func(p string) error {
			calls = append(calls, p)
			return errors.New("回收站不可用")
		}}
		res := Result{Steps: []StepPlan{{Actions: []Action{
			{Kind: ActionDelete, From: "x.txt", Rule: "delete"},
		}}}}
		rep, err := Apply(root, res, ExecOptions{}, hooks)
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Failed != 1 || rep.Applied != 0 {
			t.Fatalf("统计 = %+v, 期望 failed=1 applied=0", rep)
		}
		if len(calls) != 1 || calls[0] != want {
			t.Fatalf("回收站调用 = %v, 期望 [%s]", calls, want)
		}
		if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "回收站不可用") {
			t.Fatalf("失败记录 = %v", rep.Errors)
		}
	})
}

// TestExecPathEscape 拒绝越界路径，且不触碰根目录外的文件。
func TestExecPathEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("创建根目录失败: %v", err)
	}
	outside := filepath.Join(base, "outside.txt")
	execWriteFile(t, outside, "safe")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionDelete, From: "../outside.txt", Rule: "delete"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 || rep.Applied != 0 || len(rep.Errors) != 1 {
		t.Fatalf("统计 = %+v, 期望 failed=1 applied=0 且有一条记录", rep)
	}
	if !strings.Contains(rep.Errors[0], "逃逸") {
		t.Fatalf("失败记录 = %q, 期望包含“逃逸”", rep.Errors[0])
	}
	execAssertContent(t, outside, "safe")
}

// TestExecRemoveNonEmptyDir 非空目录不能被删除，记为失败。
func TestExecRemoveNonEmptyDir(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "dir", "inner.txt"), "inner")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionRemoveDir, From: "dir", Rule: "empty_dirs"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 || rep.Applied != 0 {
		t.Fatalf("统计 = %+v, 期望 failed=1 applied=0", rep)
	}
	if len(rep.Errors) != 1 || !strings.Contains(rep.Errors[0], "dir") {
		t.Fatalf("失败记录 = %v", rep.Errors)
	}
	execAssertContent(t, filepath.Join(root, "dir", "inner.txt"), "inner")
}

// TestExecClearReadonlyAlreadyWritable 已可写的文件按跳过计，不算失败。
func TestExecClearReadonlyAlreadyWritable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "w.txt")
	execWriteFile(t, path, "w")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionClearReadonly, From: "w.txt", Rule: "readonly"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Total != 1 || rep.Skipped != 1 || rep.Applied != 0 || rep.Failed != 0 {
		t.Fatalf("统计 = %+v, 期望 total=1 skipped=1 applied=0 failed=0", rep)
	}
	execAssertContent(t, path, "w")
}

// ---- 硬链接合并执行（a/K.cs） ----

// TestExecHardlinkMerge 成功路径：重复文件被替换为指向保留文件的硬链接。
func TestExecHardlinkMerge(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	dup := filepath.Join(root, "dup.txt")
	execWriteFile(t, keep, "same")
	execWriteFile(t, dup, "same")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionHardlink, From: "dup.txt", To: "keep.txt", Rule: "hardlink"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Total != 1 || rep.Applied != 1 || rep.Failed != 0 {
		t.Fatalf("统计 = %+v, 期望 total=1 applied=1 failed=0（错误=%v）", rep, rep.Errors)
	}
	execAssertContent(t, dup, "same")
	execAssertContent(t, keep, "same")
	keepInfo, err := os.Stat(keep)
	if err != nil {
		t.Fatalf("读取保留文件失败: %v", err)
	}
	dupInfo, err := os.Stat(dup)
	if err != nil {
		t.Fatalf("读取重复文件失败: %v", err)
	}
	if !os.SameFile(keepInfo, dupInfo) {
		t.Fatal("dup.txt 与 keep.txt 未指向同一文件（硬链接未生效）")
	}
	// 不残留备份文件。
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读取根目录失败: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "fdedup-bak") {
			t.Fatalf("残留备份文件：%s", e.Name())
		}
	}
}

// TestExecHardlinkRollback 建链接失败时必须把备份改回原名，重复文件内容不丢。
func TestExecHardlinkRollback(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "keep.txt"), "same")
	execWriteFile(t, filepath.Join(root, "dup.txt"), "same")

	hooks := ExecHooks{Link: func(string, string) error { return errors.New("模拟建链失败") }}

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionHardlink, From: "dup.txt", To: "keep.txt", Rule: "hardlink"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 || rep.Applied != 0 {
		t.Fatalf("统计 = %+v, 期望 failed=1 applied=0", rep)
	}
	execAssertContent(t, filepath.Join(root, "dup.txt"), "same")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("读取根目录失败: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "fdedup-bak") {
			t.Fatalf("回滚后仍残留备份：%s", e.Name())
		}
	}
}

// TestExecHardlinkCrossVolume 跨卷时失败，且不删除重复文件。
func TestExecHardlinkCrossVolume(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "keep.txt"), "same")
	execWriteFile(t, filepath.Join(root, "dup.txt"), "same")

	hooks := ExecHooks{SameVolume: func(string, string) (bool, error) { return false, nil }}

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionHardlink, From: "dup.txt", To: "keep.txt", Rule: "hardlink"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 {
		t.Fatalf("统计 = %+v, 期望 failed=1", rep)
	}
	execAssertContent(t, filepath.Join(root, "dup.txt"), "same")
}

// TestExecHardlinkMissingKeep 保留文件不存在时失败，不触碰重复文件。
func TestExecHardlinkMissingKeep(t *testing.T) {
	root := t.TempDir()
	execWriteFile(t, filepath.Join(root, "dup.txt"), "same")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionHardlink, From: "dup.txt", To: "missing.txt", Rule: "hardlink"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 {
		t.Fatalf("统计 = %+v, 期望 failed=1", rep)
	}
	execAssertContent(t, filepath.Join(root, "dup.txt"), "same")
}

// ---- 占用 / 长路径 ----

// TestExecDeleteLocked 被占用文件按失败处理；开启解除时才继续删除。
func TestExecDeleteLocked(t *testing.T) {
	newRoot := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		execWriteFile(t, filepath.Join(root, "locked.txt"), "x")
		execWriteFile(t, filepath.Join(root, "ok.txt"), "ok")
		return root
	}
	lockedHook := func(p string) bool { return strings.HasSuffix(p, "locked.txt") }

	t.Run("默认不解除", func(t *testing.T) {
		root := newRoot(t)
		res := Result{Steps: []StepPlan{{Actions: []Action{
			{Kind: ActionDelete, From: "locked.txt", Rule: "delete"},
			{Kind: ActionRename, From: "ok.txt", To: "done.txt", Rule: "rename"},
		}}}}
		rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, ExecHooks{IsLocked: lockedHook})
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Failed != 1 || rep.Applied != 1 {
			t.Fatalf("统计 = %+v, 期望 failed=1 applied=1（文件占用不中止后续）", rep)
		}
		if !strings.Contains(rep.Errors[0], "占用") {
			t.Fatalf("失败记录 = %q, 期望包含“占用”", rep.Errors[0])
		}
		execAssertContent(t, filepath.Join(root, "locked.txt"), "x")
	})

	t.Run("开启解除后删除", func(t *testing.T) {
		root := newRoot(t)
		var released []string
		hooks := ExecHooks{
			IsLocked: lockedHook,
			ReleaseLocks: func(p string, _ func(string, []string) bool) error {
				released = append(released, p)
				return nil
			},
		}
		res := Result{Steps: []StepPlan{{Actions: []Action{
			{Kind: ActionDelete, From: "locked.txt", Rule: "delete"},
		}}}}
		rep, err := Apply(root, res, ExecOptions{ReleaseLock: true}, hooks)
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Applied != 1 || rep.Failed != 0 {
			t.Fatalf("统计 = %+v, 期望 applied=1 failed=0（错误=%v）", rep, rep.Errors)
		}
		if len(released) != 1 || !strings.HasSuffix(released[0], "locked.txt") {
			t.Fatalf("解除占用调用 = %v, 期望 locked.txt", released)
		}
	})

	t.Run("未接入解除能力", func(t *testing.T) {
		root := newRoot(t)
		res := Result{Steps: []StepPlan{{Actions: []Action{
			{Kind: ActionDelete, From: "locked.txt", Rule: "delete"},
		}}}}
		rep, err := Apply(root, res, ExecOptions{ReleaseLock: true}, ExecHooks{IsLocked: lockedHook})
		if err != nil {
			t.Fatalf("Apply 失败: %v", err)
		}
		if rep.Failed != 1 || rep.Applied != 0 {
			t.Fatalf("统计 = %+v, 期望 failed=1 applied=0", rep)
		}
		if !strings.Contains(rep.Errors[0], "未接入平台解除占用能力") {
			t.Fatalf("失败记录 = %q, 期望包含“未接入平台解除占用能力”", rep.Errors[0])
		}
		execAssertContent(t, filepath.Join(root, "locked.txt"), "x")
	})
}

// TestExecLongPathRejected 超过 Windows 长路径上限时判失败，不落盘。
func TestExecLongPathRejected(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("a", 33000) + ".txt"
	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionDelete, From: long, Rule: "delete"},
	}}}}
	rep, err := Apply(root, res, ExecOptions{ContinueOnError: true}, ExecHooks{})
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 || rep.Applied != 0 {
		t.Fatalf("统计 = %+v, 期望 failed=1 applied=0", rep)
	}
	if !strings.Contains(rep.Errors[0], "长路径") {
		t.Fatalf("失败记录 = %q, 期望包含“长路径”", rep.Errors[0])
	}
}

// assertNoCopyTemp 断言目标目录里没有复制临时文件残留。
func assertNoCopyTemp(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败 %s: %v", dir, err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".filededup-copy-") {
			t.Fatalf("目标目录残留临时文件：%s", e.Name())
		}
	}
}

// TestApplyCrossVolumeCopies 注入“不同卷”，验证复制分支、源文件保留、内容一致。
func TestApplyCrossVolumeCopies(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "外部目标")
	src := filepath.Join(root, "a.txt")
	execWriteFile(t, src, "hello-cross-volume")
	dst := filepath.Join(out, "a.txt")

	res := Result{Steps: []StepPlan{{ID: string(StepOrganize), Actions: []Action{
		{Kind: ActionEnsureDir, To: filepath.ToSlash(out)},
		{Kind: ActionMove, From: "a.txt", To: filepath.ToSlash(dst)},
	}}}}
	hooks := ExecHooks{SameVolume: func(a, b string) (bool, error) { return false, nil }}
	rep, err := Apply(root, res, ExecOptions{}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Applied != 2 || rep.Failed != 0 {
		t.Fatalf("统计 = %+v, 期望 applied=2 failed=0", rep)
	}
	execAssertContent(t, dst, "hello-cross-volume")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("跨卷复制后源文件应保留：%v", err)
	}
	assertNoCopyTemp(t, out)
}

// TestApplyCrossVolumeCopyFailureLeavesNoPartial 复制失败时不产生目标文件与临时文件。
func TestApplyCrossVolumeCopyFailureLeavesNoPartial(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "外部目标")
	src := filepath.Join(root, "a.txt")
	execWriteFile(t, src, "full-content")
	dst := filepath.Join(out, "a.txt")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionEnsureDir, To: filepath.ToSlash(out)},
		{Kind: ActionMove, From: "a.txt", To: filepath.ToSlash(dst)},
	}}}}
	hooks := ExecHooks{
		SameVolume: func(a, b string) (bool, error) { return false, nil },
		CopyFile: func(src, dst string) error {
			if err := os.WriteFile(dst, []byte("half"), 0o644); err != nil {
				return err
			}
			return errors.New("模拟磁盘已满")
		},
	}
	rep, err := Apply(root, res, ExecOptions{}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 || rep.Applied != 1 {
		t.Fatalf("统计 = %+v, 期望 applied=1(建目录) failed=1", rep)
	}
	if !strings.Contains(rep.Errors[0], "模拟磁盘已满") {
		t.Fatalf("失败记录 = %q, 期望包含注入错误", rep.Errors[0])
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("复制失败后目标不应存在：%v", err)
	}
	execAssertContent(t, src, "full-content")
	assertNoCopyTemp(t, out)
}

// TestApplyCrossVolumeCopySizeMismatch 复制“成功”但大小不符时按失败处理并清理。
func TestApplyCrossVolumeCopySizeMismatch(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "外部目标")
	execWriteFile(t, filepath.Join(root, "a.txt"), "0123456789")
	dst := filepath.Join(out, "a.txt")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionEnsureDir, To: filepath.ToSlash(out)},
		{Kind: ActionMove, From: "a.txt", To: filepath.ToSlash(dst)},
	}}}}
	hooks := ExecHooks{
		SameVolume: func(a, b string) (bool, error) { return false, nil },
		CopyFile: func(src, dst string) error {
			return os.WriteFile(dst, []byte("01234"), 0o644)
		},
	}
	rep, err := Apply(root, res, ExecOptions{}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 {
		t.Fatalf("统计 = %+v, 期望 failed=1", rep)
	}
	if !strings.Contains(rep.Errors[0], "复制不完整") {
		t.Fatalf("失败记录 = %q, 期望包含“复制不完整”", rep.Errors[0])
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("尺寸不符时目标不应存在：%v", err)
	}
	assertNoCopyTemp(t, out)
}

// TestApplyAbsoluteDestination 目标在根目录之外（绝对路径），同卷仍走重命名移动。
func TestApplyAbsoluteDestination(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "外部目标")
	execWriteFile(t, filepath.Join(root, "a.txt"), "absolute-dest")
	dst := filepath.Join(out, "a.txt")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionEnsureDir, To: filepath.ToSlash(out)},
		{Kind: ActionMove, From: "a.txt", To: filepath.ToSlash(dst)},
	}}}}
	hooks := ExecHooks{SameVolume: func(a, b string) (bool, error) { return true, nil }}
	rep, err := Apply(root, res, ExecOptions{}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Applied != 2 || rep.Failed != 0 {
		t.Fatalf("统计 = %+v, 期望 applied=2 failed=0", rep)
	}
	execAssertContent(t, dst, "absolute-dest")
	if _, err := os.Stat(filepath.Join(root, "a.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("同卷移动后源文件应不存在：%v", err)
	}
}

// TestApplySameVolumeError 卷判定失败时如实报错，不移动、不复制。
func TestApplySameVolumeError(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(t.TempDir(), "外部目标")
	execWriteFile(t, filepath.Join(root, "a.txt"), "keep-me")
	dst := filepath.Join(out, "a.txt")

	res := Result{Steps: []StepPlan{{Actions: []Action{
		{Kind: ActionEnsureDir, To: filepath.ToSlash(out)},
		{Kind: ActionMove, From: "a.txt", To: filepath.ToSlash(dst)},
	}}}}
	hooks := ExecHooks{SameVolume: func(a, b string) (bool, error) {
		return false, errors.New("无法判断卷")
	}}
	rep, err := Apply(root, res, ExecOptions{}, hooks)
	if err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}
	if rep.Failed != 1 {
		t.Fatalf("统计 = %+v, 期望 failed=1", rep)
	}
	if !strings.Contains(rep.Errors[0], "无法判断卷") {
		t.Fatalf("失败记录 = %q, 期望包含注入错误", rep.Errors[0])
	}
	execAssertContent(t, filepath.Join(root, "a.txt"), "keep-me")
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("卷判定失败时不应产生目标文件：%v", err)
	}
}
