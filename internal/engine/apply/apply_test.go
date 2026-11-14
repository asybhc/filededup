package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filededup/internal/engine/model"
)

func snapshot(t *testing.T, path string) *model.Precheck {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Precheck{
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newPlan(actions ...model.Action) *model.Plan {
	p := &model.Plan{SchemaVersion: model.SchemaVersion, RequestID: "t", Source: model.PlanFromScan}
	for i := range actions {
		actions[i].Seq = i + 1
		if actions[i].Status == "" {
			actions[i].Status = model.StatusPlanned
		}
	}
	p.Actions = actions
	p.Summarize()
	return p
}

func TestExecuteDryRunDoesNotTouchFS(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "payload")
	p := newPlan(model.Action{
		Kind:     model.ActionDeletePermanent,
		Src:      path,
		Precheck: snapshot(t, path),
	})
	report, err := (&Executor{Trash: func(string) error {
		t.Fatal("演练不应调用回收站")
		return nil
	}}).Execute(context.Background(), p, Options{DryRun: true})
	if err != nil {
		t.Fatalf("演练失败：%v", err)
	}
	if report.DryRuns != 1 || report.Applied != 0 || report.Failed != 0 {
		t.Fatalf("报告计数不符：%+v", report)
	}
	if report.Actions[0].Status != model.StatusDryRun {
		t.Fatalf("状态=%q", report.Actions[0].Status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("演练改动了文件系统：%v", err)
	}
}

func TestExecuteTrashFailureIsSafe(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "payload")
	p := newPlan(model.Action{
		Kind:     model.ActionDeleteTrash,
		Src:      path,
		Precheck: snapshot(t, path),
	})
	boom := errors.New("拒绝访问")
	report, err := (&Executor{Trash: func(string) error { return boom }}).
		Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatalf("执行不应返回错误：%v", err)
	}
	if report.Failed != 1 || report.Applied != 0 {
		t.Fatalf("报告计数不符：%+v", report)
	}
	if report.Actions[0].Status != model.StatusFailed || !strings.Contains(report.Actions[0].Err, "拒绝访问") {
		t.Fatalf("动作状态不符：%+v", report.Actions[0])
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("失败后文件应保持原样：%v", err)
	}
}

func TestExecuteAbortsAfterFailureByDefault(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt", "aaa")
	b := writeFile(t, dir, "b.txt", "bbb")
	p := newPlan(
		model.Action{Kind: model.ActionDeleteTrash, Src: a, Precheck: snapshot(t, a)},
		model.Action{Kind: model.ActionDeleteTrash, Src: b, Precheck: snapshot(t, b)},
	)
	report, err := (&Executor{Trash: func(string) error { return errors.New("磁盘已满") }}).
		Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Skipped != 1 {
		t.Fatalf("报告计数不符：%+v", report)
	}
	if report.Actions[1].Status != model.StatusSkipped {
		t.Fatalf("后续动作应跳过，实际 %q", report.Actions[1].Status)
	}
	if _, err := os.Stat(b); err != nil {
		t.Fatalf("被跳过的文件不应改动：%v", err)
	}
}

func TestExecuteContinueOnError(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt", "aaa")
	b := writeFile(t, dir, "b.txt", "bbb")
	p := newPlan(
		model.Action{Kind: model.ActionDeleteTrash, Src: a, Precheck: snapshot(t, a)},
		model.Action{Kind: model.ActionDeletePermanent, Src: b, Precheck: snapshot(t, b)},
	)
	calls := 0
	report, err := (&Executor{Trash: func(string) error {
		calls++
		return errors.New("拒绝访问")
	}}).Execute(context.Background(), p, Options{ContinueOnError: true})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || report.Failed != 1 || report.Applied != 1 {
		t.Fatalf("报告计数不符：%+v calls=%d", report, calls)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("第二条应已永久删除：%v", err)
	}
}

func TestExecutePrecheckMismatchSkips(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "payload")
	pre := snapshot(t, path)
	if err := os.WriteFile(path, []byte("payload changed!"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := newPlan(model.Action{Kind: model.ActionDeletePermanent, Src: path, Precheck: pre})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 1 || report.Failed != 0 {
		t.Fatalf("预检不符应跳过：%+v", report)
	}
	if !strings.Contains(report.Actions[0].Err, "大小已变化") {
		t.Fatalf("错误信息=%q", report.Actions[0].Err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件不应被删除：%v", err)
	}
}

func TestExecuteMoveAndRename(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "a.txt", "payload")
	dst := filepath.Join(dir, "sub", "rename.txt")
	p := newPlan(model.Action{
		Kind:     model.ActionMove,
		Src:      src,
		Dst:      dst,
		Precheck: snapshot(t, src),
	})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil || report.Applied != 1 {
		t.Fatalf("移动失败：%v %+v", err, report)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("目标不存在：%v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("源仍存在")
	}
}

func TestExecuteMoveDestinationExistsFails(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "a.txt", "aaa")
	dst := writeFile(t, dir, "b.txt", "bbb")
	p := newPlan(model.Action{
		Kind:     model.ActionMove,
		Src:      src,
		Dst:      dst,
		Precheck: snapshot(t, src),
	})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 {
		t.Fatalf("目标已存在应失败：%+v", report)
	}
	if data, err := os.ReadFile(dst); err != nil || string(data) != "bbb" {
		t.Fatalf("目标文件被改动：%q %v", data, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("源文件不应移动：%v", err)
	}
}

func TestExecuteHardlinkMerges(t *testing.T) {
	dir := t.TempDir()
	keep := writeFile(t, dir, "keep.bin", "same")
	dup := writeFile(t, dir, "dup.bin", "same")
	p := newPlan(model.Action{
		Kind:     model.ActionHardlink,
		Src:      dup,
		Dst:      keep,
		Precheck: snapshot(t, dup),
	})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil || report.Applied != 1 {
		t.Fatalf("硬链接失败：%v %+v", err, report)
	}
	ki, err := os.Stat(keep)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dup)
	if err != nil {
		t.Fatalf("重复路径应保留为新链接：%v", err)
	}
	if !os.SameFile(ki, di) {
		t.Fatalf("两者未指向同一文件")
	}
	if _, err := os.Lstat(dup + ".fdedup-bak"); !os.IsNotExist(err) {
		t.Fatalf("备份文件未清理")
	}
}

func TestExecuteHardlinkRollbackWhenLinkFails(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "dup.bin", "same")
	p := newPlan(model.Action{
		Kind:     model.ActionHardlink,
		Src:      src,
		Dst:      filepath.Join(dir, "missing-keep.bin"),
		Precheck: snapshot(t, src),
	})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.RolledBack != 1 {
		t.Fatalf("应以回滚方式失败：%+v", report)
	}
	data, err := os.ReadFile(src)
	if err != nil || string(data) != "same" {
		t.Fatalf("回滚后源文件不符：%q %v", data, err)
	}
	if _, err := os.Lstat(src + ".fdedup-bak"); !os.IsNotExist(err) {
		t.Fatalf("备份未清理")
	}
}

func TestExecuteClearReadonlyAndMkdir(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "ro.txt", "x")
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "newdir")
	p := newPlan(
		model.Action{Kind: model.ActionClearReadonly, Src: path, Precheck: snapshot(t, path)},
		model.Action{Kind: model.ActionMkdir, Src: target},
		model.Action{Kind: model.ActionMkdir, Src: target},
	)
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied != 2 || report.Skipped != 1 {
		t.Fatalf("计数不符：%+v", report)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("只读属性未去除：%v", info.Mode())
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("目录未创建：%v", err)
	}
}

func TestExecuteJournalRecordsOutcomes(t *testing.T) {
	dir := t.TempDir()
	ok := writeFile(t, dir, "ok.txt", "1")
	bad := writeFile(t, dir, "bad.txt", "2")
	p := newPlan(
		model.Action{Kind: model.ActionDeletePermanent, Src: ok, Precheck: snapshot(t, ok)},
		model.Action{Kind: model.ActionDeleteTrash, Src: bad, Precheck: snapshot(t, bad)},
	)
	j := &fakeJournal{failAt: 2}
	report, err := (&Executor{Trash: func(string) error { return errors.New("占用中") }, Journal: j}).
		Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(j.actions) != 2 {
		t.Fatalf("操作清单条数=%d", len(j.actions))
	}
	if j.actions[0].Status != model.StatusApplied || j.actions[1].Status != model.StatusFailed {
		t.Fatalf("操作清单状态不符：%+v", j.actions)
	}
	if report.JournalError == "" {
		t.Fatalf("写入失败应记录 JournalError")
	}
}

func TestExecuteCancelled(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "a.txt", "x")
	p := newPlan(model.Action{Kind: model.ActionDeletePermanent, Src: path, Precheck: snapshot(t, path)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := (&Executor{}).Execute(ctx, p, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled，实际 %v", err)
	}
	if report.Skipped != 1 {
		t.Fatalf("取消后应跳过：%+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("取消不应删除文件：%v", err)
	}
}

func TestExecuteUnknownKindFails(t *testing.T) {
	p := newPlan(model.Action{Kind: model.ActionKind("unknown"), Src: "x"})
	report, err := (&Executor{}).Execute(context.Background(), p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || !strings.Contains(report.Actions[0].Err, "不支持的动作类型") {
		t.Fatalf("未知动作应失败：%+v", report.Actions[0])
	}
}

type fakeJournal struct {
	actions []model.Action
	failAt  int
}

func (f *fakeJournal) Record(a model.Action) error {
	f.actions = append(f.actions, a)
	if f.failAt > 0 && len(f.actions) >= f.failAt {
		return syscall.ENOSPC
	}
	return nil
}
