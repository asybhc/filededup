//go:build desktop

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExportResultsHTMLAndCSV 验证 HTML/CSV 都包含分组、成员、大小、保留策略与保留项、
// 可回收合计、扫描参数与扫描时间。
func TestExportResultsHTMLAndCSV(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("dedup-export-roundtrip-" + time.Now().Format(time.RFC3339Nano))
	keep := filepath.Join(root, "keep.txt")
	dup := filepath.Join(sub, "dup.txt")
	for _, p := range []string{keep, dup} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
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
		if st.Phase == "done" {
			break
		}
		if st.Phase == "failed" {
			t.Fatalf("扫描失败：%s", st.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("扫描超时")
		}
		time.Sleep(50 * time.Millisecond)
	}

	dir := t.TempDir()
	htmlRes, err := app.exportResults(id, "html", dir)
	if err != nil {
		t.Fatalf("导出 HTML: %v", err)
	}
	csvRes, err := app.exportResults(id, "csv", dir)
	if err != nil {
		t.Fatalf("导出 CSV: %v", err)
	}
	for _, res := range []ExportResult{htmlRes, csvRes} {
		if res.Groups != 1 {
			t.Fatalf("%s 组数应为 1，实际 %d", res.Format, res.Groups)
		}
		if res.Members != 2 {
			t.Fatalf("%s 成员数应为 2，实际 %d", res.Format, res.Members)
		}
		if res.ReclaimableBytes != int64(len(content)) {
			t.Fatalf("%s 可回收字节应为 %d，实际 %d", res.Format, len(content), res.ReclaimableBytes)
		}
		if res.Bytes <= 0 {
			t.Fatalf("%s 导出文件为空", res.Format)
		}
		if res.ScanStartedAt == "" {
			t.Fatalf("%s 缺少扫描时间", res.Format)
		}
	}

	htmlRaw, err := os.ReadFile(htmlRes.Path)
	if err != nil {
		t.Fatal(err)
	}
	htmlText := string(htmlRaw)
	for _, want := range []string{
		"查重结果报告", "扫描参数", "扫描根目录", "最短路径优先",
		"可回收空间合计", "本组可回收", "保留", "待删除",
		esc(keep), esc(dup), humanBytes(int64(len(content))),
	} {
		if !strings.Contains(htmlText, want) {
			t.Fatalf("HTML 缺少 %q", want)
		}
	}

	csvRaw, err := os.ReadFile(csvRes.Path)
	if err != nil {
		t.Fatal(err)
	}
	csvText := string(csvRaw)
	if !strings.HasPrefix(csvText, "\ufeff") {
		t.Fatalf("CSV 缺少 UTF-8 BOM")
	}
	for _, want := range []string{
		"记录类型", "扫描信息", "汇总", "分组", "文件",
		"最短路径优先", keep, dup, fmt.Sprint(len(content)), "可回收字节", "扫描开始时间",
	} {
		if !strings.Contains(csvText, want) {
			t.Fatalf("CSV 缺少 %q", want)
		}
	}

	if _, err := app.exportResults(id, "pdf", dir); err == nil {
		t.Fatal("不支持的格式应报错")
	}
	if _, err := app.ExportResults("missing-session", "csv"); err == nil {
		t.Fatal("未知会话应报错")
	}
}
