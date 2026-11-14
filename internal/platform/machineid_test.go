package platform

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
)

var machineIDPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){5}-[0-9A-HJKMNP-TV-Z]{2}$`)

func TestFormatParseMachineIDRoundTrip(t *testing.T) {
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = byte(i*17 + 3)
	}
	id := FormatMachineID(raw)
	if !machineIDPattern.MatchString(id) {
		t.Fatalf("分组码格式不符：%q", id)
	}
	got, err := ParseMachineID(id)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("往返不一致：%x != %x", got, raw)
	}
}

func TestParseMachineIDNormalizes(t *testing.T) {
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = byte(i * 7)
	}
	id := FormatMachineID(raw)
	// 小写 + 去掉分隔符 + 用空格替代分隔符，均应解析为同一结果。
	messy := strings.ToLower(strings.ReplaceAll(id, "-", " "))
	got, err := ParseMachineID(messy)
	if err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("规范化后不一致")
	}
	// Crockford 混淆字符：0→O、1→I、1→L。
	confused := strings.NewReplacer("0", "O", "1", "I").Replace(id)
	got, err = ParseMachineID(confused)
	if err != nil {
		t.Fatalf("混淆字符解析失败：%v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("混淆字符解析结果不一致")
	}
}

func TestParseMachineIDRejectsBadInput(t *testing.T) {
	cases := []string{
		"",
		"ABCD",
		"UUUU-UUUU-UUUU-UUUU-UUUU-UUUU-UU",
		"0000-0000-0000-0000-0000-0000-01",
	}
	for _, in := range cases {
		if _, err := ParseMachineID(in); err == nil {
			t.Errorf("应拒绝非法机器码 %q", in)
		}
	}
}

func TestMachineIDStable(t *testing.T) {
	first, err := MachineID()
	if errors.Is(err, ErrUnsupported) {
		t.Skip("当前平台未实现机器码采集")
	}
	if err != nil {
		t.Fatalf("采集机器码失败：%v", err)
	}
	if !machineIDPattern.MatchString(first) {
		t.Fatalf("机器码格式不符：%q", first)
	}
	second, err := MachineID()
	if err != nil {
		t.Fatalf("二次采集失败：%v", err)
	}
	if first != second {
		t.Fatalf("同一进程内机器码不稳定：%q != %q", first, second)
	}
	parts, err := MachineIDParts()
	if err != nil {
		t.Fatalf("采集原始项失败：%v", err)
	}
	if parts.PrimaryID == "" {
		t.Fatalf("主采集项为空")
	}
	if parts.Hash != parts.computeHash() {
		t.Fatalf("Hash 与采集项不一致")
	}
}
