package image

import (
	"encoding/binary"
	"errors"
	"image"
)

var errBMP = errors.New("image: 不支持的 BMP 变体")

// decodeBMPBytes 解析未压缩 BMP（BI_RGB，24/32/8/4/1 位）。
// 原版由 GDI+ 解码，这里覆盖常见静态位图；不支持的变体返回错误，由上层登记为解码失败。
func decodeBMPBytes(data []byte) (image.Image, error) {
	if len(data) < 54 || data[0] != 'B' || data[1] != 'M' {
		return nil, errBMP
	}
	offBits := int(binary.LittleEndian.Uint32(data[10:14]))
	hdrSize := int(binary.LittleEndian.Uint32(data[14:18]))
	if hdrSize < 40 || len(data) < 14+hdrSize {
		return nil, errBMP
	}
	width := int(int32(binary.LittleEndian.Uint32(data[18:22])))
	heightRaw := int(int32(binary.LittleEndian.Uint32(data[22:26])))
	planes := binary.LittleEndian.Uint16(data[26:28])
	bpp := int(binary.LittleEndian.Uint16(data[28:30]))
	compression := binary.LittleEndian.Uint32(data[30:34])
	if width <= 0 || heightRaw == 0 || planes != 1 {
		return nil, errBMP
	}
	if compression != 0 {
		return nil, errBMP
	}
	topDown := heightRaw < 0
	height := heightRaw
	if topDown {
		height = -heightRaw
	}
	stride := ((width*bpp + 31) / 32) * 4

	var palette []byte
	if bpp <= 8 {
		n := int(binary.LittleEndian.Uint32(data[46:50]))
		if n == 0 {
			n = 1 << uint(bpp)
		}
		start := 14 + hdrSize
		if start+n*4 > len(data) {
			return nil, errBMP
		}
		palette = data[start : start+n*4]
	}

	if offBits < 0 || offBits+stride*height > len(data) {
		return nil, errBMP
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for row := 0; row < height; row++ {
		srcRow := row
		if !topDown {
			srcRow = height - 1 - row
		}
		line := data[offBits+srcRow*stride:]
		dst := img.Pix[row*img.Stride:]
		for x := 0; x < width; x++ {
			var r, g, b byte
			switch bpp {
			case 32:
				b = line[x*4]
				g = line[x*4+1]
				r = line[x*4+2]
			case 24:
				b = line[x*3]
				g = line[x*3+1]
				r = line[x*3+2]
			case 8, 4, 1:
				idx := 0
				switch bpp {
				case 8:
					idx = int(line[x])
				case 4:
					idx = int(line[x/2])
					if x%2 == 0 {
						idx >>= 4
					} else {
						idx &= 0xF
					}
				case 1:
					idx = int(line[x/8])
					idx = (idx >> uint(7-x%8)) & 1
				}
				if idx*4+2 >= len(palette) {
					return nil, errBMP
				}
				b = palette[idx*4]
				g = palette[idx*4+1]
				r = palette[idx*4+2]
			default:
				return nil, errBMP
			}
			dst[x*4] = r
			dst[x*4+1] = g
			dst[x*4+2] = b
			dst[x*4+3] = 0xFF
		}
	}
	return img, nil
}
