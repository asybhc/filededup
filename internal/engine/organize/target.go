package organize

import (
	"errors"
	"path/filepath"
	"strings"
)

// ExternalTargetAlias 是“整理目标位于根目录之外”时规划层使用的虚拟目录名。
// 含控制字符，Windows 不允许其出现在真实文件名中，因此不会与根内目录重名；
// 它只存在于规划器的内存中，执行前会被 RewriteExternalTarget 改写为真实绝对路径。
const ExternalTargetAlias = "\x01filededup-external-target"

// TargetSpec 是整理目标的解析结果。
type TargetSpec struct {
	Rel      string // 规划用目标（根内相对路径）；外部目标时为 ExternalTargetAlias
	Abs      string // 外部目标的绝对路径（'/' 分隔）；根内时为空
	External bool   // 目标是否位于根目录之外
}

// ResolveTarget 解析整理目标：
//   - 空串表示不指定目标（第五步跳过）；
//   - 绝对路径落在根内时转为相对路径（保持旧行为），落在根外时标记为外部目标；
//   - 相对路径仍限制在根目录内，拒绝根目录本身与上级逃逸。
//
// 本函数只做字符串运算，不访问磁盘、不创建目录。
func ResolveTarget(absRoot, raw string) (TargetSpec, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return TargetSpec{}, nil
	}
	root := filepath.Clean(absRoot)
	if filepath.IsAbs(t) || filepath.VolumeName(t) != "" {
		if !filepath.IsAbs(t) {
			return TargetSpec{}, errors.New("整理目标路径无效：请填写完整绝对路径")
		}
		abs := filepath.Clean(t)
		if rel, err := filepath.Rel(root, abs); err == nil {
			switch {
			case rel == ".":
				return TargetSpec{}, errors.New("整理目标不能是根目录本身")
			case !isRelEscape(rel):
				return TargetSpec{Rel: filepath.ToSlash(rel)}, nil
			}
		}
		return TargetSpec{Rel: ExternalTargetAlias, Abs: filepath.ToSlash(abs), External: true}, nil
	}
	clean := filepath.Clean(t)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return TargetSpec{}, errors.New("整理目标不能是根目录本身或上级目录")
	}
	abs := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || isRelEscape(rel) {
		return TargetSpec{}, errors.New("整理目标必须位于根目录内，或填写根目录之外的绝对路径")
	}
	return TargetSpec{Rel: filepath.ToSlash(clean)}, nil
}

// isRelEscape 判断 Rel 的结果是否跳出根目录。
func isRelEscape(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// RewriteExternalTarget 把规划动作中以 ExternalTargetAlias 开头的目标改写为真实绝对路径。
// 只改 To（From 始终是根内相对路径）；非别名路径（含形似别名的前缀）保持不变。
func RewriteExternalTarget(res *Result, absTarget string) {
	if res == nil {
		return
	}
	base := filepath.ToSlash(filepath.Clean(absTarget))
	for si := range res.Steps {
		for ai := range res.Steps[si].Actions {
			a := &res.Steps[si].Actions[ai]
			switch {
			case a.To == ExternalTargetAlias:
				a.To = base
			case strings.HasPrefix(a.To, ExternalTargetAlias+"/"):
				a.To = base + strings.TrimPrefix(a.To, ExternalTargetAlias)
			}
		}
	}
}
