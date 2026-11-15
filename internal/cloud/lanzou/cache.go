package lanzou

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// uploadCacheMaxEntries 是本机上传缓存的条目上限，超出后按上传时间淘汰最旧的。
const uploadCacheMaxEntries = 5000

// CacheEntry 是一条“这文件已经传过”的记录。
//
// 与 C# 版的差异（如实记录）：C# 的键包含文件 MD5，本实现用「路径 + 大小 + 修改时间」
// 代替，避免为了查缓存再多读一遍整个文件；代价是文件内容变了但大小与修改时间都没变时
// 会误判为已传。强制重传可以绕过缓存。
type CacheEntry struct {
	Path       string    `json:"path"`
	FolderID   string    `json:"folderId"`
	Size       int64     `json:"size"`
	ModTime    int64     `json:"modTime"`
	RemoteID   string    `json:"remoteId,omitempty"`
	UploadedAt time.Time `json:"uploadedAt"`
}

// UploadCache 是本机上传缓存。不包含任何凭据。
type UploadCache struct {
	SchemaVersion int                   `json:"schemaVersion"`
	Entries       map[string]CacheEntry `json:"entries"`
}

// CachePath 返回缓存文件路径：%AppData%\FileDedup\lanzou\upload-cache.json。
func CachePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "lanzou", "upload-cache.json"), nil
}

// LoadUploadCache 读取缓存；文件不存在或损坏时返回空缓存，不阻断上传。
func LoadUploadCache() *UploadCache {
	c := &UploadCache{SchemaVersion: 1, Entries: map[string]CacheEntry{}}
	path, err := CachePath()
	if err != nil {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return c
		}
		return c
	}
	var loaded UploadCache
	if json.Unmarshal(raw, &loaded) != nil {
		return c
	}
	if loaded.Entries == nil {
		loaded.Entries = map[string]CacheEntry{}
	}
	loaded.SchemaVersion = 1
	return &loaded
}

// Key 生成本机缓存键。路径先转绝对路径并 Clean，避免同一文件不同写法重复入库。
func (c *UploadCache) Key(folderID, localPath string, size, modTime int64) string {
	abs := localPath
	if a, err := filepath.Abs(localPath); err == nil {
		abs = a
	}
	return PlatformLanzou + "|" + folderID + "|" + filepath.Clean(abs) + "|" +
		strconv.FormatInt(size, 10) + "|" + strconv.FormatInt(modTime, 10)
}

// Hit 查缓存。
func (c *UploadCache) Hit(key string) (CacheEntry, bool) {
	e, ok := c.Entries[key]
	return e, ok
}

// Put 写入一条记录。
func (c *UploadCache) Put(key string, e CacheEntry) {
	if c.Entries == nil {
		c.Entries = map[string]CacheEntry{}
	}
	c.Entries[key] = e
}

// Save 原子写盘。失败只返回错误，由调用方决定只记日志。
func (c *UploadCache) Save() error {
	path, err := CachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	c.prune()
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// prune 控制条目数量，按上传时间保留最新的。
func (c *UploadCache) prune() {
	if len(c.Entries) <= uploadCacheMaxEntries {
		return
	}
	type kv struct {
		key string
		at  time.Time
	}
	all := make([]kv, 0, len(c.Entries))
	for k, v := range c.Entries {
		all = append(all, kv{k, v.UploadedAt})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	for _, item := range all[uploadCacheMaxEntries:] {
		delete(c.Entries, item.key)
	}
}

// Stat 返回本地文件的大小与修改时间（纳秒），供缓存键使用。
func Stat(path string) (size, modTime int64, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	return info.Size(), info.ModTime().UnixNano(), nil
}
