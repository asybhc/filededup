package audio

import (
	"bytes"
	"encoding/binary"
	"math"
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

// buildWAV 生成 16 位单声道 PCM WAV。
func buildWAV(sampleRate int, seconds float64) []byte {
	n := int(float64(sampleRate) * seconds)
	data := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(math.Sin(2*math.Pi*440*float64(i)/float64(sampleRate)) * 8000)
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

// mp3Frame 生成 MPEG1 Layer III、44.1kHz、128kbps 的静音帧。
func mp3Frame() []byte {
	const frameLen = 144*128000/44100 - 1 // 417
	f := make([]byte, frameLen)
	f[0], f[1] = 0xFF, 0xFB
	f[2], f[3] = 0x90, 0x00
	return f
}

func TestWAVDuration(t *testing.T) {
	for _, sec := range []float64{0.5, 1, 2} {
		path := writeTemp(t, "a.wav", buildWAV(8000, sec))
		got, ok := Probe(path)
		if !ok {
			t.Fatalf("%v 秒 WAV 应能解析", sec)
		}
		if math.Abs(got-sec) > 0.001 {
			t.Fatalf("WAV 时长 = %v, want %v", got, sec)
		}
	}
}

func TestMP3CBRDuration(t *testing.T) {
	const frames = 383 // 约 10 秒
	var buf bytes.Buffer
	for i := 0; i < frames; i++ {
		buf.Write(mp3Frame())
	}
	path := writeTemp(t, "cbr.mp3", buf.Bytes())
	got, ok := Probe(path)
	if !ok {
		t.Fatal("CBR MP3 应能解析")
	}
	want := float64(frames) * 1152 / 44100
	if math.Abs(got-want) > 0.05 {
		t.Fatalf("CBR 时长 = %v, want %v", got, want)
	}
}

func TestMP3VBRDuration(t *testing.T) {
	const frames = 383
	var buf bytes.Buffer
	for i := 0; i < frames; i++ {
		f := mp3Frame()
		if i == 0 {
			copy(f[36:], []byte("Xing"))
			binary.BigEndian.PutUint32(f[40:], 1)
			binary.BigEndian.PutUint32(f[44:], frames)
		}
		buf.Write(f)
	}
	path := writeTemp(t, "vbr.mp3", buf.Bytes())
	got, ok := Probe(path)
	if !ok {
		t.Fatal("VBR MP3 应能解析")
	}
	want := float64(frames) * 1152 / 44100
	if math.Abs(got-want) > 0.001 {
		t.Fatalf("VBR 时长 = %v, want %v", got, want)
	}
}

func TestProbeInvalid(t *testing.T) {
	cases := map[string][]byte{
		"short.wav":   []byte("RIFF"),
		"garbage.mp3": bytes.Repeat([]byte{0x00}, 64),
		"empty.wav":   {},
		"header-only": append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, 64)...),
	}
	for name, data := range cases {
		path := writeTemp(t, name, data)
		if _, ok := Probe(path); ok {
			t.Errorf("%s 不应解析出时长", name)
		}
	}
}

func TestGroupByExtensionAndTolerance(t *testing.T) {
	items := []Item{
		{Path: `x\b.wav`, Duration: 1.0},
		{Path: `x\a.wav`, Duration: 0.5},
		{Path: `x\c.mp3`, Duration: 0.5},
		{Path: `x\d.mp3`, Duration: 9.0},
		{Path: `x\e.wav`, Duration: 2.5},
	}
	groups := Group(items, 2)
	if len(groups) != 1 {
		t.Fatalf("wav 锚点 0.5 覆盖 0.5/1.0/2.5，mp3 两个时长相隔 8.5 秒不成组，应只剩 1 组，实际 %d: %v", len(groups), groups)
	}
	if len(groups[0]) != 3 || groups[0][0] != `x\a.wav` {
		t.Fatalf("wav 组应按时长排序且以 0.5 为锚点: %v", groups[0])
	}
}
