package logx

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filededup/internal/engine/model"
)

func TestOpsWriterAppendsJSONLines(t *testing.T) {
	dir := t.TempDir()
	clock := time.Date(2026, 11, 14, 10, 30, 0, 0, time.UTC)
	w, err := OpenOps(dir, "plan-1", clock)
	if err != nil {
		t.Fatalf("打开操作清单失败：%v", err)
	}
	path := w.Path()
	if filepath.Base(path) != "ops_20261114.jsonl" {
		t.Fatalf("文件名=%q", filepath.Base(path))
	}

	src := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = w.Record(model.Action{
		Seq:      1,
		Kind:     model.ActionDeleteTrash,
		Src:      src,
		Status:   model.StatusFailed,
		Err:      "拒绝访问",
		Precheck: &model.Precheck{Size: 7, ModTime: "2026-11-14T10:00:00Z"},
	})
	if err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("关闭失败：%v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("重复关闭应安全：%v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []Record
	for sc.Scan() {
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("解析行失败：%v", err)
		}
		lines = append(lines, rec)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("行数=%d", len(lines))
	}
	got := lines[0]
	if got.PlanID != "plan-1" || got.Seq != 1 || got.Kind != model.ActionDeleteTrash {
		t.Fatalf("字段不符：%+v", got)
	}
	if got.Status != model.StatusFailed || got.Error != "拒绝访问" {
		t.Fatalf("状态字段不符：%+v", got)
	}
	if got.Pre == nil || got.Pre.Size != 7 {
		t.Fatalf("pre 字段缺失：%+v", got.Pre)
	}
	if got.Post == nil || got.Post.Size != 7 {
		t.Fatalf("失败后源文件仍在，post 应记录当前指纹：%+v", got.Post)
	}
	if got.TS == "" {
		t.Fatalf("ts 为空")
	}
}

func TestOpsWriterPostFingerprintForMove(t *testing.T) {
	dir := t.TempDir()
	w, err := OpenOps(dir, "plan-2", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	src := filepath.Join(dir, "from.txt")
	dst := filepath.Join(dir, "to.txt")
	if err := os.WriteFile(dst, []byte("1234"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(model.Action{
		Seq: 1, Kind: model.ActionRename, Src: src, Dst: dst, Status: model.StatusApplied,
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	var rec Record
	if err := json.Unmarshal(raw[:len(raw)-1], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Post == nil {
		t.Fatalf("移动后 post 应指向 Dst")
	}
	if rec.Post.Size != 4 {
		t.Fatalf("post.Size=%d，期望 4", rec.Post.Size)
	}
}
