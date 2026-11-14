// Package image 实现相似图片判定：9×8 dHash 感知哈希与相似组聚类。
//
// 算法逐条复刻 C# 基线 A/Q.cs：
//   - 先按 EXIF Orientation(274) 归一，再缩放到 9×8 白底 24 位画布（双线性、高质量）。
//   - 8 行 × 8 列，灰度按 (R*299+G*587+B*114)/1000 取整，左侧灰度大于右侧时置位；
//     bit = row*8+col。
//   - 分组：贪心锚点 + 组内直径约束（与锚点及组内每个已加入成员的海明距离都 <= 阈值）。
//
// 与原版的差异（M3 已登记）：JPEG/PNG/GIF/BMP/TIFF 逐位对齐 C#；
// WebP 用纯 Go 解码器（原版 GDI+ 不支持，实测 C# 对 webp 一律解码失败），
// 因此 WebP 属于 Go 侧超集能力，不参与与 C# 的指纹对拍。
package image

import (
	"os"
	"path/filepath"
	"strings"
)

// supportedExts 是参与相似图片判定的扩展名清单（与原版一致）。
// 全部格式均可解码：jpg/jpeg/png/gif/bmp 走标准库或本包实现，tif/tiff/webp 走 golang.org/x/image。
var supportedExts = []string{"jpg", "jpeg", "png", "gif", "bmp", "tif", "tiff", "webp"}

// Supported 报告扩展名是否在原版图片支持清单内（大小写不敏感）。
func Supported(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	for _, e := range supportedExts {
		if e == ext {
			return true
		}
	}
	return false
}

// Fingerprint 是一张图片的指纹与退化标记。
type Fingerprint struct {
	Hash uint64
	// Uniform 为 true 表示归一化画布去掉 1 像素白边后，内部各像素灰度几乎相同
	// （极差 <= 1），即这张图没有任何行内明暗变化。纯色图、纯色条都属于这一类，
	// 它们的 dHash 位由实现细节（白边、浮点截断）决定，彼此无关的图会得到相同指纹。
	Uniform bool
}

// Hash 计算 9×8 dHash。第二个返回值为 false 表示解码失败。
func Hash(path string) (uint64, bool) {
	fp, ok := Compute(path)
	return fp.Hash, ok
}

// Compute 计算指纹，并判定是否为退化（均匀）指纹。
func Compute(path string) (Fingerprint, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Fingerprint{}, false
	}
	defer f.Close()
	img, _, err := decode(f, path)
	if err != nil {
		return Fingerprint{}, false
	}
	orientation := 1
	if isJPEG(path) {
		orientation = jpegOrientation(f)
	}
	px := normalize(img, orientation)
	var fp Fingerprint
	bit := 0
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if gray(px.at(x, y)) > gray(px.at(x+1, y)) {
				fp.Hash |= 1 << uint(bit)
			}
			bit++
		}
	}
	fp.Uniform = uniformCanvas(px)
	return fp, true
}

// uniformCanvas 判断归一化画布内部（去掉人为叠加的白边）是否几乎没有灰度变化。
// 白边是 1 像素宽的外框，会制造出与实际内容无关的固定梯度，因此不在判定范围内。
func uniformCanvas(px *rgbImage) bool {
	lo, hi := 255, 0
	for y := 1; y < px.h-1; y++ {
		for x := 1; x < px.w-1; x++ {
			g := gray(px.at(x, y))
			if g < lo {
				lo = g
			}
			if g > hi {
				hi = g
			}
		}
	}
	if hi == 0 && lo == 255 { // 画布过小、没有内部像素
		return true
	}
	return hi-lo <= 1
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
