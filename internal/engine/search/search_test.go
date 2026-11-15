package search

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	other := t.TempDir()
	writeFile(t, filepath.Join(root, "照片", "Alpha.JPG"), 3000)
	writeFile(t, filepath.Join(root, "照片", "beta.txt"), 10)
	writeFile(t, filepath.Join(root, "文档", "gamma.jpg"), 2000)
	writeFile(t, filepath.Join(root, "node_modules", "dep.js"), 500)
	writeFile(t, filepath.Join(other, "delta.png"), 4000)
	return root, other
}

func TestSubstringExtAndSizeFilter(t *testing.T) {
	root, other := fixture(t)
	req := Request{
		Roots:   []string{root, other},
		Query:   "照片",
		Exts:    []string{".jpg"},
		MinSize: 100,
	}
	res, err := Run(context.Background(), req, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("want 1 match, got %d: %+v", len(res.Matches), res.Matches)
	}
	for _, m := range res.Matches {
		if !strings.HasSuffix(strings.ToLower(m.Path), ".jpg") {
			t.Errorf("extension filter failed: %s", m.Path)
		}
		if m.Size < 100 {
			t.Errorf("size filter failed: %s %d", m.Path, m.Size)
		}
	}
	// 第二个根目录也要参与搜索。
	res, err = Run(context.Background(), Request{Roots: []string{root, other}, Query: "delta"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Root != other {
		t.Fatalf("multi-root search failed: %+v", res.Matches)
	}
}

func TestWildcardAndExcludes(t *testing.T) {
	root, other := fixture(t)
	req := Request{
		Roots:    []string{root, other},
		Query:    "*.jpg",
		Mode:     ModeWildcard,
		Excludes: []string{"node_modules"},
	}
	res, err := Run(context.Background(), req, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("want 2 jpg matches, got %d", len(res.Matches))
	}
	for _, m := range res.Matches {
		if strings.Contains(m.Path, "node_modules") {
			t.Errorf("excluded dir leaked: %s", m.Path)
		}
	}
}

func TestWildcardAutoSubstring(t *testing.T) {
	root, _ := fixture(t)
	req := Request{Roots: []string{root}, Query: "alp", Mode: ModeWildcard}
	res, err := Run(context.Background(), req, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 1 || res.Matches[0].Name != "Alpha.JPG" {
		t.Fatalf("want Alpha.JPG, got %+v", res.Matches)
	}
}

func TestLimitTruncates(t *testing.T) {
	root, other := fixture(t)
	req := Request{Roots: []string{root, other}, Limit: 2}
	res, err := Run(context.Background(), req, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Matches) != 2 {
		t.Fatalf("want truncated with 2 matches, got %v %d", res.Truncated, len(res.Matches))
	}
}

func TestCancel(t *testing.T) {
	root, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Request{Roots: []string{root}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("want context canceled, got %v", err)
	}
}

func TestHiddenInjected(t *testing.T) {
	root, _ := fixture(t)
	hidden := func(p string, _ fs.FileInfo) bool { return strings.Contains(p, "文档") }
	res, err := Run(context.Background(), Request{Roots: []string{root}}, Options{HiddenFile: hidden})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Matches {
		if strings.Contains(m.Path, "文档") {
			t.Fatalf("hidden dir leaked: %s", m.Path)
		}
	}
	res, err = Run(context.Background(), Request{Roots: []string{root}, IncludeHidden: true}, Options{HiddenFile: hidden})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range res.Matches {
		if strings.Contains(m.Path, "文档") {
			found = true
		}
	}
	if !found {
		t.Fatal("IncludeHidden should include hidden dir")
	}
}

func TestSort(t *testing.T) {
	ms := []Match{{Path: "b", Name: "b", Size: 2, ModTime: "2026-01-02T00:00:00Z"}, {Path: "a", Name: "a", Size: 9, ModTime: "2026-01-01T00:00:00Z"}}
	Sort(ms, "size", true)
	if ms[0].Size != 9 {
		t.Fatalf("size desc failed: %+v", ms)
	}
	Sort(ms, "name", false)
	if ms[0].Name != "a" {
		t.Fatalf("name asc failed: %+v", ms)
	}
	Sort(ms, "modTime", true)
	if ms[0].Name != "b" {
		t.Fatalf("modTime desc failed: %+v", ms)
	}
}

func TestValidateRejectsRelativeRoot(t *testing.T) {
	req := Request{Roots: []string{"relative/dir"}}
	if err := req.Validate(); err == nil {
		t.Fatal("relative root should be rejected")
	}
}
