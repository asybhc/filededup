package scan

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSplitExcludes(t *testing.T) {
	got := splitExcludes([]string{"node_modules, .git；dist  build\\", "", "a;b"})
	want := []string{"node_modules", ".git", "dist", "build", "a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestExcludedBareName(t *testing.T) {
	patterns := splitExcludes([]string{"skip,tmp"})
	cases := map[string]bool{
		"/root/skip/a.txt":         true,
		"/root/a/skip/b.txt":       true,
		"/root/skipfile.txt":       false,
		"/root/tmp":                false, // 只匹配父目录链，不匹配文件本身
		"/root/sub/tmp/c.txt":      true,
		`C:\root\skip\d.txt`:       true,
		"/root/keep/skip.txt":      false,
		"/root/keep/not-tmp/a.txt": false,
	}
	for p, want := range cases {
		if got := excluded(p, patterns); got != want {
			t.Errorf("excluded(%q)=%v want %v", p, got, want)
		}
	}
}

func TestIsToolReportName(t *testing.T) {
	cases := map[string]bool{
		`C:\Users\a\Documents\dedup-report-20261116-153000.html`: true,
		`C:\Users\a\Documents\DEDUP-REPORT-20261116-153000.CSV`:  true,
		`/home/a/dedup-report-20261116-153000.csv`:               true,
		`C:\a\dedup-report-20261116-153000.txt`:                  false,
		`C:\a\my-dedup-report-20261116-153000.html`:              false,
		`C:\a\upload-report-20261116-153000.csv`:                 false,
		`C:\a\dedup-report.html`:                                 false,
	}
	for p, want := range cases {
		if got := isToolReportName(p); got != want {
			t.Errorf("isToolReportName(%q)=%v want %v", p, got, want)
		}
	}
}

// 导出报告必须被扫描忽略：否则用户扫描导出目录时会把报告本身当候选。
func TestScanSkipsToolReports(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.txt", "duplicate-content")
	writeFile(t, root, "b.txt", "duplicate-content")
	writeFile(t, root, "dedup-report-20261116-153000.html", "<html>same</html>")
	writeFile(t, root, "dedup-report-20261116-153000.csv", "<html>same</html>")

	res, err := Run(context.Background(), testRequest(t, root))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("应只有 1 个普通重复组，实际 %d 组", len(res.Groups))
	}
	for _, g := range res.Groups {
		for _, m := range g.Members {
			if strings.Contains(strings.ToLower(m.Path), "dedup-report-") {
				t.Fatalf("报告文件不应参与查重：%s", m.Path)
			}
		}
	}
}

func TestExcludedPathLike(t *testing.T) {
	patterns := splitExcludes([]string{`/root/vendor`, `C:\work\cache`})
	cases := map[string]bool{
		"/root/vendor/a.txt":     true,
		"/root/vendor2/a.txt":    false,
		"/root/vendor":           true,
		`C:\work\cache\x\y.txt`:  true,
		`C:\work\cache2\x\y.txt`: false,
	}
	for p, want := range cases {
		if got := excluded(p, patterns); got != want {
			t.Errorf("excluded(%q)=%v want %v", p, got, want)
		}
	}
}
