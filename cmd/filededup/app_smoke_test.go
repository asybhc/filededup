//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	pv, err := app.PreviewPlan(id, "trash")
	if err != nil {
		t.Fatalf("PreviewPlan: %v", err)
	}
	if pv.Total != 1 || len(pv.Samples) != 1 || pv.Samples[0].Src != dup {
		t.Fatalf("预览不符合预期: total=%d samples=%v", pv.Total, pv.Samples)
	}

	if err := app.StartExecute(id, "permanent", false); err == nil {
		t.Fatal("未确认时不应允许执行")
	}

	if err := app.StartExecute(id, "permanent", true); err != nil {
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
