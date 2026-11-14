// Package audio 实现相似音频判定：读取 WAV/MP3 时长并按扩展名+时长聚类。
//
// 算法逐条复刻 C# 基线 A/S.cs：
//   - WAV：解析 RIFF/WAVE 的 fmt/data 块，时长 = dataSize / sampleRate。
//   - MP3：跳过 ID3v2，扫描帧同步字，优先用 Xing/Info 的 frames 换算时长，
//     不合法时回退 CBR 估算 fileLength*8/bitrate。
//   - 分组：按扩展名分组、时长升序，锚点比较 duration - 组首 <= max(0,tolerance)。
package audio

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Supported 报告扩展名是否在支持清单内（.mp3 / .wav，大小写不敏感）。
func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3", ".wav":
		return true
	}
	return false
}

// Probe 返回音频时长（秒）。第二个返回值为 false 表示格式不支持或数据不合法。
func Probe(path string) (float64, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		d, ok := wavDuration(path)
		if !ok {
			return 0, false
		}
		return d, true
	case ".mp3":
		d, ok := mp3Duration(path)
		if !ok {
			return 0, false
		}
		return d, true
	}
	return 0, false
}

// wavDuration 复刻 C# A/S.cs B(string)。
func wavDuration(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	length := info.Size()
	if length < 44 {
		return 0, false
	}
	r := bufio.NewReaderSize(f, 64<<10)
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil || string(hdr[:]) != "RIFF" {
		return 0, false
	}
	if _, err := io.CopyN(io.Discard, r, 4); err != nil {
		return 0, false
	}
	if _, err := io.ReadFull(r, hdr[:]); err != nil || string(hdr[:]) != "WAVE" {
		return 0, false
	}
	sampleRate := int32(0)
	dataSize := int64(-1)
	pos := int64(12)
	for pos+8 <= length {
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return 0, false
		}
		var szBuf [4]byte
		if _, err := io.ReadFull(r, szBuf[:]); err != nil {
			return 0, false
		}
		size := int64(binary.LittleEndian.Uint32(szBuf[:]))
		pos += 8
		switch string(hdr[:]) {
		case "fmt ":
			// 8 字节固定字段 + 4 字节采样率，共 12 字节（与 C# 读取顺序一致）。
			var body [12]byte
			if _, err := io.ReadFull(r, body[:]); err != nil {
				return 0, false
			}
			pos += 12
			sampleRate = int32(binary.LittleEndian.Uint32(body[8:12]))
			skip := size - 12
			if skip < 0 {
				skip = 0
			}
			if err := discard(r, &pos, skip, length); err != nil {
				return 0, false
			}
		case "data":
			dataSize = size
			if sampleRate > 0 {
				pos = length // 跳出循环
				break
			}
			skip := size
			if rem := length - pos; skip > rem {
				skip = rem
			}
			if err := discard(r, &pos, skip, length); err != nil {
				return 0, false
			}
		default:
			skip := size
			if rem := length - pos; skip > rem {
				skip = rem
			}
			if err := discard(r, &pos, skip, length); err != nil {
				return 0, false
			}
		}
	}
	if sampleRate <= 0 || dataSize <= 0 {
		return 0, false
	}
	return float64(dataSize) / float64(sampleRate), true
}

func discard(r *bufio.Reader, pos *int64, n, length int64) error {
	if n < 0 {
		n = 0
	}
	if *pos+n > length {
		return io.ErrUnexpectedEOF
	}
	if n == 0 {
		return nil
	}
	got, err := io.CopyN(io.Discard, r, n)
	*pos += got
	return err
}

// mp3Duration 复刻 C# A/S.cs b(string)。
func mp3Duration(path string) (float64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, false
	}
	length := info.Size()
	r := bufio.NewReaderSize(f, 64<<10)
	pos := int64(0)

	readByte := func() (int, bool) {
		b, err := r.ReadByte()
		if err != nil {
			return -1, false
		}
		pos++
		return int(b), true
	}

	if length >= 10 {
		var hdr3 [3]byte
		if _, err := io.ReadFull(r, hdr3[:]); err != nil {
			return 0, false
		}
		pos += 3
		if hdr3[0] == 'I' && hdr3[1] == 'D' && hdr3[2] == '3' {
			var vf [3]byte
			if _, err := io.ReadFull(r, vf[:]); err != nil {
				return 0, false
			}
			var syncsafe [4]byte
			if _, err := io.ReadFull(r, syncsafe[:]); err != nil {
				return 0, false
			}
			size := int64(syncsafe[0]&0x7F)<<21 | int64(syncsafe[1]&0x7F)<<14 |
				int64(syncsafe[2]&0x7F)<<7 | int64(syncsafe[3]&0x7F)
			if err := seek(r, f, &pos, size+10); err != nil {
				return 0, false
			}
		} else if err := seek(r, f, &pos, 0); err != nil {
			return 0, false
		}
	}

	for {
		b, ok := readByte()
		if !ok {
			return 0, false
		}
		if b != 0xFF {
			continue
		}
		b2, ok := readByte()
		if !ok {
			return 0, false
		}
		// 同步字：第二字节高 3 位必须为 111。
		if b2&0xE0 != 0xE0 {
			continue
		}
		version := (b2 >> 3) & 3
		layer := (b2 >> 1) & 3
		if version == 1 || layer == 0 {
			continue
		}
		frameStart := pos - 2
		b3, ok := readByte()
		if !ok {
			return 0, false
		}
		bitrateIdx := (b3 >> 4) & 0xF
		sampleIdx := (b3 >> 2) & 3
		b4, ok := readByte()
		if !ok {
			return 0, false
		}
		if bitrateIdx == 0 || bitrateIdx == 15 || sampleIdx == 3 {
			continue
		}
		versionKey := 2
		if version == 3 {
			versionKey = 3
		}
		bitrate := bitrateTable(versionKey, layer, bitrateIdx)
		if bitrate <= 0 {
			continue
		}
		if d, ok := xingDuration(f, length, frameStart, version, layer, sampleIdx, b4); ok {
			return d, true
		}
		return float64(length) * 8.0 / float64(bitrate), true
	}
}

func seek(r *bufio.Reader, f *os.File, pos *int64, target int64) error {
	if target < 0 {
		return errors.New("audio: negative seek")
	}
	if _, err := f.Seek(target, io.SeekStart); err != nil {
		return err
	}
	r.Reset(f)
	*pos = target
	return nil
}

// xingDuration 复刻 C# A/S.cs A_m_2(...)：读取 Xing/Info 标签的 frames 字段。
func xingDuration(f *os.File, length, frameStart int64, version, layer, sampleIdx, b4 int) (float64, bool) {
	mono := (b4>>6)&3 == 3
	var sideInfo int
	switch {
	case version == 3 && mono:
		sideInfo = 17
	case version == 3:
		sideInfo = 32
	case mono:
		sideInfo = 9
	default:
		sideInfo = 17
	}
	off := frameStart + 4 + int64(sideInfo)
	if off+12 > length {
		return 0, false
	}
	var tag [12]byte
	if _, err := f.ReadAt(tag[:], off); err != nil {
		return 0, false
	}
	isXing := tag[0] == 'X' && tag[1] == 'i' && tag[2] == 'n' && tag[3] == 'g'
	isInfo := tag[0] == 'I' && tag[1] == 'n' && tag[2] == 'f' && tag[3] == 'o'
	if !isXing && !isInfo {
		return 0, false
	}
	flags := binary.BigEndian.Uint32(tag[4:8])
	if flags&1 == 0 {
		return 0, false
	}
	frames := binary.BigEndian.Uint32(tag[8:12])
	if frames == 0 {
		return 0, false
	}
	rate := sampleRate(version, sampleIdx)
	if rate <= 0 {
		return 0, false
	}
	var spf int
	switch layer {
	case 1: // Layer III
		if version == 3 {
			spf = 1152
		} else {
			spf = 576
		}
	case 3: // Layer I
		spf = 384
	default: // Layer II
		spf = 1152
	}
	dur := float64(frames) * float64(spf) / float64(rate)
	avg := float64(length-frameStart) * 8.0 / dur
	if dur >= 1.0 && dur <= 86400.0 && avg >= 8000.0 && avg <= 640000.0 {
		return dur, true
	}
	return 0, false
}

func sampleRate(version, idx int) int {
	var table [3]int
	switch version {
	case 3:
		table = [3]int{44100, 48000, 32000}
	case 2:
		table = [3]int{22050, 24000, 16000}
	default:
		table = [3]int{11025, 12000, 8000}
	}
	if idx < 0 || idx > 2 {
		return 0
	}
	return table[idx]
}

// bitrateTable 复刻 C# A/S.cs A_m_1(int version, int layer, int bitrateIdx)（单位 bps）。
// versionKey：3=MPEG1，2=MPEG2/2.5；layer：1=LayerIII，2=LayerII，3=LayerI。
func bitrateTable(versionKey, layer, idx int) int {
	mpeg1 := [3][15]int{
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	}
	mpeg2 := [3][15]int{
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	}
	row := 2
	switch layer {
	case 2:
		row = 1
	case 3:
		row = 0
	}
	if idx < 0 || idx > 14 {
		return 0
	}
	if versionKey == 3 {
		return mpeg1[row][idx] * 1000
	}
	return mpeg2[row][idx] * 1000
}

// Item 是一个待分组的音频文件。
type Item struct {
	Path     string
	Duration float64 // 秒
}

// Group 复刻 C# A/S.cs A_m_1(List<string>, tolerance)：
// 按扩展名分组，组内按时长升序，与组首（锚点）比较，输出成员数 >= 2 的组。
// 输入顺序不参与语义：先按（时长, 小写路径, 路径）排序，保证确定性。
func Group(items []Item, tolerance int) [][]string {
	byExt := make(map[string][]Item)
	var extOrder []string
	for _, it := range items {
		ext := strings.ToLower(filepath.Ext(it.Path))
		if _, ok := byExt[ext]; !ok {
			extOrder = append(extOrder, ext)
		}
		byExt[ext] = append(byExt[ext], it)
	}
	sort.Strings(extOrder)

	limit := float64(tolerance)
	if limit < 0 {
		limit = 0
	}
	var out [][]string
	for _, ext := range extOrder {
		list := byExt[ext]
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Duration != list[j].Duration {
				return list[i].Duration < list[j].Duration
			}
			li, lj := strings.ToLower(list[i].Path), strings.ToLower(list[j].Path)
			if li != lj {
				return li < lj
			}
			return list[i].Path < list[j].Path
		})
		var cur []Item
		flush := func() {
			if len(cur) >= 2 {
				paths := make([]string, len(cur))
				for i := range cur {
					paths[i] = cur[i].Path
				}
				out = append(out, paths)
			}
		}
		for _, it := range list {
			if len(cur) == 0 || it.Duration-cur[0].Duration <= limit {
				cur = append(cur, it)
				continue
			}
			flush()
			cur = []Item{it}
		}
		flush()
	}
	return out
}

// DurationMS 把秒换算为毫秒（四舍五入，与 C# Math.Round 一致）。
func DurationMS(sec float64) int64 {
	return int64(math.Round(sec * 1000))
}
