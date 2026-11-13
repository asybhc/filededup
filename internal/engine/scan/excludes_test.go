package scan

import (
	"reflect"
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
