//go:build desktop

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filededup/internal/engine/search"
)

func TestEverythingArgsOrder(t *testing.T) {
	args := everythingArgs(50, `D:\root`, "report")
	want := []string{"-n", "50", "-path", `D:\root`, "report"}
	if len(args) != len(want) {
		t.Fatalf("args=%v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args=%v, want %v", args, want)
		}
	}
	// 空关键词仍然带 -n 与 -path，es.exe 会列出该目录下的全部条目。
	args = everythingArgs(7, `D:\root`, "  ")
	if len(args) != 4 || args[0] != "-n" || args[3] != `D:\root` {
		t.Fatalf("empty query args=%v", args)
	}
}

// buildFakeES 现场编译 testdata/fake-es 为 es.exe，放到临时 PATH 里，
// 从而让 findEverythingCLI 与子进程调用链路都走真实进程。
func buildFakeES(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("es.exe 调用链只在 Windows 上验证")
	}
	dir := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "es.exe"), "./testdata/fake-es")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译假 es.exe 失败：%v\n%s", err, out)
	}
	return dir
}

func TestEverythingRunInvokesCLIWithArgs(t *testing.T) {
	dir := buildFakeES(t)
	t.Setenv("PATH", dir+";"+os.Getenv("PATH"))
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_ES_LOG", logPath)

	root := t.TempDir()
	for _, name := range []string{"keep.txt", filepath.Join("sub", "keep.log"), "other.txt"} {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	res, err := runEverythingSearch(context.Background(), "", search.Request{
		Roots: []string{root},
		Query: "keep",
		Limit: 50,
		Mode:  search.ModeSubstring,
	}, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("matches=%d, want 2", len(res.Matches))
	}
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取调用日志失败：%v", err)
	}
	want := "-n\x1f50\x1f-path\x1f" + root + "\x1fkeep"
	if !strings.Contains(string(calls), want) {
		t.Fatalf("子进程参数不符：%q，want 含 %q", calls, want)
	}
}

func TestEverythingMissingRootReportsReason(t *testing.T) {
	dir := buildFakeES(t)
	t.Setenv("PATH", dir+";"+os.Getenv("PATH"))
	logPath := filepath.Join(dir, "calls.log")
	t.Setenv("FAKE_ES_LOG", logPath)

	missing := filepath.Join(t.TempDir(), "no-such-dir")
	if _, err := runEverythingSearch(context.Background(), "", search.Request{
		Roots: []string{missing},
		Mode:  search.ModeSubstring,
	}, nil); err == nil || !strings.Contains(err.Error(), "搜索目录不可用") {
		t.Fatalf("应报目录不可用，得到 %v", err)
	}
	// 目录自查在调用 es.exe 之前完成，不应产生子进程调用。
	if _, err := os.Stat(logPath); err == nil {
		t.Fatalf("不可用目录不应调用 es.exe")
	}

	// 有效目录 + 缺失目录：保留有效结果，只记录跳过，不整体失败。
	valid := t.TempDir()
	if err := os.WriteFile(filepath.Join(valid, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	res, err := runEverythingSearch(context.Background(), "", search.Request{
		Roots: []string{missing, valid},
		Mode:  search.ModeSubstring,
		Limit: 10,
	}, nil)
	if err != nil {
		t.Fatalf("混合目录不应失败：%v", err)
	}
	if len(res.Matches) != 1 || len(res.Skipped) != 1 {
		t.Fatalf("matches=%d skipped=%d", len(res.Matches), len(res.Skipped))
	}
}

func TestEverythingLinesParsesOutput(t *testing.T) {
	out := []byte("\ufeffC:\\a\\b.txt\r\n\r\nnot a path\r\nD:\\x\\y.png\r\n")
	lines := everythingLines(out)
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	if lines[0] != `C:\a\b.txt` || lines[1] != `D:\x\y.png` {
		t.Fatalf("lines=%v", lines)
	}
}

func TestDecodeConsoleGBK(t *testing.T) {
	// GBK 编码的“中文”。
	raw := []byte{0xD6, 0xD0, 0xCE, 0xC4}
	if got := decodeConsole(raw); got != "中文" {
		t.Fatalf("decodeConsole=%q", got)
	}
	if got := decodeConsole([]byte("utf8-中文")); got != "utf8-中文" {
		t.Fatalf("utf8 decode=%q", got)
	}
}

func TestEverythingFilterConditions(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) string {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		return p
	}
	keep := write("keep.txt", "12345")
	small := write("small.txt", "1")
	pic := write("pic.png", "12345")
	inSkip := write(filepath.Join("skipme", "inside.txt"), "12345")

	req := search.Request{
		Roots:    []string{dir},
		Exts:     []string{"txt"},
		MinSize:  5,
		MaxSize:  100,
		Excludes: []string{"skipme"},
		Mode:     search.ModeSubstring,
	}
	f := newEverythingFilter(req)
	if _, ok := f.match(keep, dir); !ok {
		t.Fatal("keep.txt 应命中")
	}
	if _, ok := f.match(small, dir); ok {
		t.Fatal("small.txt 小于最小大小，不应命中")
	}
	if _, ok := f.match(pic, dir); ok {
		t.Fatal("pic.png 扩展名不符，不应命中")
	}
	if _, ok := f.match(inSkip, dir); ok {
		t.Fatal("skipme 下文件应被排除")
	}
	// 排除项为空时同目录文件应命中。
	if _, ok := newEverythingFilter(search.Request{Mode: search.ModeSubstring}).match(keep, dir); !ok {
		t.Fatal("无过滤条件时应命中")
	}
}
