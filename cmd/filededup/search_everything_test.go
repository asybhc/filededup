//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"testing"

	"filededup/internal/engine/search"
)

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
