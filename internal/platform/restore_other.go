//go:build !windows

package platform

// 非 Windows 平台暂不支持“按原路径从回收站恢复”：
//   - macOS 的 ~/.Trash 不保存原路径，无法判断该恢复到哪个目录；
//   - Linux 的 XDG 回收站虽然记录了 .trashinfo 的 Path=，但本构建尚未实现解析，
//     为避免出现“看起来能撤销、实际恢复到错误位置”，这里统一明确失败。
//
// 调用方（查重侧撤销）会把这些原因原样展示给用户，而不是假装成功。

// RestoreFromTrash 在非 Windows 平台返回明确的“不支持”原因。
func RestoreFromTrash(targets []RestoreTarget) RestoreReport {
	if len(targets) == 0 {
		return RestoreReport{}
	}
	reason := "当前平台不支持自动从回收站撤销，请在系统回收站中手动还原"
	return failedRestore(targets, reason)
}
