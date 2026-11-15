//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestDedupUndoRoundTrip 走完整链路：扫描 → 按计划把副本移入回收站 → 读到撤销记录 →
// 撤销后原文件回来且内容一致。只在 Windows 上跑（其他平台恢复明确不支持）。
func TestDedupUndoRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("回收站恢复仅在 Windows 实现")
	}
	// 把撤销记录与操作清单隔离到临时配置目录，避免污染真实用户数据。
	t.Setenv("AppData", t.TempDir())

	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	content := []byte("dedup-undo-roundtrip-" + time.Now().Format(time.RFC3339Nano))
	keep := filepath.Join(dirA, "keep.txt")
	dup := filepath.Join(dirB, "dup.txt")
	for _, p := range []string{keep, dup} {
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
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, serr := app.GetScanState(id)
		if serr != nil {
			t.Fatalf("GetScanState: %v", serr)
		}
		if st.Phase == "done" || st.Phase == "failed" {
			if st.Phase != "done" {
				t.Fatalf("扫描失败：%s", st.Error)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("扫描超时")
		}
		time.Sleep(50 * time.Millisecond)
	}

	before, err := app.GetDedupUndoInfo()
	if err != nil {
		t.Fatalf("GetDedupUndoInfo: %v", err)
	}
	if before.Available {
		t.Fatalf("尚未执行时不应有可撤销记录：%+v", before)
	}

	if err := app.StartExecute(id, "trash", nil, true); err != nil {
		t.Fatalf("StartExecute: %v", err)
	}
	var ex ExecuteState
	for {
		ex, err = app.GetExecuteState(id)
		if err != nil {
			t.Fatalf("GetExecuteState: %v", err)
		}
		if ex.Done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("执行超时")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ex.Failed != 0 || ex.Applied != 1 {
		t.Fatalf("执行结果异常：%+v", ex)
	}

	info, err := app.GetDedupUndoInfo()
	if err != nil {
		t.Fatalf("GetDedupUndoInfo: %v", err)
	}
	if !info.Available || info.Trash != 1 || len(info.Items) != 1 {
		t.Fatalf("撤销记录异常：%+v", info)
	}
	deleted := info.Items[0].Path
	if deleted != dup && deleted != keep {
		t.Fatalf("撤销明细不是组内成员：%+v", info.Items[0])
	}
	if info.Items[0].Dir != filepath.Dir(deleted) {
		t.Fatalf("撤销明细目录异常：%+v", info.Items[0])
	}
	if info.FinishedAt == "" {
		t.Fatalf("撤销记录缺少批次时间")
	}
	if _, err := os.Stat(deleted); !os.IsNotExist(err) {
		t.Fatalf("被删除的文件应已移入回收站：%v", err)
	}

	res, err := app.UndoLastDedup()
	if err != nil {
		t.Fatalf("UndoLastDedup: %v", err)
	}
	if res.Restored != 1 || res.Failed != 0 {
		t.Fatalf("撤销结果异常：%+v", res)
	}
	got, err := os.ReadFile(deleted)
	if err != nil {
		t.Fatalf("撤销后读取失败：%v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("撤销后内容不一致")
	}
	after, err := app.GetDedupUndoInfo()
	if err != nil {
		t.Fatalf("GetDedupUndoInfo: %v", err)
	}
	if after.Available {
		t.Fatalf("全部恢复后不应再留有可撤销记录：%+v", after)
	}
}
