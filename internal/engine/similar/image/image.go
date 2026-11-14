// Package image 实现相似图片判定：9×8 dHash 感知哈希与相似组聚类。
//
// 算法逐条复刻 C# 基线 A/Q.cs：
//   - 先按 EXIF Orientation(274) 归一，再缩放到 9×8 白底 24 位画布（双线性、高质量）。
//   - 8 行 × 8 列，灰度按 (R*299+G*587+B*114)/1000 取整，左侧灰度大于右侧时置位；
//     bit = row*8+col。
//   - 分组：贪心锚点 + 组内直径约束（与锚点及组内每个已加入成员的海明距离都 <= 阈值）。
//
// 与原版的差异（M3 已登记）：引擎禁止第三方依赖，TIFF/WebP 无法解码，
// 这两个扩展名会进入 Skipped(unsupported_format)，其余 5 种格式（jpg/jpeg/png/gif/bmp）
// 用标准库 + 自写 BMP 解码实现。
package image

import (
	"os"
	"path/filepath"
	"strings"
)

var supportedExts = []string{"jpg", "jpeg", "png", "gif", "bmp", "tif", "tiff", "webp"}

var decodableExts = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "bmp": true,
}

// Supported 报告扩展名是否在原版图片支持清单内（大小写不敏感）。
// 注意：tif/tiff/webp 在原版清单内，但当前实现无法解码。
func Supported(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	for _, e := range supportedExts {
		if e == ext {
			return true
		}
	}
	return false
}

// Decodable 报告当前实现是否能解码该扩展名。
func Decodable(path string) bool {
	return decodableExts[strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))]
}

// Hash 计算 9×8 dHash。第二个返回值为 false 表示解码失败。
func Hash(path string) (uint64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	img, _, err := decode(f, path)
	if err != nil {
		return 0, false
	}
	orientation := 1
	if isJPEG(path) {
		orientation = jpegOrientation(f)
	}
	px := normalize(img, orientation)
	var h uint64
	bit := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if gray(px.at(x, y)) > gray(px.at(x+1, y)) {
				h |= 1 << uint(bit)
			}
			bit++
		}
	}
	return h, true
}

// Hex 输出 16 位小写十六进制，与 C# ulong.ToString("x16") 对齐。
func Hex(h uint64) string {
	const digits = "0123456789abcdef"
	buf := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		buf[i] = digits[h&0xF]
		h >>= 4
	}
	return string(buf)
}

// Hashed 是一个已计算指纹的候选图片。
type Hashed struct {
	Path string
	Hash uint64
}

// Group 复刻 C# A/Q.cs A_m_1(List<string>, threshold)：
// 贪心锚点 + 组内直径约束，输出成员数 > 1 的组。
func Group(items []Hashed, threshold int) [][]string {
	used := make([]bool, len(items))
	var out [][]string
	for i := 0; i < len(items); i++ {
		if used[i] {
			continue
		}
		used[i] = true
		group := []int{i}
		for j := i + 1; j < len(items); j++ {
			if used[j] || Hamming(items[i].Hash, items[j].Hash) > threshold {
				continue
			}
			ok := true
			for _, k := range group {
				if Hamming(items[k].Hash, items[j].Hash) > threshold {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			group = append(group, j)
			used[j] = true
		}
		if len(group) > 1 {
			paths := make([]string, len(group))
			for k, idx := range group {
				paths[k] = items[idx].Path
			}
			out = append(out, paths)
		}
	}
	return out
}

// Hamming 返回两个指纹的汉明距离。
func Hamming(a, b uint64) int {
	x := a ^ b
	n := 0
	for x != 0 {
		n += int(x & 1)
		x >>= 1
	}
	return n
}
