package image

import (
	"encoding/binary"
	"io"
)

// jpegOrientation 读取 EXIF Orientation(274)。解析失败一律返回 1（不做变换），
// 与 C# A/Q.cs A_m_1(Image) 的异常回退一致。
func jpegOrientation(f io.ReaderAt) int {
	var head [2]byte
	if _, err := f.ReadAt(head[:], 0); err != nil || head[0] != 0xFF || head[1] != 0xD8 {
		return 1
	}
	pos := int64(2)
	var marker [2]byte
	for i := 0; i < 256; i++ {
		if _, err := f.ReadAt(marker[:], pos); err != nil {
			return 1
		}
		if marker[0] != 0xFF {
			return 1
		}
		switch marker[1] {
		case 0xD8: // 重复的 SOI
			pos += 2
			continue
		case 0xDA, 0xD9: // SOS / EOI：之后不再有 APP 段
			return 1
		case 0x01, 0xD0, 0xD1, 0xD2, 0xD3, 0xD4, 0xD5, 0xD6, 0xD7:
			pos += 2
			continue
		}
		var lenBuf [2]byte
		if _, err := f.ReadAt(lenBuf[:], pos+2); err != nil {
			return 1
		}
		segLen := int(binary.BigEndian.Uint16(lenBuf[:]))
		if segLen < 2 {
			return 1
		}
		if marker[1] == 0xE1 && segLen-2 >= 6 && segLen <= 1<<20 {
			buf := make([]byte, segLen-2)
			if _, err := f.ReadAt(buf, pos+4); err != nil {
				return 1
			}
			if string(buf[:4]) == "Exif" && buf[4] == 0 && buf[5] == 0 {
				if o, ok := tiffOrientation(buf[6:]); ok {
					return o
				}
			}
		}
		pos += 2 + int64(segLen)
	}
	return 1
}

// tiffOrientation 解析 TIFF 头与 IFD0，返回 1..8 的 Orientation；失败返回 false。
func tiffOrientation(b []byte) (int, bool) {
	if len(b) < 8 {
		return 0, false
	}
	var order binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	ifd := int(order.Uint32(b[4:8]))
	if ifd < 8 || ifd+2 > len(b) {
		return 0, false
	}
	n := int(order.Uint16(b[ifd : ifd+2]))
	for i := 0; i < n; i++ {
		off := ifd + 2 + i*12
		if off+12 > len(b) {
			return 0, false
		}
		tag := order.Uint16(b[off : off+2])
		typ := order.Uint16(b[off+2 : off+4])
		count := order.Uint32(b[off+4 : off+8])
		if tag != 274 {
			continue
		}
		if typ != 3 || count < 1 {
			return 0, false
		}
		v := int(order.Uint16(b[off+8 : off+10]))
		if v >= 1 && v <= 8 {
			return v, true
		}
		return 0, false
	}
	return 0, false
}
