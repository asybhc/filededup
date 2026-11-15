//go:build windows

package auth

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"filededup/internal/platform"

	"golang.org/x/sys/windows/registry"
)

const machineTracePrefix = "FileDedupTool|v1|"

// readMachineFingerprint 复刻旧版 C# 的机器码算法，保证旧许可证可继续验证：
//
//	机器码 = CrockfordBase32(SHA256("FileDedupTool|v1|" + 各项拼接)[:10])
//
// 首选 MachineGuid + 系统盘卷序列号；MachineGuid 缺失时退回
// CPU 名称/标识、主板厂商/型号、卷序列号、计算机名。
func readMachineFingerprint() ([10]byte, error) {
	guid := readRegistryString(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, "MachineGuid", registry.QUERY_VALUE|registry.WOW64_64KEY)

	serialText := "00000000"
	if root := systemRootVolume(); root != "" {
		if serial, err := platform.VolumeSerial(root); err == nil {
			serialText = fmt.Sprintf("%08X", serial)
		}
	}

	var parts []string
	if strings.TrimSpace(guid) != "" {
		parts = append(parts, "g="+normalizeHW(guid), "v="+serialText)
	} else {
		proc := readRegistryString(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "ProcessorNameString", registry.QUERY_VALUE)
		ident := readRegistryString(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\CentralProcessor\0`, "Identifier", registry.QUERY_VALUE)
		boardMaker := readRegistryString(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, "BaseBoardManufacturer", registry.QUERY_VALUE)
		boardProduct := readRegistryString(registry.LOCAL_MACHINE, `HARDWARE\DESCRIPTION\System\BIOS`, "BaseBoardProduct", registry.QUERY_VALUE)
		host, _ := os.Hostname()
		parts = append(parts,
			"c="+normalizeHW(proc+"|"+ident),
			"b="+normalizeHW(boardMaker+"|"+boardProduct),
			"v="+serialText,
			"n="+normalizeHW(host),
		)
	}

	var fp [10]byte
	joined := strings.Join(parts, "\n")
	if joined == "" {
		return fp, fmt.Errorf("无法读取机器信息，机器码不可用")
	}
	sum := sha256.Sum256([]byte(machineTracePrefix + joined))
	copy(fp[:], sum[:10])
	return fp, nil
}

// systemRootVolume 返回 Windows 目录所在盘符的根，如 "C:\"。
// 旧版取的是 Windows 目录所在卷，而不是 %SystemDrive%，这里保持一致。
func systemRootVolume() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("WINDIR")
	}
	if root == "" {
		root = `C:\Windows`
	}
	vol := filepath.VolumeName(root)
	if vol == "" {
		return ""
	}
	return vol + `\`
}

func readRegistryString(root registry.Key, path, name string, access uint32) string {
	key, err := registry.OpenKey(root, path, access|registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if err != nil {
		return ""
	}
	return value
}

// normalizeHW 对应旧版的 A()：Trim → 小写 → 去掉所有空白。
func normalizeHW(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}
