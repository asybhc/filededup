//go:build !windows

package platform

import "fmt"

// Trash 在非 Windows 平台尚未实现（M2 只交付 Windows 回收站）。
// 调用方必须把该错误当作"当前动作安全失败"处理，不得回退为永久删除。
func Trash(path string) error {
	return fmt.Errorf("%w: 回收站（%s）", ErrUnsupported, path)
}
