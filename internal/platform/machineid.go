package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// crockford 是 Crockford Base32 字母表：去掉 I、L、O、U，避免与 1、0 混淆。
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// MachineParts 是机器码 v2 的三个采集项（授权方案 B）。
//
// 派生规则（冻结，M8 验签必须与此一致）：
//
//	SHA-256(OS + "\x00" + PrimaryID + "\x00" + SecondaryID)
//	取前 16 字节，Crockford Base32 编码，每 4 字符插入 '-'。
//
// 三个采集项：
//   - windows：OS="windows"，PrimaryID=注册表 MachineGuid，SecondaryID=系统盘卷序列号（8 位大写 hex）
//   - darwin： OS="darwin"， PrimaryID=IOPlatformUUID，SecondaryID 为空
//   - linux：  OS="linux"，  PrimaryID=/etc/machine-id，SecondaryID=DMI product_uuid（读不到时为空）
type MachineParts struct {
	OS          string `json:"os"`
	PrimaryID   string `json:"primaryId"`
	SecondaryID string `json:"secondaryId"`
	Hash        string `json:"hash"` // 完整 SHA-256 小写 hex，仅供排障
}

func machineInput(p MachineParts) []byte {
	return []byte(p.OS + "\x00" + p.PrimaryID + "\x00" + p.SecondaryID)
}

func (p MachineParts) computeHash() string {
	sum := sha256.Sum256(machineInput(p))
	return hex.EncodeToString(sum[:])
}

// MachineID 返回机器码 v2。同一台机器、同一权限下必须稳定可复现。
func MachineID() (string, error) {
	p, err := MachineIDParts()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(machineInput(p))
	return FormatMachineID(sum[:16]), nil
}

// FormatMachineID 把 16 字节摘要编码为 26 字符、每 4 字符一组的分组码。
func FormatMachineID(b []byte) string {
	var enc strings.Builder
	var acc uint32
	var bits uint
	for _, by := range b {
		acc = acc<<8 | uint32(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			enc.WriteByte(crockford[(acc>>bits)&0x1F])
		}
	}
	if bits > 0 {
		enc.WriteByte(crockford[(acc<<(5-bits))&0x1F])
	}
	raw := enc.String()
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if i > 0 && i%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteByte(raw[i])
	}
	return out.String()
}

// ParseMachineID 规范化用户输入并解码回 16 字节。
// 忽略分隔符与大小写，并按 Crockford 规则把 I/L 当作 1、O 当作 0。
func ParseMachineID(s string) ([]byte, error) {
	clean := make([]byte, 0, 32)
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ', '\t', '\r', '\n':
			continue
		case 'I', 'L':
			r = '1'
		case 'O':
			r = '0'
		}
		idx := strings.IndexRune(crockford, r)
		if idx < 0 {
			return nil, fmt.Errorf("机器码含非法字符 %q", r)
		}
		clean = append(clean, byte(idx))
	}
	if len(clean) != 26 {
		return nil, fmt.Errorf("机器码长度应为 26 个有效字符，实际 %d", len(clean))
	}
	var out []byte
	var acc uint32
	var bits uint
	for _, v := range clean {
		acc = acc<<5 | uint32(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte(acc>>bits))
		}
	}
	// 26*5=130 位，128 位有效数据后残留 2 位补零。
	if bits > 0 && acc&((1<<bits)-1) != 0 {
		return nil, fmt.Errorf("机器码末尾填充位非零")
	}
	if len(out) != 16 {
		return nil, fmt.Errorf("机器码解码长度应为 16 字节，实际 %d", len(out))
	}
	return out, nil
}
