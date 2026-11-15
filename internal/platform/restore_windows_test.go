//go:build windows

package platform

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRestoreFromTrashRoundTrip 真实走一遍“回收站删除 → 按原路径恢复”，
// 并校验目录被删除后能先重建目录再还原。回收站不可用时跳过并说明原因。
func TestRestoreFromTrashRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("创建子目录失败：%v", err)
	}
	path := filepath.Join(sub, "fdedup-restore-test.txt")
	want := []byte("restore me " + time.Now().Format(time.RFC3339Nano))
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
	if err := Trash(path); err != nil {
		t.Skipf("回收站不可用，跳过恢复验证：%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("删除后原路径仍然存在：%v", err)
	}
	// 模拟“目标目录也已被删掉”，验证还原前会先建目录。
	if err := os.Remove(sub); err != nil {
		t.Fatalf("删除空子目录失败：%v", err)
	}

	rep := RestoreFromTrash([]string{path})
	if len(rep.Results) != 1 {
		t.Fatalf("恢复结果条数 = %d，期望 1", len(rep.Results))
	}
	res := rep.Results[0]
	if res.Path != path {
		t.Fatalf("恢复结果路径 = %q，期望 %q", res.Path, path)
	}
	if !res.OK {
		t.Fatalf("恢复失败：%s", res.Err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("恢复后读取失败：%v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("恢复后内容不一致：%q != %q", got, want)
	}
	t.Logf("回收站项数=%d，恢复后内容一致，目录已重建", rep.BinItems)
	if err := os.Remove(path); err != nil {
		t.Logf("清理恢复出来的测试文件失败：%v", err)
	}
}

// TestRestoreFromTrashMissing 验证“回收站里没有对应项”时返回明确失败而非成功。
func TestRestoreFromTrashMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fdedup-never-in-trash.txt")
	rep := RestoreFromTrash([]string{path})
	if len(rep.Results) != 1 {
		t.Fatalf("恢复结果条数 = %d，期望 1", len(rep.Results))
	}
	if rep.Results[0].OK {
		t.Fatalf("不存在的项不应报告恢复成功")
	}
	if !strings.Contains(rep.Results[0].Err, "找不到") {
		t.Fatalf("失败原因应说明回收站里找不到，实际：%q", rep.Results[0].Err)
	}
	if rep.BinItems < 0 {
		t.Fatalf("应能读到回收站项数，实际 %d", rep.BinItems)
	}
}

// TestRestoreFromTrashNoOverwrite 验证原位置已有同名文件时不覆盖。
func TestRestoreFromTrashNoOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fdedup-existing.txt")
	if err := os.WriteFile(path, []byte("later-created"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := RestoreFromTrash([]string{path})
	if len(rep.Results) != 1 {
		t.Fatalf("恢复结果条数 = %d，期望 1", len(rep.Results))
	}
	if rep.Results[0].OK {
		t.Fatalf("目标已存在时不应报告恢复成功")
	}
	if !strings.Contains(rep.Results[0].Err, "未覆盖") {
		t.Fatalf("失败原因应说明未覆盖，实际：%q", rep.Results[0].Err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "later-created" {
		t.Fatalf("已有文件不应被改动：content=%q err=%v", got, err)
	}
}

// TestRestoreFromTrashEmpty 验证空输入不调用脚本且返回空结果。
func TestRestoreFromTrashEmpty(t *testing.T) {
	rep := RestoreFromTrash(nil)
	if len(rep.Results) != 0 {
		t.Fatalf("空输入应返回空结果，得到 %d 条", len(rep.Results))
	}
}
