//go:build !windows && !darwin && !linux

package platform

import "fmt"

// MachineIDParts 在 M2 尚未覆盖的平台上返回 ErrUnsupported。
func MachineIDParts() (MachineParts, error) {
	return MachineParts{}, fmt.Errorf("%w: 机器码采集", ErrUnsupported)
}
