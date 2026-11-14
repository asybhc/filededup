package image

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"image"
	"image/color"
	"io"
)

// partialPNG 尽力解码一个被截断的 PNG：解析已完整到达的 chunk（不校验 CRC、不要求 IEND），
// 对 IDAT 做"能解多少解多少"的 zlib 解压，只采用完整的扫描行，其余行按不透明黑填充。
//
// 这是为了对齐原版 System.Drawing.Image.FromStream(validateImageData:false) 的宽松行为：
// 实测原版对截断 PNG 仍会出指纹，且截断点之后按黑处理。
//
// 仅支持非隔行、8 位深、颜色类型 0/2/4/6（灰度/RGB/灰度+Alpha/RGBA）。
// 其他情形返回错误，由调用方按"解码失败"处理——不做近似或猜测。
func partialPNG(data []byte) (image.Image, error) {
	if len(data) < 33 || string(data[1:4]) != "PNG" {
		return nil, io.ErrUnexpectedEOF
	}
	var w, h int
	depth, ctype := 0, 0
	interlaced := false
	var idat []byte
	for off := 8; off+8 <= len(data); {
		n := int(binary.BigEndian.Uint32(data[off:]))
		typ := string(data[off+4 : off+8])
		body := off + 8
		end := body + n
		if end > len(data) {
			end = len(data) // chunk 被截断：只取现有字节
		}
		switch typ {
		case "IHDR":
			if end-body < 13 {
				return nil, io.ErrUnexpectedEOF
			}
			w = int(binary.BigEndian.Uint32(data[body:]))
			h = int(binary.BigEndian.Uint32(data[body+4:]))
			depth = int(data[body+8])
			ctype = int(data[body+9])
			interlaced = data[body+12] != 0
		case "IDAT":
			idat = append(idat, data[body:end]...)
		}
		next := body + n + 4
		if next > len(data) {
			break
		}
		off = next
	}
	channels, ok := map[int]int{0: 1, 2: 3, 4: 2, 6: 4}[ctype]
	if !ok || w <= 0 || h <= 0 || depth != 8 || interlaced || len(idat) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	zr, err := zlib.NewReader(bytes.NewReader(idat))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	raw, _ := io.ReadAll(zr) // 截断流：忽略错误，保留已解出的字节

	stride := w * channels
	rows := len(raw) / (stride + 1)
	if rows > h {
		rows = h
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255 // 未解出的行按不透明黑填充（实测 GDI+ 行为）
	}
	prev := make([]byte, stride)
	cur := make([]byte, stride)
	for y := 0; y < rows; y++ {
		line := raw[y*(stride+1):]
		f := line[0]
		copy(cur, line[1:1+stride])
		unfilter(f, cur, prev, channels)
		for x := 0; x < w; x++ {
			p := cur[x*channels:]
			switch ctype {
			case 0:
				img.SetNRGBA(x, y, color.NRGBA{R: p[0], G: p[0], B: p[0], A: 255})
			case 2:
				img.SetNRGBA(x, y, color.NRGBA{R: p[0], G: p[1], B: p[2], A: 255})
			case 4:
				img.SetNRGBA(x, y, color.NRGBA{R: p[0], G: p[0], B: p[0], A: p[1]})
			case 6:
				img.SetNRGBA(x, y, color.NRGBA{R: p[0], G: p[1], B: p[2], A: p[3]})
			}
		}
		prev, cur = cur, prev
	}
	return img, nil
}

// unfilter 就地还原 PNG 扫描行的 5 种过滤（None/Sub/Up/Average/Paeth）。
func unfilter(f byte, cur, prev []byte, bpp int) {
	for i := range cur {
		var a, b, c int
		if i >= bpp {
			a = int(cur[i-bpp])
		}
		if prev != nil {
			b = int(prev[i])
			if i >= bpp {
				c = int(prev[i-bpp])
			}
		}
		switch f {
		case 1:
			cur[i] += byte(a)
		case 2:
			cur[i] += byte(b)
		case 3:
			cur[i] += byte((a + b) / 2)
		case 4:
			cur[i] += byte(paeth(a, b, c))
		}
	}
}

func paeth(a, b, c int) int {
	p := a + b - c
	pa, pb, pc := abs(p-a), abs(p-b), abs(p-c)
	if pa <= pb && pa <= pc {
		return a
	}
	if pb <= pc {
		return b
	}
	return c
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
