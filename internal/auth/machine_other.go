//go:build !windows

package auth

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"filededup/internal/platform"
)

const machineTracePrefix = "FileDedupTool|v1|"

// readMachineFingerprint 是非 Windows 平台的实现。
//
// 优先使用 platform.MachineIDParts 的稳定硬件标识：
//   - linux：/etc/machine-id（回退 dbus machine-id）+ DMI product_uuid
//   - darwin：IOPlatformUUID
//
// 采集失败时才退回主机名。非 Windows 产物尚未发布，因此这里不承诺与旧版
// C# 机器码算法逐位兼容；Windows 仍走 machine_windows.go 的兼容实现。
func readMachineFingerprint() ([10]byte, error) {
	var fp [10]byte
	if parts, err := platform.MachineIDParts(); err == nil && strings.TrimSpace(parts.PrimaryID) != "" {
		joined := "os=" + parts.OS + "\np=" + parts.PrimaryID + "\ns=" + parts.SecondaryID
		sum := sha256.Sum256([]byte(machineTracePrefix + joined))
		copy(fp[:], sum[:10])
		return fp, nil
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return fp, fmt.Errorf("无法读取机器信息，机器码不可用")
	}
	sum := sha256.Sum256([]byte(machineTracePrefix + "host=" + host))
	copy(fp[:], sum[:10])
	return fp, nil
}
