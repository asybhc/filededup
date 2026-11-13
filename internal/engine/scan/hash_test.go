package scan

import (
	"testing"
	"time"
)

func TestHashCacheEviction(t *testing.T) {
	c := newHashCache(3, 3)
	c.put("k1", "v1")
	c.put("k2", "v2")
	c.put("k3", "v3")
	if _, ok := c.get("k2"); !ok {
		t.Fatal("k2 should be cached")
	}
	c.put("k4", "v4")
	if _, ok := c.get("k1"); ok {
		t.Fatal("k1 should be evicted")
	}
	if v, ok := c.get("k4"); !ok || v != "v4" {
		t.Fatalf("k4=%q ok=%v", v, ok)
	}
	if len(c.m) != 1 {
		t.Fatalf("cache size=%d want 1", len(c.m))
	}
}

func TestHashCacheDisabled(t *testing.T) {
	c := newHashCache(10, 2)
	c.disable()
	c.put("k", "v")
	if _, ok := c.get("k"); ok {
		t.Fatal("disabled cache should not store")
	}
}

func TestCacheKeyIncludesMTime(t *testing.T) {
	a := fileEntry{path: "/x/a", size: 1, modTime: time.Unix(0, 100)}
	b := fileEntry{path: "/x/a", size: 1, modTime: time.Unix(0, 200)}
	if cacheKey(a) == cacheKey(b) {
		t.Fatal("cache key must include mtime")
	}
}
