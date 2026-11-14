//go:build linux

package platform

import (
	"fmt"
	"os"
	"strings"
)

// MachineIDParts 采集 Linux 机器码 v2 的原始项：
// /etc/machine-id（回退 /var/lib/dbus/machine-id）+ DMI product_uuid。
//
// 注意：product_uuid 在容器或权限受限环境中可能读不到，此时第二采集项为空，
// 同一台机器在不同权限下可能得到不同机器码。M8 上线前需要确认该平台的授权策略。
func MachineIDParts() (MachineParts, error) {
	primary, err := readTrimmed("/etc/machine-id")
	if err != nil {
		primary, err = readTrimmed("/var/lib/dbus/machine-id")
		if err != nil {
			return MachineParts{}, fmt.Errorf("platform: 读取 machine-id 失败：%w", err)
		}
	}
	secondary, _ := readTrimmed("/sys/class/dmi/id/product_uuid")
	p := MachineParts{
		OS:          "linux",
		PrimaryID:   primary,
		SecondaryID: secondary,
	}
	p.Hash = p.computeHash()
	return p, nil
}

func readTrimmed(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}
