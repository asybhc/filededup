package auth

import (
	"sync"
)

// 机器码缓存：同一进程内硬件信息不会变，避免每次刷新界面都读注册表。
var (
	machineOnce sync.Once
	machineRaw  [10]byte
	machineErr  error
)

func machineFingerprint() ([10]byte, error) {
	machineOnce.Do(func() {
		machineRaw, machineErr = readMachineFingerprint()
	})
	return machineRaw, machineErr
}

// MachineCodeRaw 返回 16 位未分组的机器码（Crockford Base32）。
func MachineCodeRaw() (string, error) {
	fp, err := machineFingerprint()
	if err != nil {
		return "", err
	}
	return crockfordEncode(fp[:]), nil
}

// MachineCode 返回带连字符的机器码，例如 ABCD-EFGH-JKMN-PQRS。
func MachineCode() (string, error) {
	raw, err := MachineCodeRaw()
	if err != nil {
		return "", err
	}
	return groupBy4(raw), nil
}
