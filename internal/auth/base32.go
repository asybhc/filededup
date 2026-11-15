package auth

import "strings"

// crockfordAlphabet 与旧版 C# 的实现一致：去掉 I、L、O、U，避免混淆。
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// crockfordEncode 把字节流按 5 bit 一组编码，末尾不足 5 bit 补零。
// 10 字节恰好编成 16 个字符。
func crockfordEncode(src []byte) string {
	if len(src) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow((len(src)*8 + 4) / 5)
	var buf uint64
	var bits uint
	for _, b := range src {
		buf = (buf << 8) | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockfordAlphabet[(buf>>bits)&0x1F])
		}
		buf &= (1 << bits) - 1
	}
	if bits > 0 {
		sb.WriteByte(crockfordAlphabet[(buf<<(5-bits))&0x1F])
	}
	return sb.String()
}

// crockfordValue 返回字符对应的 5 bit 值；-1 表示非法字符。
func crockfordValue(r rune) int {
	switch r {
	case '-', ' ', '\r', '\n', '\t', '_', '.':
		return -2
	case 'I', 'i', 'L', 'l':
		return 1
	case 'O', 'o':
		return 0
	}
	if r < 0x80 {
		c := byte(r)
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		if i := strings.IndexByte(crockfordAlphabet, c); i >= 0 {
			return i
		}
	}
	return -1
}

// crockfordDecode 解码 Crockford Base32。分隔符会被忽略，末尾不足 8 bit 的
// 残余位丢弃（与旧版一致）。
func crockfordDecode(s string) ([]byte, bool) {
	if s == "" {
		return nil, false
	}
	out := make([]byte, 0, len(s)*5/8)
	var buf uint64
	var bits uint
	for _, r := range s {
		v := crockfordValue(r)
		if v == -2 {
			continue
		}
		if v < 0 {
			return nil, false
		}
		buf = (buf << 5) | uint64(v)
		bits += 5
		for bits >= 8 {
			bits -= 8
			out = append(out, byte((buf>>bits)&0xFF))
		}
		buf &= (1 << bits) - 1
	}
	return out, true
}

// groupBy4 把 16 位机器码按 4 个一组加上连字符。
func groupBy4(s string) string {
	if len(s) <= 4 {
		return s
	}
	var sb strings.Builder
	for i, r := range s {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// normalizeMachineText 只保留字母数字，并把 I/L 归一为 1、O 归一为 0。
func normalizeMachineText(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// NormalizeMachineCode 把用户输入的机器码归一化成 16 位大写形式。
func NormalizeMachineCode(s string) string {
	return normalizeMachineText(s)
}
