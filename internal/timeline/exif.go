package timeline

// EXIF 拍摄时间解析：只做“取出 DateTimeOriginal（回退 DateTime）”这一件事，
// 不处理方向、GPS 等其余字段。JPEG/TIFF 之外的文件按修改时间处理。

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// exifHeadLimit 是读取文件头的上限；EXIF APP1 通常只有几十 KB。
const exifHeadLimit = 256 << 10

var exifExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".jpe": true,
	".tif": true, ".tiff": true,
}

// fileWhen 返回用于分桶的时间：图片优先 EXIF 拍摄时间，其余用修改时间。
func fileWhen(path string, mod time.Time) (time.Time, string) {
	if t, ok := exifTime(path); ok {
		return t, "exif"
	}
	return mod, "mtime"
}

func exifTime(path string) (time.Time, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	if !exifExts[ext] {
		return time.Time{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}, false
	}
	defer f.Close()
	buf := make([]byte, exifHeadLimit)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return time.Time{}, false
	}
	buf = buf[:n]
	if ext == ".tif" || ext == ".tiff" {
		return parseTIFF(buf)
	}
	return parseJPEGEXIF(buf)
}

// parseJPEGEXIF 顺序扫描 JPEG 段，找到 APP1/Exif 后交给 TIFF 解析。
func parseJPEGEXIF(b []byte) (time.Time, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return time.Time{}, false
	}
	i := 2
	for i+1 < len(b) {
		if b[i] != 0xFF {
			i++
			continue
		}
		marker := b[i+1]
		if marker == 0xFF {
			i++
			continue
		}
		if marker == 0xD8 || marker == 0xD9 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA { // 图像数据开始，元数据已结束
			break
		}
		if i+4 > len(b) {
			break
		}
		size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if size < 2 || i+2+size > len(b) {
			break
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) >= 6 && string(seg[:6]) == "Exif\x00\x00" {
			return parseTIFF(seg[6:])
		}
		i += 2 + size
	}
	return time.Time{}, false
}

func parseTIFF(b []byte) (time.Time, bool) {
	if len(b) < 8 {
		return time.Time{}, false
	}
	var bo binary.ByteOrder
	switch {
	case bytes.Equal(b[:2], []byte{'I', 'I'}):
		bo = binary.LittleEndian
	case bytes.Equal(b[:2], []byte{'M', 'M'}):
		bo = binary.BigEndian
	default:
		return time.Time{}, false
	}
	if bo.Uint16(b[2:4]) != 42 {
		return time.Time{}, false
	}
	ifd0 := int(bo.Uint32(b[4:8]))
	dt, exifIFD := readIFD(b, bo, ifd0)
	if dt.IsZero() {
		if exifIFD > 0 {
			dt, _ = readIFD(b, bo, exifIFD)
		}
	}
	if dt.IsZero() {
		return time.Time{}, false
	}
	return dt, true
}

// readIFD 返回 DateTime/DateTimeOriginal 以及 ExifIFD 偏移（仅 IFD0 上有效）。
func readIFD(b []byte, bo binary.ByteOrder, off int) (time.Time, int) {
	if off <= 0 || off+2 > len(b) {
		return time.Time{}, 0
	}
	count := int(bo.Uint16(b[off : off+2]))
	pos := off + 2
	var dt, dto time.Time
	exifIFD := 0
	for i := 0; i < count; i++ {
		e := pos + i*12
		if e+12 > len(b) {
			break
		}
		tag := bo.Uint16(b[e : e+2])
		typ := bo.Uint16(b[e+2 : e+4])
		// 指针字段（LONG）与 ASCII 时间字段直接读内联值。
		switch tag {
		case 0x8769:
			if typ == 4 {
				exifIFD = int(bo.Uint32(b[e+8 : e+12]))
			}
		case 0x9003, 0x0132:
			s := asciiValue(b, bo, e)
			if s != "" {
				if t, ok := parseExifDate(s); ok {
					if tag == 0x9003 {
						dto = t
					} else {
						dt = t
					}
				}
			}
		}
	}
	if !dto.IsZero() {
		return dto, exifIFD
	}
	return dt, exifIFD
}

func asciiValue(b []byte, bo binary.ByteOrder, entry int) string {
	typ := bo.Uint16(b[entry+2 : entry+4])
	if typ != 2 { // ASCII
		return ""
	}
	n := int(bo.Uint32(b[entry+4 : entry+8]))
	if n <= 0 {
		return ""
	}
	raw := b[entry+8 : entry+12]
	if n > 4 {
		po := int(bo.Uint32(b[entry+8 : entry+12]))
		if po <= 0 || po+n > len(b) {
			return ""
		}
		raw = b[po : po+n]
	} else if n > len(raw) {
		raw = raw[:n]
	}
	return strings.TrimRight(string(raw), "\x00")
}

func parseExifDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006:01:02 15:04:05", "2006-01-02 15:04:05", "2006:01:02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			// 0000:00:00 之类的占位值按无效处理。
			if t.Year() > 1900 {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
