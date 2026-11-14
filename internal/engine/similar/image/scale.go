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
	"image/png"

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
	case ".png":
		// 先严格解码；失败时按原版 validateImageData:false 的宽松行为，
		// 对截断 PNG 做"已到达的行 + 其余补黑"的部分解码。
		data, err := io.ReadAll(r)
		if err != nil {
			return nil, "", err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err == nil {
			return img, "png", nil
		}
		img, perr := partialPNG(data)
		if perr != nil {
			return nil, "", err
		}
		return img, "png", nil
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

// drawScaled 把 src 缩放到 dstW×dstH 的白底画布。
//
// 实测 GDI+（HighQualityBilinear + PixelOffsetMode.HighQuality）的等效核在缩小、放大
// 两个方向上是同一个三角核：半径 = max(1, 缩放比) 个源像素，目标像素中心取 (x+0.5)×缩放比，
// 源图外按白底参与加权。缩小方向半径 >1（预滤波），放大方向半径固定为 1 个源像素；
// 某一轴恰好 1:1 时只要另一轴发生缩放，GDI+ 仍会走同一条滤波路径，因此该轴也会出现
// 约 1/8 的边缘白渗入（纯黑 8×8 源放大到 9×8 时首末行即为此现象）。
// 只有两轴同时 1:1 才是纯拷贝（对应 C# 第二趟 DrawImage 的直通情形）。
func drawScaled(dstW, dstH int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	if sw <= 0 || sh <= 0 {
		return newRGBImage(dstW, dstH)
	}
	if sw == dstW && sh == dstH {
		out := newRGBImage(dstW, dstH)
		for y := 0; y < dstH; y++ {
			for x := 0; x < dstW; x++ {
				out.set(x, y, src.at(x, y))
			}
		}
		return out
	}
	return resampleY(dstH, midPass(dstW, dstH, src))
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

// tentTaps 计算三角核权重。
// 实测 GDI+ HighQualityBilinear 的等效核在缩小/放大方向相同：支撑宽度 2×半径，
// 半径 = max(1, 缩放比) 个源像素；目标像素中心映射为 (x+0.5)×缩放比（不减半像素）；
// 源图外的部分按白底计。center 是目标像素中心在源轴上的坐标，scale 是源/目标像素比。
func tentTaps(center, scale float64) []tap {
	r := math.Max(1, scale)
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
// 缩放比 sw/dw 可大于 1（缩小）、小于 1（放大）或等于 1（该轴 1:1 但另一轴缩放），
// 三者统一走三角核 + 越界白底。
func resampleX(dw int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	out := newRGBImage(dw, sh)
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

// resampleY 把 src 垂直重采样到 dh 行，宽度不变。核与 resampleX 相同。
func resampleY(dh int, src *rgbImage) *rgbImage {
	sw, sh := src.w, src.h
	out := newRGBImage(sw, dh)
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
