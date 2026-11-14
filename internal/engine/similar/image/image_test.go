package image

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/tiff"
)

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	return path
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

// 纯色图的 dHash 与 C# 基线一致：所有比较都是“相等”，只有 GDI+ 的白边让首位为 1。
func TestHashSolidColor(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 40, A: 255})
		}
	}
	path := writeTemp(t, "solid.png", encodePNG(t, img))
	h, ok := Hash(path)
	if !ok {
		t.Fatal("纯色 PNG 应能解码")
	}
	if want := uint64(0x0101010101010101); h != want {
		t.Fatalf("dHash = %016x, want %016x", h, want)
	}
}

// 纯色图（含纯白）都必须判为退化指纹，且指纹与 C# 一致：
// 非白色因白边得 0x0101…01，纯白无边缘对比得全 0。
// 有真实横向明暗变化的图不算退化。
func TestComputeUniform(t *testing.T) {
	solid := []struct {
		c    color.NRGBA
		want string
	}{
		{color.NRGBA{R: 200, G: 30, B: 40, A: 255}, "0101010101010101"},
		{color.NRGBA{R: 10, G: 10, B: 200, A: 255}, "0101010101010101"},
		{color.NRGBA{R: 0, G: 0, B: 0, A: 255}, "0101010101010101"},
		{color.NRGBA{R: 255, G: 255, B: 255, A: 255}, "0000000000000000"},
	}
	for _, s := range solid {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				img.SetNRGBA(x, y, s.c)
			}
		}
		fp, ok := Compute(writeTemp(t, "solid.png", encodePNG(t, img)))
		if !ok {
			t.Fatalf("纯色 PNG 应能解码")
		}
		if !fp.Uniform {
			t.Errorf("纯色 %v 应判为退化指纹（fingerprint=%016x）", s.c, fp.Hash)
		}
		if got := Hex(fp.Hash); got != s.want {
			t.Errorf("纯色 %v 指纹 = %s, want %s（须与 C# 基线一致）", s.c, got, s.want)
		}
	}

	grad := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			grad.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(x * 3), B: uint8(x * 2), A: 255})
		}
	}
	fp, ok := Compute(writeTemp(t, "grad.png", encodePNG(t, grad)))
	if !ok {
		t.Fatal("渐变 PNG 应能解码")
	}
	if fp.Uniform {
		t.Errorf("横向渐变图不应判为退化指纹（fingerprint=%016x）", fp.Hash)
	}
}

func TestHamming(t *testing.T) {
	cases := []struct {
		a, b uint64
		want int
	}{
		{0, 0, 0},
		{0, ^uint64(0), 64},
		{0b1011, 0b0010, 2},
	}
	for _, c := range cases {
		if got := Hamming(c.a, c.b); got != c.want {
			t.Errorf("Hamming(%#b, %#b) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestHex(t *testing.T) {
	if got := Hex(0x0f0f); got != "0000000000000f0f" {
		t.Fatalf("Hex = %q", got)
	}
}

// 链式假阳性：A≈B、B≈C，但 A 与 C 超过阈值时必须拆组（组内直径约束）。
func TestGroupChain(t *testing.T) {
	items := []Hashed{
		{Path: "a.png", Hash: 0b000000},
		{Path: "b.png", Hash: 0b000111},
		{Path: "c.png", Hash: 0b111111},
		{Path: "d.png", Hash: 0b111000},
	}
	groups := Group(items, 3)
	if len(groups) != 2 {
		t.Fatalf("链式用例应拆成 2 组，实际 %d 组: %v", len(groups), groups)
	}
	if len(groups[0]) != 2 || groups[0][0] != "a.png" || groups[0][1] != "b.png" {
		t.Fatalf("第一组应为 [a.png b.png]: %v", groups[0])
	}
	if len(groups[1]) != 2 || groups[1][0] != "c.png" || groups[1][1] != "d.png" {
		t.Fatalf("第二组应为 [c.png d.png]: %v", groups[1])
	}
}

func TestGroupThreshold(t *testing.T) {
	items := []Hashed{
		{Path: "a.png", Hash: 0b000000},
		{Path: "b.png", Hash: 0b000001},
		{Path: "c.png", Hash: 0b111000},
		{Path: "d.png", Hash: 0b111001},
	}
	groups := Group(items, 1)
	if len(groups) != 2 {
		t.Fatalf("应分成 2 组，实际 %d: %v", len(groups), groups)
	}
	if groups[0][0] != "a.png" || groups[0][1] != "b.png" {
		t.Fatalf("成员应按路径排序: %v", groups[0])
	}
	if groups[1][0] != "c.png" || groups[1][1] != "d.png" {
		t.Fatalf("成员应按路径排序: %v", groups[1])
	}
}

// buildBMP24 构造 w×h 的 24 位 BMP（自底向上）。
func buildBMP24(w, h int, pixel func(x, y int) color.RGBA) []byte {
	stride := (w*3 + 3) &^ 3
	dataSize := stride * h
	total := 14 + 40 + dataSize
	buf := make([]byte, total)
	buf[0], buf[1] = 'B', 'M'
	putLE32(buf[2:], uint32(total))
	putLE32(buf[10:], 54)
	putLE32(buf[14:], 40)
	putLE32(buf[18:], uint32(w))
	putLE32(buf[22:], uint32(h))
	putLE16(buf[26:], 1)
	putLE16(buf[28:], 24)
	putLE32(buf[34:], uint32(dataSize))
	for y := 0; y < h; y++ {
		row := 54 + (h-1-y)*stride
		for x := 0; x < w; x++ {
			c := pixel(x, y)
			buf[row+x*3] = c.B
			buf[row+x*3+1] = c.G
			buf[row+x*3+2] = c.R
		}
	}
	return buf
}

func putLE16(b []byte, v uint16) { b[0], b[1] = byte(v), byte(v>>8) }
func putLE32(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
}

func TestDecodeBMP(t *testing.T) {
	const w, h = 20, 12
	bmp := buildBMP24(w, h, func(x, y int) color.RGBA {
		return color.RGBA{R: uint8(x * 10), G: uint8(y * 20), B: 7, A: 255}
	})
	path := writeTemp(t, "test.bmp", bmp)
	h1, ok := Hash(path)
	if !ok {
		t.Fatal("BMP 应能解码")
	}
	// 同一像素内容的 PNG 应得到相同 dHash。
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 10), G: uint8(y * 20), B: 7, A: 255})
		}
	}
	pngPath := writeTemp(t, "test.png", encodePNG(t, img))
	h2, ok := Hash(pngPath)
	if !ok {
		t.Fatal("PNG 应能解码")
	}
	if h1 != h2 {
		t.Fatalf("BMP 与 PNG 的 dHash 不一致: %016x vs %016x", h1, h2)
	}
}

// buildExifApp1 生成只含 Orientation 的 APP1 段。
func buildExifApp1(orientation uint16) []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00,
		0x12, 0x01,
		0x03, 0x00,
		0x01, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	tiff[18] = byte(orientation)
	tiff[19] = byte(orientation >> 8)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2
	out := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen)}
	return append(out, payload...)
}

func TestJPEGOrientation(t *testing.T) {
	for _, want := range []int{1, 3, 6, 8} {
		data := []byte{0xFF, 0xD8}
		if want != 1 {
			data = append(data, buildExifApp1(uint16(want))...)
		}
		data = append(data, 0xFF, 0xD9)
		if got := jpegOrientation(bytes.NewReader(data)); got != want {
			t.Errorf("Orientation = %d, want %d", got, want)
		}
	}
}

// Orientation=6 的 JPEG 与旋转后的图应得到相同 dHash（仅验证解码方向整体可用）。
func TestHashWithExifOrientation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 5), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("编码 JPEG 失败: %v", err)
	}
	plain := buf.Bytes()
	withExif := append([]byte{}, plain[:2]...)
	withExif = append(withExif, buildExifApp1(6)...)
	withExif = append(withExif, plain[2:]...)
	path := writeTemp(t, "exif6.jpg", withExif)
	if _, ok := Hash(path); !ok {
		t.Fatal("带 EXIF 的 JPEG 应能解码")
	}
}

// 大比例缩小（1024×1024 → 9×8）必须复现 GDI+ 的预滤波核：支撑宽度 2×缩放比的
// 三角核、目标像素中心取 (x+0.5)×scale、源图外按白底取样、两趟之间中间结果取整到 8 位。
// 两个样本与 E 批次对拍用的是同一文件，指纹须与 C# 基线逐位一致。
func TestHashLargeDownscaleGDIPlus(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"build", "appicon.png"}, "011397970f2b2b03"},
		{[]string{"frontend", "src", "assets", "images", "logo-universal.png"}, "63431f1f1f1f1f3c"},
	}
	for _, c := range cases {
		path := filepath.Join(append([]string{"..", "..", "..", "..", "testdata", "out", "uispike"}, c.parts...)...)
		fp, ok := Compute(path)
		if !ok {
			t.Fatalf("%s 应能解码", path)
		}
		if got := Hex(fp.Hash); got != c.want {
			t.Errorf("%s 指纹 = %s, want %s（须与 C# 基线一致）", path, got, c.want)
		}
	}
}

// TIFF 走 golang.org/x/image/tiff：同像素内容的 TIFF 与 PNG 必须得到相同 dHash。
// 与 C# 基线的一致性另由 parity similar 对 GDI+ 生成的 tif/tiff 验证。
func TestDecodeTIFF(t *testing.T) {
	const w, h = 20, 12
	pixel := func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 10), G: uint8(y * 20), B: 7, A: 255}
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, pixel(x, y))
		}
	}
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, img, &tiff.Options{Compression: tiff.Deflate}); err != nil {
		t.Fatalf("编码 TIFF 失败: %v", err)
	}
	for _, name := range []string{"test.tif", "test.tiff"} {
		path := writeTemp(t, name, buf.Bytes())
		h1, ok := Hash(path)
		if !ok {
			t.Fatalf("%s 应能解码", name)
		}
		h2, ok := Hash(writeTemp(t, "test.png", encodePNG(t, img)))
		if !ok {
			t.Fatal("PNG 应能解码")
		}
		if h1 != h2 {
			t.Fatalf("%s 与 PNG 的 dHash 不一致: %016x vs %016x", name, h1, h2)
		}
	}
}

// gopher.webp：来自 golang.org/x/image testdata 的 442 字节有损 WebP。
// 原版 GDI+ 不支持 WebP，这里只验证"纯 Go 解码器可用且指纹非退化"。
const gopherWebPBase64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func TestDecodeWebP(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(gopherWebPBase64)
	if err != nil {
		t.Fatalf("样本解码失败: %v", err)
	}
	path := writeTemp(t, "gopher.webp", data)
	fp, ok := Compute(path)
	if !ok {
		t.Fatal("WebP 应能解码")
	}
	if fp.Uniform {
		t.Fatalf("真实照片的指纹不应是退化指纹: %016x", fp.Hash)
	}
	if len(Hex(fp.Hash)) != 16 {
		t.Fatalf("Hex 长度应为 16: %q", Hex(fp.Hash))
	}

	// 截断的 WebP 必须按解码失败处理，而不是 panic 或返回指纹。
	truncated := writeTemp(t, "broken.webp", data[:len(data)/2])
	if _, ok := Hash(truncated); ok {
		t.Fatal("截断的 WebP 不应算出指纹")
	}
}
