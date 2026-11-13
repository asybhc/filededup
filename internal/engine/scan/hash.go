package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strconv"
	"sync"
)

// hashCache 是进程内哈希缓存，键为 路径|长度|修改时间(100ns)。
// 上限 20000，溢出淘汰最旧 2000 条（与 C# 基线一致）。
type hashCache struct {
	mu       sync.Mutex
	m        map[string]string
	order    []string
	limit    int
	evict    int
	disabled bool
}

func newHashCache(limit, evict int) *hashCache {
	if limit <= 0 {
		limit = 20000
	}
	if evict <= 0 {
		evict = 2000
	}
	if evict > limit {
		evict = limit
	}
	return &hashCache{m: make(map[string]string), limit: limit, evict: evict}
}

func (c *hashCache) disable() { c.disabled = true }

func (c *hashCache) get(key string) (string, bool) {
	if c.disabled {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok
}

func (c *hashCache) put(key, val string) {
	if c.disabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.m[key]; exists {
		c.m[key] = val
		return
	}
	c.m[key] = val
	c.order = append(c.order, key)
	if len(c.order) > c.limit {
		for i := 0; i < c.evict && i < len(c.order); i++ {
			delete(c.m, c.order[i])
		}
		c.order = c.order[min(c.evict, len(c.order)):]
	}
}

func cacheKey(f fileEntry) string {
	return f.path + "|" + strconv.FormatInt(f.size, 10) + "|" + strconv.FormatInt(f.modTime.UTC().UnixNano()/100, 10)
}

// hashFile 流式计算 SHA-256，每 1 MiB 检查一次取消。
func hashFile(ctx context.Context, path string, buf []byte, open func(string) (io.ReadCloser, error)) (string, int64, error) {
	f, err := open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha256.New()
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", total, err
		}
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := h.Write(buf[:n]); werr != nil {
				return "", total, werr
			}
			total += int64(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", total, rerr
		}
	}
	return hex.EncodeToString(h.Sum(nil)), total, nil
}
