package scan

import (
	"testing"
	"time"

	"filededup/internal/engine/model"
)

func TestKeepPolicies(t *testing.T) {
	base := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	files := []fileEntry{
		{path: "/x/aa.txt", size: 10, modTime: base},
		{path: "/x/b.txt", size: 20, modTime: base.Add(time.Hour)},
		{path: "/x/cc.txt", size: 5, modTime: base.Add(-time.Hour)},
	}
	idxs := []int{0, 1, 2}
	cases := []struct {
		policy model.KeepPolicy
		want   int
	}{
		{model.KeepNewest, 1},
		{model.KeepOldest, 2},
		{model.KeepLargest, 1},
		{model.KeepNameShortest, 1},
		{model.KeepPathShortest, 1},
	}
	for _, c := range cases {
		if got := keepIndex(c.policy, idxs, files); got != c.want {
			t.Errorf("policy %s: got %d want %d", c.policy, got, c.want)
		}
	}
}

func TestKeepTieBreakByPath(t *testing.T) {
	base := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	files := []fileEntry{
		{path: "/x/b.txt", size: 1, modTime: base},
		{path: "/x/a.txt", size: 1, modTime: base},
	}
	if got := keepIndex(model.KeepNewest, []int{0, 1}, files); got != 1 {
		t.Fatalf("got %d want 1", got)
	}
	if got := keepIndex(model.KeepOldest, []int{0, 1}, files); got != 1 {
		t.Fatalf("got %d want 1", got)
	}
}

func TestUTF16Len(t *testing.T) {
	cases := map[string]int{
		"abc": 3,
		"报告":  2,
		"😀":   2,
		"a😀b": 4,
	}
	for s, want := range cases {
		if got := utf16Len(s); got != want {
			t.Errorf("utf16Len(%q)=%d want %d", s, got, want)
		}
	}
}
