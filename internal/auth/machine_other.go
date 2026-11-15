//go:build !windows

package auth

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// readMachineFingerprint 是非 Windows 平台的兜底实现，仅用于开发与测试构建。
// 正式产物只发布 Windows 版，机器码以 Windows 实现为准。
func readMachineFingerprint() ([10]byte, error) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return [10]byte{}, fmt.Errorf("无法读取主机名，机器码不可用")
	}
	sum := sha256.Sum256([]byte(machineTracePrefix + "host=" + host))
	var fp [10]byte
	copy(fp[:], sum[:10])
	return fp, nil
}
