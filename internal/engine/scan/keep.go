package scan

import (
	"path/filepath"
	"strings"

	"filededup/internal/engine/model"
)

// utf16Len 返回 UTF-16 码元数，复刻 C# string.Length。
// 确定性排序与保留策略比较必须用它，不能用 len()（UTF-8 字节）。
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// keepIndex 在候选下标中选出保留项。语义见 B/E.cs#L1188-L1207。
// 并列时追加确定性比较（路径长度、路径序数），避免依赖枚举顺序。
func keepIndex(policy model.KeepPolicy, idxs []int, files []fileEntry) int {
	return keepIndexPolicy(policy, idxs, files)
}

// keepIndexPolicy 是 keepIndex 的显式策略版本，供图片组使用独立保留策略。
func keepIndexPolicy(policy model.KeepPolicy, idxs []int, files []fileEntry) int {
	best := idxs[0]
	better := func(a, b int) bool { return compareKeep(policy, files[a], files[b]) < 0 }
	for _, i := range idxs[1:] {
		if better(i, best) {
			best = i
		}
	}
	return best
}

func compareKeep(policy model.KeepPolicy, a, b fileEntry) int {
	switch policy {
	case model.KeepNewest:
		if c := b.modTime.Compare(a.modTime); c != 0 {
			return c
		}
	case model.KeepOldest:
		if c := a.modTime.Compare(b.modTime); c != 0 {
			return c
		}
	case model.KeepNameShortest:
		if c := compareInt(utf16Len(filepath.Base(a.path)), utf16Len(filepath.Base(b.path))); c != 0 {
			return c
		}
	case model.KeepLargest:
		if c := compareInt64(b.size, a.size); c != 0 {
			return c
		}
	}
	if c := compareInt(utf16Len(a.path), utf16Len(b.path)); c != 0 {
		return c
	}
	return strings.Compare(a.path, b.path)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
