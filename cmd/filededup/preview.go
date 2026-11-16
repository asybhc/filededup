//go:build desktop

package main

// 结果页大图预览：只读本地图片，解码后等比缩到 maxEdge 以内，
// 以 data URL 返回给前端，不写任何临时文件。

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	stdimage "image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	imagesim "filededup/internal/engine/similar/image"
	xdraw "golang.org/x/image/draw"
)

// ImagePreview 是大图预览结果。
type ImagePreview struct {
	DataURL string `json:"dataUrl"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Format  string `json:"format"`
}

// previewFileLimit 是允许预览的最大文件尺寸，避免误点超大图导致内存暴涨。
const previewFileLimit = 200 << 20

// ReadImagePreview 解码图片并缩放；maxEdge <= 0 时按 1600 处理。
func (a *App) ReadImagePreview(path string, maxEdge int) (ImagePreview, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return ImagePreview{}, errors.New("未提供图片路径")
	}
	info, err := os.Stat(p)
	if err != nil {
		return ImagePreview{}, fmt.Errorf("图片不可读：%w", err)
	}
	if info.IsDir() {
		return ImagePreview{}, errors.New("目标是文件夹，不是图片")
	}
	if info.Size() > previewFileLimit {
		return ImagePreview{}, fmt.Errorf("图片超过 %d MB，已拒绝预览", previewFileLimit>>20)
	}
	img, format, err := imagesim.Load(p)
	if err != nil {
		return ImagePreview{}, fmt.Errorf("无法解码图片：%w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return ImagePreview{}, errors.New("图片尺寸为空")
	}
	if maxEdge <= 0 {
		maxEdge = 1600
	}
	out := img
	if w > maxEdge || h > maxEdge {
		scale := float64(maxEdge) / float64(w)
		if h > w {
			scale = float64(maxEdge) / float64(h)
		}
		dw := int(float64(w) * scale)
		dh := int(float64(h) * scale)
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
		dst := stdimage.NewRGBA(stdimage.Rect(0, 0, dw, dh))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
		out = dst
	}
	var buf bytes.Buffer
	ext := strings.ToLower(filepath.Ext(p))
	switch {
	case ext == ".jpg" || ext == ".jpeg" || format == "jpeg":
		if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 88}); err != nil {
			return ImagePreview{}, err
		}
	default:
		if err := png.Encode(&buf, out); err != nil {
			return ImagePreview{}, err
		}
	}
	media := "image/png"
	if ext == ".jpg" || ext == ".jpeg" || format == "jpeg" {
		media = "image/jpeg"
	}
	return ImagePreview{
		DataURL: "data:" + media + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:   w,
		Height:  h,
		Format:  format,
	}, nil
}
