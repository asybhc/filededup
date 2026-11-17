package main

// apply 的根目录守卫：执行任何动作前确认 src/dst 都落在计划声明的根目录内。
//
// 背景：--plan 接受任意外部 JSON，若被改成根目录之外的绝对路径或含 .. 的路径，
// 执行器会照做（删除/移动/改属性）。这里在打开操作清单之前整批拒绝，越界时
// 不产生任何磁盘改动。
//
// 符号链接按解析后的真实路径判断：根内一个指向根外的链接不能成为越界跳板。
// 目标路径可能尚不存在（move/rename 的新位置），解析时取最近的已存在祖先。

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"filededup/internal/engine/model"
)

// rootFlags 支持 -root 重复指定多个根目录（多根扫描计划）。
type rootFlags []string

func (r *rootFlags) String() string { return strings.Join(*r, ",") }

func (r *rootFlags) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// validateActionRoots 校验计划中每条动作的 src 与 dst 都在根目录内。
// 命令行 -root 优先于计划里的 roots；两者都为空时拒绝执行（旧计划需显式指定）。
func validateActionRoots(p *model.Plan, cliRoots []string) error {
	if p == nil {
		return errors.New("计划为空")
	}
	rawRoots := cliRoots
	if len(rawRoots) == 0 {
		rawRoots = p.Roots
	}
	if len(rawRoots) == 0 {
		return errors.New("计划未声明根目录，请用 -root 指定计划根目录")
	}
	roots := make([]string, 0, len(rawRoots))
	for _, r := range rawRoots {
		real, err := resolveRealPath(r)
		if err != nil {
			return fmt.Errorf("解析根目录失败 %q：%w", r, err)
		}
		roots = append(roots, real)
	}
	for _, a := range p.Actions {
		if err := checkPathInRoots(a.Src, roots); err != nil {
			return fmt.Errorf("第 %d 条动作（%s）的 src 越界：%w", a.Seq, a.Kind, err)
		}
		if a.Dst != "" {
			if err := checkPathInRoots(a.Dst, roots); err != nil {
				return fmt.Errorf("第 %d 条动作（%s）的 dst 越界：%w", a.Seq, a.Kind, err)
			}
		}
	}
	return nil
}

func checkPathInRoots(p string, roots []string) error {
	real, err := resolveRealPath(p)
	if err != nil {
		return fmt.Errorf("%q 无法解析：%w", p, err)
	}
	for _, root := range roots {
		if pathWithinRoot(real, root) {
			return nil
		}
	}
	return fmt.Errorf("%q 不在计划根目录内", p)
}

// pathWithinRoot 判断已解析的绝对路径是否在根目录内（含根目录本身）。
func pathWithinRoot(real, root string) bool {
	rel, err := filepath.Rel(root, real)
	if err != nil {
		// 跨卷时 Rel 直接报错，按越界处理。
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || filepath.IsAbs(rel) ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// resolveRealPath 返回解析符号链接/联接点后的绝对路径。
// 路径尚不存在时，向上找到最近的已存在祖先，解析后拼回剩余部分。
func resolveRealPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	var tail []string
	cur := abs
	for {
		real, err := filepath.EvalSymlinks(cur)
		if err == nil {
			if len(tail) == 0 {
				return real, nil
			}
			return filepath.Join(append([]string{real}, tail...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// 到达卷根仍不存在：按原绝对路径判断（不会因解析失败而放行越界）。
			return abs, nil
		}
		tail = append([]string{filepath.Base(cur)}, tail...)
		cur = parent
	}
}
