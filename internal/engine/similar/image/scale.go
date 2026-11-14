package image

import (
	"bytes"
	"image"
	"image/color"
	"io"
	"math"
	"path/filepath"
	"strings"

	// 注册标准库解码器；gif 取首帧。
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// 纯 Go 的官方扩展解码器（不引入 cgo）：标准库 image/* 不含 TIFF/WebP。
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

func isJPEG(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".jpg" || ext == ".jpeg"
}

// decode 按扩展名选择解码器。
// BMP 使用本包实现（标准库不含 BMP）；TIFF/WebP 使用 golang.org/x/image 的纯 Go 解码器。
func decode(r io.Reader, path string) (image.Image, string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bmp":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, "", err
		}
		img, err := decodeBMPBytes(data)
		return img, "bmp", err
	case ".tif", ".tiff":
		// 先读入内存：TIFF 解码器需要可 Seek 的读取器。
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, "", err
		}
		img, err := tiff.Decode(bytes.NewReader(data))
		return img, "tiff", err
	case ".webp":
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, "", err
		}
		img, err := webp.Decode(bytes.NewReader(data))
		return img, "webp", err
	}
	return image.Decode(r)
}

// rgb 是 0..255 的直通颜色（已合成到白底）。
type rgb struct {
	r, g, b float64
}

func (c rgb) gray() int {
	return int((c.r*299 + c.g*587 + c.b*114) / 1000)
}

func gray(c rgb) int { return c.gray() }

// rgbImage 是白底合成后的中间画布。
type rgbImage struct {
	w, h int
	px   []rgb
}

func newRGBImage(w, h int) *rgbImage {
	img := &rgbImage{w: w, h: h, px: make([]rgb, w*h)}
	for i := range img.px {
		img.px[i] = rgb{255, 255, 255}
	}
	return img
}

func (m *rgbImage) at(x, y int) rgb { return m.px[y*m.w+x] }

func (m *rgbImage) set(x, y int, c rgb) { m.px[y*m.w+x] = c }

// sampler 是可被重采样的图像抽象。
type sampler interface {
	bounds() (w, h int)
	at(x, y int) rgb
}

type goImage struct{ img image.Image }

func (g goImage) bounds() (int, int) { return g.img.Bounds().Dx(), g.img.Bounds().Dy() }

// at 取源像素并合成到白底。使用 NRGBA 避免预乘 alpha 带来的暗边。
func (g goImage) at(x, y int) rgb {
	b := g.img.Bounds()
	c := color.NRGBAModel.Convert(g.img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
	return over(c)
}

func over(c color.NRGBA) rgb {
	a := float64(c.A) / 255
	return rgb{
		r: float64(c.R)*a + 255*(1-a),
		g: float64(c.G)*a + 255*(1-a),
		b: float64(c.B)*a + 255*(1-a),
	}
}

func (m *rgbImage) bounds() (int, int) { return m.w, m.h }

// 白边系数：实测 GDI+ 对"放大轴"的最外 1 像素叠加大约 1/7.75 的白底
// （100 灰的纯色图放大后边为 120 左右）。
// 缩小轴不需要人工白边：三角核在源图边界外按白底取样，天然产生同样的白渗入。
const edgeWhite = 0.129

// drawScaled 把 src 缩放到 dstW×dstH 的白底画布。
// 逐轴选择算法：缩小轴用展宽三角核（GDI+ 的预滤波行为），放大轴用双线性。
// 放大轴的最外 1 像素叠加白底，复现 GDI+ 的边缘抗锯齿；缩小轴由核本身处理。
func drawScaled(dstW, dstH int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	if sw <= 0 || sh <= 0 {
		return newRGBImage(dstW, dstH)
	}
	dst := resampleY(dstH, midPass(dstW, dstH, src))
	upX := sw < dstW
	upY := sh < dstH
	if !upX && !upY {
		return dst
	}
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			c := dst.at(x, y)
			if upX && (x == 0 || x == dstW-1) {
				c = overWhite(c, edgeWhite)
			}
			if upY && (y == 0 || y == dstH-1) {
				c = overWhite(c, edgeWhite)
			}
			dst.set(x, y, c)
		}
	}
	return dst
}

func overWhite(c rgb, t float64) rgb {
	return rgb{c.r + (255-c.r)*t, c.g + (255-c.g)*t, c.b + (255-c.b)*t}
}

// midPass 执行水平重采样，并在"两轴同时缩小"时把中间结果取整到 8 位。
// GDI+ 的两趟缩放在两趟之间落一块 8 位缓冲，这一步取整会改变少数临界像素的
// 灰度比较结果（实测：不取整时纯竖向渐变会翻转 1 位）。只有两轴都缩小时中间
// 结果才有非整数值、且不会被随后的输出取整覆盖，因此只在该情形下取整。
func midPass(dstW, dstH int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	mid := resampleX(dstW, src)
	if sw > dstW && sh > dstH {
		for i := range mid.px {
			mid.px[i] = quantize(mid.px[i])
		}
	}
	return mid
}

// tap 是三角核在某个源下标上的权重。
type tap struct {
	i int
	w float64
}

// tentTaps 计算缩小方向的三角核权重。
// 实测 GDI+ HighQualityBilinear 在缩小方向的等效核是"支撑宽度 2×缩放比"的三角核，
// 目标像素中心映射为 (x+0.5)×缩放比（不减半像素）；源图外的部分按白底计。
// center 是目标像素中心在源轴上的坐标，scale 是源/目标像素比（>1）。
func tentTaps(center, scale float64) []tap {
	r := scale
	lo := center - r
	hi := center + r
	i0 := int(math.Floor(lo))
	i1 := int(math.Ceil(hi))
	taps := make([]tap, 0, i1-i0)
	for i := i0; i < i1; i++ {
		a := math.Max(lo, float64(i))
		b := math.Min(hi, float64(i+1))
		if b <= a {
			continue
		}
		var area float64
		if center > a && center < b {
			// 峰值落在该源像素内，拆成两段梯形分别积分。
			area = tentSegment(a, center, center, r) + tentSegment(center, b, center, r)
		} else {
			area = tentSegment(a, b, center, r)
		}
		if area > 0 {
			taps = append(taps, tap{i, area})
		}
	}
	return taps
}

// tentSegment 计算三角核在 [a,b] 上的精确积分（线性函数用梯形公式即可）。
func tentSegment(a, b, center, r float64) float64 {
	w0 := 1 - math.Abs(a-center)/r
	w1 := 1 - math.Abs(b-center)/r
	return (w0 + w1) / 2 * (b - a)
}

// resampleX 把 src 水平重采样到 dw 列，高度不变。
func resampleX(dw int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	out := newRGBImage(dw, sh)
	if dw == sw {
		for y := 0; y < sh; y++ {
			for x := 0; x < dw; x++ {
				out.set(x, y, src.at(x, y))
			}
		}
		return out
	}
	if dw > sw {
		scale := float64(sw) / float64(dw)
		for y := 0; y < sh; y++ {
			for x := 0; x < dw; x++ {
				f := (float64(x)+0.5)*scale - 0.5
				i0 := int(math.Floor(f))
				t := f - float64(i0)
				if i0 < 0 {
					i0, t = 0, 0
				}
				if i0 >= sw-1 {
					i0, t = sw-1, 0
				}
				i1 := i0 + 1
				if i1 >= sw {
					i1 = sw - 1
				}
				out.set(x, y, mix(src.at(i0, y), src.at(i1, y), t))
			}
		}
		return out
	}
	scale := float64(sw) / float64(dw)
	cols := make([][]tap, dw)
	for x := 0; x < dw; x++ {
		cols[x] = tentTaps((float64(x)+0.5)*scale, scale)
	}
	for y := 0; y < sh; y++ {
		for x := 0; x < dw; x++ {
			var r, g, b, total float64
			for _, t := range cols[x] {
				c := rgb{255, 255, 255}
				if t.i >= 0 && t.i < sw {
					c = src.at(t.i, y)
				}
				r += c.r * t.w
				g += c.g * t.w
				b += c.b * t.w
				total += t.w
			}
			if total <= 0 {
				out.set(x, y, rgb{255, 255, 255})
				continue
			}
			out.set(x, y, rgb{r / total, g / total, b / total})
		}
	}
	return out
}

// resampleY 把 src 垂直重采样到 dh 行，宽度不变。
func resampleY(dh int, src *rgbImage) *rgbImage {
	sw, sh := src.w, src.h
	out := newRGBImage(sw, dh)
	if dh == sh {
		for y := 0; y < dh; y++ {
			for x := 0; x < sw; x++ {
				out.set(x, y, src.at(x, y))
			}
		}
		return out
	}
	if dh > sh {
		scale := float64(sh) / float64(dh)
		for y := 0; y < dh; y++ {
			f := (float64(y)+0.5)*scale - 0.5
			j0 := int(math.Floor(f))
			t := f - float64(j0)
			if j0 < 0 {
				j0, t = 0, 0
			}
			if j0 >= sh-1 {
				j0, t = sh-1, 0
			}
			j1 := j0 + 1
			if j1 >= sh {
				j1 = sh - 1
			}
			for x := 0; x < sw; x++ {
				out.set(x, y, mix(src.at(x, j0), src.at(x, j1), t))
			}
		}
		return out
	}
	scale := float64(sh) / float64(dh)
	rows := make([][]tap, dh)
	for y := 0; y < dh; y++ {
		rows[y] = tentTaps((float64(y)+0.5)*scale, scale)
	}
	for y := 0; y < dh; y++ {
		for x := 0; x < sw; x++ {
			var r, g, b, total float64
			for _, t := range rows[y] {
				c := rgb{255, 255, 255}
				if t.i >= 0 && t.i < sh {
					c = src.at(x, t.i)
				}
				r += c.r * t.w
				g += c.g * t.w
				b += c.b * t.w
				total += t.w
			}
			if total <= 0 {
				out.set(x, y, rgb{255, 255, 255})
				continue
			}
			out.set(x, y, rgb{r / total, g / total, b / total})
		}
	}
	return out
}

// mix 在两种颜色间线性插值。
func mix(a, b rgb, t float64) rgb {
	return rgb{
		r: a.r + (b.r-a.r)*t,
		g: a.g + (b.g-a.g)*t,
		b: a.b + (b.b-a.b)*t,
	}
}

// normalize 复刻 C# A/Q.cs A_m_1(Image, 9, 8)：
// Orientation 5-8 时先按行列互换的尺寸绘制，再旋转/镜像回正，最后统一绘制到 9×8。
func normalize(img image.Image, orientation int) *rgbImage {
	const dstW, dstH = 9, 8
	swap := orientation >= 5
	w, h := dstW, dstH
	if swap {
		w, h = dstH, dstW
	}
	first := drawScaled(w, h, goImage{img})
	if orientation > 1 {
		first = rotateFlip(first, orientation)
	}
	out := drawScaled(dstW, dstH, first)
	// GDI+ 全程使用 8 位整数通道；这里把浮点结果就近取整，避免 254.9999… 被
	// 灰度公式截断成 254，从而在纯色图等场景制造出与原版不同的指纹位。
	for i := range out.px {
		out.px[i] = quantize(out.px[i])
	}
	return out
}

// quantize 把浮点通道四舍五入回 8 位整数。
func quantize(c rgb) rgb {
	return rgb{math.Round(c.r), math.Round(c.g), math.Round(c.b)}
}

// rotateFlip 复刻 GDI+ RotateFlipType 的 7 种变换。
func rotateFlip(src *rgbImage, orientation int) *rgbImage {
	switch orientation {
	case 2: // RotateNoneFlipX
		return flipX(src)
	case 3: // Rotate180FlipNone
		return flipY(flipX(src))
	case 4: // RotateNoneFlipY
		return flipY(src)
	case 5: // Rotate90FlipX
		return flipX(rotate90(src))
	case 6: // Rotate90FlipNone
		return rotate90(src)
	case 7: // Rotate270FlipX
		return flipX(rotate270(src))
	case 8: // Rotate270FlipNone
		return rotate270(src)
	}
	return src
}

func flipX(src *rgbImage) *rgbImage {
	dst := newRGBImage(src.w, src.h)
	for y := 0; y < src.h; y++ {
		for x := 0; x < src.w; x++ {
			dst.set(src.w-1-x, y, src.at(x, y))
		}
	}
	return dst
}

func flipY(src *rgbImage) *rgbImage {
	dst := newRGBImage(src.w, src.h)
	for y := 0; y < src.h; y++ {
		for x := 0; x < src.w; x++ {
			dst.set(x, src.h-1-y, src.at(x, y))
		}
	}
	return dst
}

func rotate90(src *rgbImage) *rgbImage {
	dst := newRGBImage(src.h, src.w)
	for y := 0; y < src.h; y++ {
		for x := 0; x < src.w; x++ {
			dst.set(src.h-1-y, x, src.at(x, y))
		}
	}
	return dst
}

func rotate270(src *rgbImage) *rgbImage {
	dst := newRGBImage(src.h, src.w)
	for y := 0; y < src.h; y++ {
		for x := 0; x < src.w; x++ {
			dst.set(y, src.w-1-x, src.at(x, y))
		}
	}
	return dst
}
