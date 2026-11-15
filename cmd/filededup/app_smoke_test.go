//go:build desktop

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 冒烟测试要真删文件、真整理，关闭授权门控以保证测试可重复。
func TestMain(m *testing.M) {
	enforceLicense = false
	os.Exit(m.Run())
}

// 临时冒烟测试：验证 UI 后端门面（App）与引擎的真实接线。
func TestAppSmoke(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "照片", "2024")
	dirB := filepath.Join(root, "备份")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	content := make([]byte, 64*1024)
	for i := range content {
		content[i] = byte(i % 251)
	}
	kept := filepath.Join(dirA, "海边日落.jpg")
	dup := filepath.Join(dirB, "海边日落 副本.jpg")
	for _, p := range []string{kept, dup} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(dup, old, old); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	id, err := app.StartScan(ScanRequest{
		Root:           root,
		KeepPolicy:     "pathShortest",
		ImageThreshold: 8,
		AudioTolerance: 2,
	})
	if err != nil {
		t.Fatalf("StartScan: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var st ScanState
	for time.Now().Before(deadline) {
		st, err = app.GetScanState(id)
		if err != nil {
			t.Fatalf("GetScanState: %v", err)
		}
		if st.Phase == "done" || st.Phase == "failed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st.Phase != "done" {
		t.Fatalf("扫描未完成: phase=%s err=%s", st.Phase, st.Error)
	}
	t.Logf("scan done: files=%d groups=%d dupFiles=%d reclaim=%d elapsed=%dms", st.Files, st.Groups, st.DuplicateFiles, st.Reclaimable, st.ElapsedMS)
	if st.Groups != 1 || st.DuplicateFiles != 1 {
		t.Fatalf("分组不符合预期: groups=%d dup=%d", st.Groups, st.DuplicateFiles)
	}

	page, err := app.GetGroups(id, 0, 100)
	if err != nil {
		t.Fatalf("GetGroups: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("组数量异常: total=%d items=%d", page.Total, len(page.Items))
	}
	g := page.Items[0]
	keepCount := 0
	keepPath, delPath := "", ""
	for _, m := range g.Members {
		if m.Keep {
			keepCount++
			keepPath = m.Path
		} else {
			delPath = m.Path
		}
	}
	if keepCount != 1 {
		t.Fatalf("保留项数量应为 1，实际 %d", keepCount)
	}
	kept, dup = keepPath, delPath
	t.Logf("keep=%s delete=%s", kept, dup)

	pv, err := app.PreviewPlan(id, "trash", nil)
	if err != nil {
		t.Fatalf("PreviewPlan: %v", err)
	}
	if pv.Total != 1 || len(pv.Samples) != 1 || pv.Samples[0].Src != dup {
		t.Fatalf("预览不符合预期: total=%d samples=%v", pv.Total, pv.Samples)
	}

	// 显式勾选：只删 dup 时与默认一致；把保留项也勾上必须被拒绝。
	pvSel, err := app.PreviewPlan(id, "trash", []string{dup})
	if err != nil {
		t.Fatalf("PreviewPlan 显式勾选: %v", err)
	}
	if pvSel.Total != 1 || pvSel.Groups != 1 || pvSel.Samples[0].Src != dup {
		t.Fatalf("显式勾选预览不符合预期: total=%d groups=%d samples=%v", pvSel.Total, pvSel.Groups, pvSel.Samples)
	}
	if _, err := app.PreviewPlan(id, "trash", []string{dup, kept}); err == nil {
		t.Fatal("勾选整组删除时不应生成计划")
	}

	if err := app.StartExecute(id, "permanent", nil, false); err == nil {
		t.Fatal("未确认时不应允许执行")
	}

	if err := app.StartExecute(id, "permanent", []string{dup}, true); err != nil {
		t.Fatalf("StartExecute: %v", err)
	}
	var es ExecuteState
	for time.Now().Before(deadline) {
		es, err = app.GetExecuteState(id)
		if err != nil {
			t.Fatalf("GetExecuteState: %v", err)
		}
		if es.Done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !es.Done || es.Applied != 1 || es.Failed != 0 {
		t.Fatalf("执行结果异常: done=%v applied=%d failed=%d err=%s", es.Done, es.Applied, es.Failed, es.Error)
	}
	if _, err := os.Stat(dup); !os.IsNotExist(err) {
		t.Fatalf("重复文件应已删除: %v", err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("保留文件不应被删除: %v", err)
	}
	t.Logf("execute done: applied=%d reclaimed=%d", es.Applied, es.Reclaimed)
}

// TestSettingsTolerance 覆盖设置文件的容错：缺键补默认、多余键不判损坏、坏 JSON 回退默认。
func TestSettingsTolerance(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("AppData", cfgDir)

	def, warn := LoadSettings()
	if warn != "" {
		t.Fatalf("首次加载不应有警告: %s", warn)
	}
	if def.SchemaVersion != settingsSchemaVersion || def.Scan.KeepPolicy != "pathShortest" {
		t.Fatalf("默认值异常: %+v", def)
	}

	path, err := settingsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	// 旧配置：缺 organize 段、scan 只有两个键，另有未知键。
	old := `{
  "schemaVersion": 1,
  "scan": {"keepPolicy": "newest", "imageThreshold": 12},
  "unknownTop": {"keep": true}
}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	got, warn := LoadSettings()
	if warn != "" {
		t.Fatalf("未知键不应判损坏: %s", warn)
	}
	if got.Scan.KeepPolicy != "newest" || got.Scan.ImageThreshold != 12 {
		t.Fatalf("已有键未生效: %+v", got.Scan)
	}
	if !got.Scan.SimilarImage || got.Scan.DeleteMode != "trash" {
		t.Fatalf("缺失键未补默认值: %+v", got.Scan)
	}
	if got.Organize.Mode != "category" || !got.Organize.Steps.Organize {
		t.Fatalf("缺失段落未用默认值: %+v", got.Organize)
	}

	// 保存后未知键应保留。
	got.Scan.DeleteMode = "permanent"
	if err := SaveSettings(got); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("保存结果不是合法 JSON: %v", err)
	}
	if _, ok := top["unknownTop"]; !ok {
		t.Fatal("保存后未知键被丢弃")
	}
	again, warn := LoadSettings()
	if warn != "" || again.Scan.DeleteMode != "permanent" {
		t.Fatalf("回读不一致: warn=%q settings=%+v", warn, again.Scan)
	}

	// 坏 JSON：回退默认并给出提示。
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken, warn := LoadSettings()
	if warn == "" {
		t.Fatal("坏 JSON 应给出提示")
	}
	if broken.Scan.KeepPolicy != "pathShortest" {
		t.Fatalf("坏 JSON 未回退默认值: %+v", broken)
	}
}

// TestOrganizeJob 覆盖整理器的试运行与真跑：试运行不落盘，真跑只删重复项。
func TestOrganizeJob(t *testing.T) {
	root := t.TempDir()
	keepPath := filepath.Join(root, "甲", "原图.txt")
	dupPath := filepath.Join(root, "乙", "原图 副本.txt")
	for _, p := range []string{keepPath, dupPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("organize smoke payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp()
	meta := app.GetOrganizeMeta()
	if len(meta.Standalone) != 9 || len(meta.Preview) != 9 {
		t.Fatalf("九步顺序异常: standalone=%d preview=%d", len(meta.Standalone), len(meta.Preview))
	}
	if meta.Standalone[0].ID == meta.Preview[0].ID {
		t.Fatal("两套顺序不应相同")
	}

	req := OrganizeRequest{
		Root:            root,
		DryRun:          true,
		Steps:           OrganizeSteps{ContentDedup: true},
		Mode:            "category",
		Conflict:        "rename",
		DeleteMode:      "permanent",
		KeepPolicy:      "pathShortest",
		ImageKeepPolicy: "largest",
		ImageThreshold:  8,
		AudioTolerance:  2,
		Strength:        1,
		Excludes:        []string{},
	}
	id, err := app.StartOrganize(req)
	if err != nil {
		t.Fatalf("StartOrganize: %v", err)
	}
	st := waitOrganize(t, app, id)
	if st.Phase != "done" || !st.DryRun || st.ActionTotal != 1 {
		t.Fatalf("试运行异常: phase=%s dryRun=%v actions=%d err=%s", st.Phase, st.DryRun, st.ActionTotal, st.Error)
	}
	for _, p := range []string{keepPath, dupPath} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("试运行不应改动文件: %s", p)
		}
	}

	req.DryRun = false
	id, err = app.StartOrganize(req)
	if err != nil {
		t.Fatalf("StartOrganize(真跑): %v", err)
	}
	st = waitOrganize(t, app, id)
	if st.Phase != "done" || st.Exec.Applied != 1 || st.Exec.Failed != 0 {
		t.Fatalf("真跑异常: phase=%s applied=%d failed=%d err=%s", st.Phase, st.Exec.Applied, st.Exec.Failed, st.Error)
	}
	if _, err := os.Stat(dupPath); !os.IsNotExist(err) {
		t.Fatalf("重复文件应已删除: %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("保留文件不应被删除: %v", err)
	}

	// 参数校验：关掉全部步骤应被拒绝。
	req.Steps = OrganizeSteps{}
	if _, err := app.StartOrganize(req); err == nil {
		t.Fatal("未启用步骤时应拒绝执行")
	}
}

func waitOrganize(t *testing.T, app *App, id string) OrganizeState {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st, err := app.GetOrganizeState(id)
		if err != nil {
			t.Fatalf("GetOrganizeState: %v", err)
		}
		if st.Phase != "running" {
			return st
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("整理任务超时: %s", id)
	return OrganizeState{}
}
