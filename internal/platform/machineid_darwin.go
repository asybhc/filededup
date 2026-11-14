//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var platformUUIDPattern = regexp.MustCompile(`"IOPlatformUUID"\s*=\s*"([^"]+)"`)

// MachineIDParts 采集 macOS 机器码 v2 的原始项：IOPlatformUUID。
// macOS 没有 Windows 那样的"系统盘卷序列号"概念，第二采集项留空（格式冻结）。
func MachineIDParts() (MachineParts, error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return MachineParts{}, fmt.Errorf("platform: 读取 IOPlatformUUID 失败：%w", err)
	}
	m := platformUUIDPattern.FindSubmatch(out)
	if len(m) != 2 {
		return MachineParts{}, fmt.Errorf("platform: ioreg 输出中没有 IOPlatformUUID")
	}
	p := MachineParts{
		OS:        "darwin",
		PrimaryID: strings.TrimSpace(string(m[1])),
	}
	p.Hash = p.computeHash()
	return p, nil
}
