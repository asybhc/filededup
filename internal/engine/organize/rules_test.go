package organize

import (
	"encoding/json"
	"os"
	"testing"
)

type renameGolden struct {
	Name     string `json:"name"`
	Prefix   string `json:"prefix"`
	Suffix   string `json:"suffix"`
	Sanitize string `json:"sanitize"`
	Category string `json:"category"`
	ExtUpper string `json:"extUpper"`
}

// TestRenameRulesGolden 用 C# 输出（--dump-rename-json）逐条对拍纯函数。
func TestRenameRulesGolden(t *testing.T) {
	raw, err := os.ReadFile("testdata/rename_golden.json")
	if err != nil {
		t.Fatalf("读取金标准失败: %v", err)
	}
	var cases []renameGolden
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("解析金标准失败: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("金标准为空")
	}
	for _, c := range cases {
		stem := WinStem(c.Name)
		if got := CleanPrefix(stem); got != c.Prefix {
			t.Errorf("%q prefix = %q, 原版 %q", c.Name, got, c.Prefix)
		}
		if got := CleanSuffix(stem); got != c.Suffix {
			t.Errorf("%q suffix = %q, 原版 %q", c.Name, got, c.Suffix)
		}
		if got := SanitizeTargetName(c.Name); got != c.Sanitize {
			t.Errorf("%q sanitize = %q, 原版 %q", c.Name, got, c.Sanitize)
		}
		if got := Category(c.Name); got != c.Category {
			t.Errorf("%q category = %q, 原版 %q", c.Name, got, c.Category)
		}
		if got := ExtensionUpper(c.Name); got != c.ExtUpper {
			t.Errorf("%q extUpper = %q, 原版 %q", c.Name, got, c.ExtUpper)
		}
	}
}

// TestWinPath 覆盖 .NET 路径语义的关键边界。
func TestWinPath(t *testing.T) {
	cases := []struct{ in, base, ext, stem string }{
		{`a\b\c.txt`, "c.txt", ".txt", "c"},
		{"a/b/c.TXT", "c.TXT", ".TXT", "c"},
		{".gitignore", ".gitignore", ".gitignore", ""},
		{"a.tar.gz", "a.tar.gz", ".gz", "a.tar"},
		{"trailingdot.", "trailingdot.", "", "trailingdot"},
		{"noext", "noext", "", "noext"},
	}
	for _, c := range cases {
		if got := WinBase(c.in); got != c.base {
			t.Errorf("WinBase(%q)=%q want %q", c.in, got, c.base)
		}
		if got := WinExt(c.in); got != c.ext {
			t.Errorf("WinExt(%q)=%q want %q", c.in, got, c.ext)
		}
		if got := WinStem(c.in); got != c.stem {
			t.Errorf("WinStem(%q)=%q want %q", c.in, got, c.stem)
		}
	}
}

// TestKeepWinner 覆盖保留规则。
func TestKeepWinner(t *testing.T) {
	group := []FileEntry{
		{Path: "b/aa.txt", Size: 5},
		{Path: "a/bb.txt", Size: 9},
		{Path: "c.txt", Size: 9},
	}
	if got := KeepWinner(group, KeepShortestPath); group[got].Path != "c.txt" {
		t.Errorf("最短路径保留 %q", group[got].Path)
	}
	if got := KeepWinner(group, KeepLargest); group[got].Path != "c.txt" {
		t.Errorf("最大文件保留 %q", group[got].Path)
	}
	if got := KeepWinner(group, KeepShortestName); group[got].Path != "c.txt" {
		t.Errorf("最短文件名保留 %q", group[got].Path)
	}
}
