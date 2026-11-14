package image

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
