// Package orgscan 提供整理器的目录枚举与指纹预计算。
//
// 放在 internal/engine 之外，是因为枚举需要文件系统属性与平台判断；
// 引擎（纯函数层）不得依赖平台包，而 CLI 与对拍工具必须使用同一份枚举逻辑，
// 否则“同一语料、不同枚举”会制造虚假的对拍差异。
package orgscan

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"filededup/internal/engine/organize"
	"filededup/internal/engine/similar/audio"
	imgsim "filededup/internal/engine/similar/image"
	"filededup/internal/platform"
)

// File 是导出用（JSON）的文件条目，字段与 C# 诊断导出同构。
type File struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
	Readonly bool   `json:"readonly"`
}

// Tree 是一次完整枚举的结果。
type Tree struct {
	Dirs    []string
	Files   []File
	Entries []organize.FileEntry
}

// Collect 枚举 root 下全部目录与文件：相对 '/' 路径、大小、只读/隐藏/系统位、SHA-256。
//
// 对齐 B/E.cs B_m_1：重解析点（符号链接/目录联接）不扫描、不跟随、不计数；
// 读不到文件内容时 SHA-256 留空（与 C# 一致），不中止枚举。
func Collect(root string) (Tree, error) {
	t := Tree{Dirs: []string{}, Files: []File{}, Entries: []organize.FileEntry{}}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if platform.IsReparse(path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		relSlash := filepath.ToSlash(rel)
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		if d.IsDir() {
			t.Dirs = append(t.Dirs, relSlash)
			return nil
		}
		ro, hidden, system := FileFlags(path)
		sum, herr := SHA256(path)
		if herr != nil {
			sum = ""
		}
		t.Files = append(t.Files, File{Path: relSlash, Size: info.Size(), SHA256: sum, Readonly: ro})
		t.Entries = append(t.Entries, organize.FileEntry{
			Path: relSlash, Size: info.Size(), ModTime: info.ModTime(),
			Readonly: ro, Hidden: hidden, System: system, SHA256: sum,
		})
		return nil
	})
	if err != nil {
		return t, err
	}
	sort.Strings(t.Dirs)
	sort.Slice(t.Files, func(i, j int) bool { return t.Files[i].Path < t.Files[j].Path })
	return t, nil
}

// SHA256 计算文件内容的 SHA-256 十六进制串。
func SHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// PrepareProviders 预计算图片 dHash / 音频时长，并返回规划器所需的 Providers。
// 注意：会就地写入 entries 的 DHash/AudioMs 字段（调用方的快照随之带上指纹）。
func PrepareProviders(entries []organize.FileEntry, root string, cfg organize.Config) organize.Providers {
	imgHash := map[string]uint64{}
	audMS := map[string]int64{}
	for i := range entries {
		e := &entries[i]
		abs := filepath.Join(root, filepath.FromSlash(e.Path))
		if cfg.Steps.ImageDedup && organize.IsImage(e.Path) {
			if h, ok := imgsim.Hash(abs); ok {
				e.DHash, e.HasDHash = h, true
				imgHash[e.Path] = h
			}
		}
		if cfg.Steps.AudioDedup && organize.IsAudio(e.Path) {
			if sec, ok := audio.Probe(abs); ok {
				ms := audio.DurationMS(sec)
				e.AudioMs, e.HasAudio = ms, true
				audMS[e.Path] = ms
			}
		}
	}
	return organize.Providers{
		ContentHash: func(e organize.FileEntry) (string, bool) {
			if e.SHA256 == "" {
				return "", false
			}
			return e.SHA256, true
		},
		ImageHash: func(e organize.FileEntry) (uint64, bool) {
			h, ok := imgHash[e.Path]
			return h, ok
		},
		AudioMsFn: func(e organize.FileEntry) (int64, bool) {
			ms, ok := audMS[e.Path]
			return ms, ok
		},
	}
}
