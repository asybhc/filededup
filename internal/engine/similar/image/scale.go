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

// 白边系数：实测 GDI+ 对每个被缩放的轴，最外 1 像素叠加大约 1/7.75 的白底
// （100 灰的纯色图缩到 9×8 后边为 120，内部仍为 100）。
const edgeWhite = 0.129

// drawScaled 把 src 缩放到 dstW×dstH 的白底画布。
// 逐轴选择算法：缩小的轴用面积平均（GDI+ 的预滤波行为），放大的轴用双线性。
// 任一轴发生缩放时，该轴最外 1 像素叠加白底，复现 GDI+ 的边缘抗锯齿。
func drawScaled(dstW, dstH int, src sampler) *rgbImage {
	sw, sh := src.bounds()
	if sw <= 0 || sh <= 0 {
		return newRGBImage(dstW, dstH)
	}
	dst := resampleY(dstH, resampleX(dstW, src))
	scaleX := sw != dstW
	scaleY := sh != dstH
	if !scaleX && !scaleY {
		return dst
	}
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			c := dst.at(x, y)
			if scaleX && (x == 0 || x == dstW-1) {
				c = overWhite(c, edgeWhite)
			}
			if scaleY && (y == 0 || y == dstH-1) {
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
	if dw < sw {
		for y := 0; y < sh; y++ {
			for x := 0; x < dw; x++ {
				start := float64(x) * float64(sw) / float64(dw)
				end := float64(x+1) * float64(sw) / float64(dw)
				out.set(x, y, rangeAverage(src, y, start, end))
			}
		}
		return out
	}
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
	if dh < sh {
		for y := 0; y < dh; y++ {
			start := float64(y) * float64(sh) / float64(dh)
			end := float64(y+1) * float64(sh) / float64(dh)
			j0 := int(math.Floor(start))
			j1 := int(math.Ceil(end))
			if j0 < 0 {
				j0 = 0
			}
			if j1 > sh {
				j1 = sh
			}
			for x := 0; x < sw; x++ {
				var r, g, b, total float64
				for j := j0; j < j1; j++ {
					w := math.Min(end, float64(j+1)) - math.Max(start, float64(j))
					if w <= 0 {
						continue
					}
					c := src.at(x, j)
					r += c.r * w
					g += c.g * w
					b += c.b * w
					total += w
				}
				if total <= 0 {
					continue
				}
				out.set(x, y, rgb{r / total, g / total, b / total})
			}
		}
		return out
	}
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

// mix 在两种颜色间线性插值。
func mix(a, b rgb, t float64) rgb {
	return rgb{
		r: a.r + (b.r-a.r)*t,
		g: a.g + (b.g-a.g)*t,
		b: a.b + (b.b-a.b)*t,
	}
}

func rangeAverage(src sampler, y int, start, end float64) rgb {
	sw, _ := src.bounds()
	var r, g, b, total float64
	i0 := int(math.Floor(start))
	i1 := int(math.Ceil(end))
	if i0 < 0 {
		i0 = 0
	}
	if i1 > sw {
		i1 = sw
	}
	for i := i0; i < i1; i++ {
		w := math.Min(end, float64(i+1)) - math.Max(start, float64(i))
		if w <= 0 {
			continue
		}
		c := src.at(i, y)
		r += c.r * w
		g += c.g * w
		b += c.b * w
		total += w
	}
	if total <= 0 {
		return rgb{255, 255, 255}
	}
	return rgb{r / total, g / total, b / total}
}

// bilinearScale 按目标像素中心映射做双线性插值（用于放大或单轴放大）。
func bilinearScale(dst *rgbImage, src sampler) {
	sw, sh := src.bounds()
	sx := float64(sw) / float64(dst.w)
	sy := float64(sh) / float64(dst.h)
	for y := 0; y < dst.h; y++ {
		fy := (float64(y)+0.5)*sy - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		if y0 < 0 {
			y0, wy = 0, 0
		}
		if y0 >= sh-1 {
			y0, wy = sh-1, 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		for x := 0; x < dst.w; x++ {
			fx := (float64(x)+0.5)*sx - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			if x0 < 0 {
				x0, wx = 0, 0
			}
			if x0 >= sw-1 {
				x0, wx = sw-1, 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			c00 := src.at(x0, y0)
			c10 := src.at(x1, y0)
			c01 := src.at(x0, y1)
			c11 := src.at(x1, y1)
			blend := func(a, b, t float64) float64 { return a + (b-a)*t }
			dst.set(x, y, rgb{
				r: blend(blend(c00.r, c10.r, wx), blend(c01.r, c11.r, wx), wy),
				g: blend(blend(c00.g, c10.g, wx), blend(c01.g, c11.g, wx), wy),
				b: blend(blend(c00.b, c10.b, wx), blend(c01.b, c11.b, wx), wy),
			})
		}
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
	return drawScaled(dstW, dstH, first)
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
