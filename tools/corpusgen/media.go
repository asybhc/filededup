package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"runtime"
)

// 说明：本文件的编码函数在参数合法时不会失败；一旦失败说明程序有 bug，直接 panic 由 Generate 捕获为 error。

func goos() string { return runtime.GOOS }

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: 255} }

func solid(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func gradientH(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	den := max(1, w-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(x * 255 / den)
			img.SetRGBA(x, y, color.RGBA{R: v, G: uint8(255 - int(v)), B: uint8((x * 7) % 256), A: 255})
		}
	}
	return img
}

func gradientV(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	den := max(1, h-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(y * 255 / den)
			img.SetRGBA(x, y, color.RGBA{R: uint8((y * 3) % 256), G: v, B: uint8(255 - int(v)), A: 255})
		}
	}
	return img
}

func alphaGradient(w, h int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	denX := max(1, w-1)
	denY := max(1, h-1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(y * 255 / denY),
				G: 40,
				B: 200,
				A: uint8(x * 255 / denX),
			})
		}
	}
	return img
}

func flipH(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(b.Min.X+w-1-x, b.Min.Y+y))
		}
	}
	return dst
}

// rotate90CCW 逆时针旋转 90°；配合 EXIF Orientation=6（显示时顺时针 90°）可还原原图。
func rotate90CCW(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < w; y++ {
		for x := 0; x < h; x++ {
			dst.Set(x, y, src.At(b.Min.X+w-1-y, b.Min.Y+x))
		}
	}
	return dst
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func encodeJPEG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// insertExifOrientation 在 SOI 之后插入一个只含 Orientation 的 APP1(Exif) 段。
func insertExifOrientation(jpg []byte, orientation uint16) []byte {
	if len(jpg) < 2 || jpg[0] != 0xFF || jpg[1] != 0xD8 {
		panic("insertExifOrientation: 输入不是 JPEG")
	}
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00,
		0x01, 0x00, // IFD0 条目数
		0x12, 0x01, // tag 0x0112 Orientation
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		0x00, 0x00, 0x00, 0x00, // value
		0x00, 0x00, 0x00, 0x00, // next IFD
	}
	binary.LittleEndian.PutUint16(tiff[18:20], orientation)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := len(payload) + 2

	out := make([]byte, 0, len(jpg)+segLen+2)
	out = append(out, jpg[0], jpg[1], 0xFF, 0xE1, byte(segLen>>8), byte(segLen))
	out = append(out, payload...)
	out = append(out, jpg[2:]...)
	return out
}

// wavPCM 生成 16-bit 单声道 PCM WAV；freq=0 为静音；amplitude 默认 0.3。
func wavPCM(sampleRate int, seconds, freq float64, amplitude ...float64) []byte {
	amp := 0.3
	if len(amplitude) > 0 {
		amp = amplitude[0]
	}
	n := int(float64(sampleRate) * seconds)
	data := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := 0.0
		if freq > 0 {
			v = math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)) * amp
		}
		s := int16(v * 32767)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	return wavContainer(data, sampleRate)
}

func wavContainer(data []byte, sampleRate int) []byte {
	const channels = 1
	const bitsPerSample = 16
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(byteRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(blockAlign))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// MPEG1 Layer III 码率表（kbps），索引与帧头一致。
var mp3Bitrates = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}

func mp3FrameLen(bitrateIndex byte) int {
	bitrate := mp3Bitrates[bitrateIndex]
	return 144 * bitrate * 1000 / 44100
}

func mp3Frame(bitrateIndex byte) []byte {
	f := make([]byte, mp3FrameLen(bitrateIndex))
	f[0] = 0xFF
	f[1] = 0xFB // MPEG1 / Layer III / 无 CRC
	f[2] = bitrateIndex << 4
	f[3] = 0x00 // stereo
	return f
}

func mp3FrameCount(seconds float64) int {
	return int(math.Round(seconds * 44100 / 1152))
}

// mp3CBR 生成 128kbps CBR 静音帧。
func mp3CBR(seconds float64) []byte {
	n := mp3FrameCount(seconds)
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.Write(mp3Frame(9))
	}
	return buf.Bytes()
}

// mp3VBR 生成码率交替变化的帧，并在首帧写入 Xing(帧数) 头，用于验证 VBR 时长计算。
func mp3VBR(seconds float64) []byte {
	n := mp3FrameCount(seconds)
	pattern := []byte{9, 11, 10} // 128 / 192 / 160 kbps
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		frame := mp3Frame(pattern[i%len(pattern)])
		if i == 0 {
			// MPEG1 立体声 side info 32 字节，Xing 位于帧内偏移 4+32=36。
			copy(frame[36:], []byte("Xing"))
			binary.BigEndian.PutUint32(frame[40:], 0x00000001) // flags: 仅 frames
			binary.BigEndian.PutUint32(frame[44:], uint32(n))
		}
		buf.Write(frame)
	}
	return buf.Bytes()
}

func syncsafe(n int) [4]byte {
	return [4]byte{
		byte((n >> 21) & 0x7F),
		byte((n >> 14) & 0x7F),
		byte((n >> 7) & 0x7F),
		byte(n & 0x7F),
	}
}

func id3Frame(id string, payload []byte) []byte {
	out := make([]byte, 0, 10+len(payload))
	out = append(out, id...)
	size := [4]byte{byte(len(payload) >> 24), byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload))}
	out = append(out, size[:]...)
	out = append(out, 0x00, 0x00)
	out = append(out, payload...)
	return out
}

// id3v23 生成含标题与封面（APIC）的 ID3v2.3 标签。
func id3v23(title string, coverPNG []byte) []byte {
	var frames bytes.Buffer
	frames.Write(id3Frame("TIT2", append([]byte{0x03}, []byte(title)...)))

	apic := []byte{0x00} // ISO-8859-1
	apic = append(apic, []byte("image/png\x00")...)
	apic = append(apic, 0x03) // 封面（前置）
	apic = append(apic, 0x00) // 空描述
	apic = append(apic, coverPNG...)
	frames.Write(id3Frame("APIC", apic))

	size := syncsafe(frames.Len())
	out := make([]byte, 0, 10+frames.Len())
	out = append(out, 'I', 'D', '3', 0x03, 0x00, 0x00)
	out = append(out, size[:]...)
	out = append(out, frames.Bytes()...)
	return out
}
