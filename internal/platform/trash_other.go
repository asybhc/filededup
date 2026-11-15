//go:build !windows && !linux && !darwin

package platform

import "fmt"

// Trash 在尚未实现回收站的平台返回 ErrUnsupported。
// 调用方必须把该错误当作"当前动作安全失败"处理，不得回退为永久删除。
func Trash(path string) error {
	return fmt.Errorf("%w: 回收站（%s）", ErrUnsupported, path)
}
