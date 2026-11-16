package image

// Load 解码整张图片并按 EXIF 方向校正，供界面预览使用。
//
// 与指纹路径共享同一套解码器（含截断 PNG 的宽松解码），但返回完整图像而不是 9×8 画布；
// 方向校正按 EXIF 1-8 做整图像素重排。

import (
	"image"
	"os"
)

// Load 返回已按 EXIF 方向校正的图像与格式名。
func Load(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := decode(f, path)
	if err != nil {
		return nil, "", err
	}
	orientation := 1
	if isJPEG(path) {
		orientation = jpegOrientation(f)
	}
	if orientation > 1 {
		img = applyOrientation(img, orientation)
	}
	return img, format, nil
}

func applyOrientation(src image.Image, orientation int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	swap := orientation >= 5
	dstW, dstH := w, h
	if swap {
		dstW, dstH = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			var sx, sy int
			switch orientation {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			default:
				sx, sy = x, y
			}
			dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}
