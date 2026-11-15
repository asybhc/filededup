package organize

// 本文件是整理计划的文件系统执行层：把 Plan 产出的 Result 逐条落到 root 下。
//
// 与纯函数层严格分工：这里只做路径解析与系统调用，不重新判断规则、不重算冲突
// （StepPlan 的 To 已是规划期解析冲突后的最终路径），也不依赖 Windows 专有 API。
// 动作失败时不猜测补救策略，只记录错误并（默认）中止后续动作。

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExecHooks 注入平台能力；零值即安全默认值。
type ExecHooks struct {
	// Trash 把绝对路径移入回收站（由外壳注入平台实现）。
	// nil 时 delete 退化为 os.Remove（永久删除）。
	Trash func(absPath string) error
	// ConfirmLock 在解除文件占用前确认；仅 ReleaseLock=true 且占用进程有可见窗口时调用。
	// nil 表示无法确认（按失败处理，不会静默杀进程）。
	ConfirmLock func(absPath string, holderNames []string) bool
	// IsLocked 判断文件是否被其它进程占用；nil 视为未占用。
	IsLocked func(absPath string) bool
	// ReleaseLocks 解除文件占用；confirm 为 nil 表示无法人工确认。
	// nil 表示未接入平台解除占用能力，此时删除被占用文件按失败处理。
	ReleaseLocks func(absPath string, confirm func(absPath string, holderNames []string) bool) error
	// SameVolume 判断两个路径是否同卷；nil 时按 filepath.VolumeName 比较大小写不敏感。
	SameVolume func(a, b string) (bool, error)
	// Link 创建硬链接；nil 时退化为 os.Link。
	Link func(oldname, newname string) error
}

// ExecOptions 是执行参数。
type ExecOptions struct {
	// DryRun 只创建目录、不移动/删除/改属性。
	// 与实测原版一致：试运行进入整理步骤后仍会 Directory.CreateDirectory
	// （B/E.cs 先建目录再判断 c_1 试运行开关），因此“试运行零落盘”并不成立。
	DryRun          bool
	ContinueOnError bool // 默认 false：首条失败后其余动作记为 skipped
	// ReleaseLock 对应原版“解除文件占用”开关（d.D）。
	// false 时被占用文件直接判失败，不结束任何进程；true 时才尝试关闭/结束占用进程。
	ReleaseLock bool
}

// ExecReport 是执行统计。
//
// Total 只统计可执行动作（Suggest 不落盘也不计数）。DryRun 时全部可执行动作计入
// Total 与 Skipped，Applied/Failed 恒为 0。Errors 每条失败一行，格式为
// “动作 序号 类型 from → to: 错误”（无 To 的动作省略箭头段）。
type ExecReport struct {
	Total   int
	Applied int
	Failed  int
	Skipped int
	Errors  []string
}

// 内部哨兵：表示“动作无需执行”，按 skipped 而非 failed 计。
var errAlreadyWritable = errors.New("文件已是可写")

// Apply 按 Result.Steps 顺序把动作落到 root 下。
// 只有无法开始执行（根目录为空/不存在/不是目录）时才返回 error；
// 单条动作的失败写入 ExecReport，不影响返回值。
func Apply(root string, res Result, opt ExecOptions, hooks ExecHooks) (ExecReport, error) {
	var rep ExecReport
	absRoot, err := execRoot(root)
	if err != nil {
		return rep, err
	}

	aborted := false
	seq := 0 // 动作在计划中的序号（含 Suggest），仅用于错误定位
	for _, step := range res.Steps {
		for i := range step.Actions {
			a := step.Actions[i]
			seq++
			if a.Kind == ActionSuggest {
				// 对齐原版“存疑项仅报告”的行为：不落盘，也不计入任何统计。
				continue
			}
			rep.Total++
			// 对齐原版演练开关（A_1.c_1）：只打印计划，不改动文件；
			// 唯一例外是目录创建——原版在试运行下也会建好目标目录。
			if opt.DryRun {
				if a.Kind == ActionMove || a.Kind == ActionEnsureDir {
					if err := dryRunMkdir(absRoot, a); err != nil {
						rep.Failed++
						rep.Errors = append(rep.Errors, execErrorLine(seq, a, err))
					}
				}
				rep.Skipped++
				continue
			}
			if aborted {
				rep.Skipped++
				continue
			}
			if err := applyOne(absRoot, a, opt, hooks); err != nil {
				if errors.Is(err, errAlreadyWritable) {
					rep.Skipped++
					continue
				}
				rep.Failed++
				rep.Errors = append(rep.Errors, execErrorLine(seq, a, err))
				if !opt.ContinueOnError {
					aborted = true
				}
				continue
			}
			rep.Applied++
		}
	}
	return rep, nil
}

// execRoot 校验并规范化根目录；不做任何写操作（DryRun 也走这里）。
func execRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("organize: 根目录为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("organize: 解析根目录失败: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("organize: 根目录不可访问: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("organize: 根目录不是目录: %s", abs)
	}
	return abs, nil
}

// resolveRel 把“相对根目录、'/' 分隔”的路径解析为绝对路径，并拒绝任何越界。
func resolveRel(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("路径为空")
	}
	// 绝对路径、盘符路径、以分隔符开头的路径一律拒绝（原版的所有动作均为根内相对路径）。
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" ||
		strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return "", fmt.Errorf("路径必须是根目录内的相对路径：%q", rel)
	}
	joined := filepath.Join(root, filepath.FromSlash(rel))
	back, err := filepath.Rel(root, joined)
	if err != nil {
		return "", fmt.Errorf("解析相对路径失败：%q: %w", rel, err)
	}
	// Join 会折叠 “..”，因此这里只需检查结果是否仍在根目录内。
	if back == "." || back == ".." ||
		strings.HasPrefix(back, ".."+string(filepath.Separator)) || filepath.IsAbs(back) {
		return "", fmt.Errorf("路径逃逸根目录：%q", rel)
	}
	// 对齐原版长路径归一化后的上限：Windows 单路径最多 32767 个 UTF-16 码元。
	if utf16Len(joined) > 32767 {
		return "", fmt.Errorf("路径超过 Windows 长路径上限：%q", rel)
	}
	return joined, nil
}

// applyOne 执行单条动作；调用方已保证非 DryRun。
func applyOne(root string, a Action, opt ExecOptions, hooks ExecHooks) error {
	switch a.Kind {
	case ActionEnsureDir:
		// 对齐原版 e_m_1 入口处的 Directory.CreateDirectory(A_1.a)。
		if a.To == "" {
			return errors.New("目标目录为空")
		}
		dir, err := resolveRel(root, a.To)
		if err != nil {
			return err
		}
		return os.MkdirAll(dir, 0o755)

	case ActionRename, ActionMove:
		// 对齐原版 b_m_1：先 Directory.CreateDirectory 目标目录，再判断目标是否存在，
		// 最后 File.Move。原版在此按冲突策略兜底；本层规划期已解决冲突，故目标已存在
		// 时不再猜测策略，直接按失败记录。
		from, err := resolveRel(root, a.From)
		if err != nil {
			return err
		}
		if a.To == "" {
			return errors.New("目标路径为空")
		}
		to, err := resolveRel(root, a.To)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if _, statErr := os.Lstat(to); statErr == nil {
			return fmt.Errorf("目标已存在：%s", a.To)
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return statErr
		}
		return os.Rename(from, to)

	case ActionDelete:
		// 对齐原版特殊文件删除与去重删除：按 a.A 的选择走回收站或永久删除。
		from, err := resolveRel(root, a.From)
		if err != nil {
			return err
		}
		if err := ensureDeletable(from, opt, hooks); err != nil {
			return err
		}
		if hooks.Trash != nil {
			return hooks.Trash(from)
		}
		return os.Remove(from)

	case ActionHardlink:
		// 对齐 a/K.cs：把重复文件替换为指向保留文件的硬链接。
		if a.To == "" {
			return errors.New("硬链接目标为空")
		}
		from, err := resolveRel(root, a.From)
		if err != nil {
			return err
		}
		to, err := resolveRel(root, a.To)
		if err != nil {
			return err
		}
		return hardlinkMerge(from, to, hooks)

	case ActionClearReadonly:
		// 对齐原版 c()：File.GetAttributes 含 ReadOnly 才 SetAttributes 清位。
		// 已经可写的文件按“跳过”计，不算失败（原版只统计去掉只读属性的数量）。
		from, err := resolveRel(root, a.From)
		if err != nil {
			return err
		}
		info, err := os.Stat(from)
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if perm&0o200 != 0 {
			return errAlreadyWritable
		}
		// Windows 上只读属性对应 Perm()=0444，加上 0o200 即可清除；
		// Linux/macOS 上则直接补上属主写位。
		return os.Chmod(from, perm|0o200)

	case ActionRemoveDir:
		// 对齐原版空目录清理：只在目录为空时 Directory.Delete，绝不递归删除。
		// 非空目录会失败，由调用方记入 Errors。
		dir, err := resolveRel(root, a.From)
		if err != nil {
			return err
		}
		return os.Remove(dir)

	default:
		return fmt.Errorf("不支持的动作类型：%q", a.Kind)
	}
}

// dryRunMkdir 复刻原版试运行下仍会发生的目录创建。
func dryRunMkdir(root string, a Action) error {
	target := a.To
	if target == "" {
		return nil
	}
	dir, err := resolveRel(root, target)
	if err != nil {
		return err
	}
	if a.Kind == ActionMove {
		dir = filepath.Dir(dir)
	}
	return os.MkdirAll(dir, 0o755)
}

// execErrorLine 生成一行失败记录：“动作 序号 类型 from → to: 错误”。
func execErrorLine(seq int, a Action, err error) string {
	line := fmt.Sprintf("动作 %d %s", seq, a.Kind)
	if a.From != "" {
		line += " " + a.From
	}
	if a.To != "" {
		line += " → " + a.To
	}
	return line + ": " + err.Error()
}

// ensureDeletable 对齐 B/E.cs B_m_1：删除前先看文件是否被占用。
// 未占用 → 放行；占用且未开启解除 → 直接失败；开启解除 → 由注入的平台能力尝试解除。
func ensureDeletable(path string, opt ExecOptions, hooks ExecHooks) error {
	if !hooks.isLocked(path) {
		return nil
	}
	if !opt.ReleaseLock {
		return fmt.Errorf("文件被其它进程占用：%s", filepath.Base(path))
	}
	if err := hooks.releaseLocks(path); err != nil {
		return fmt.Errorf("解除文件占用失败：%w", err)
	}
	return nil
}

// isLocked 调用注入的占用检测；未注入时视为未占用。
func (h ExecHooks) isLocked(absPath string) bool {
	if h.IsLocked == nil {
		return false
	}
	return h.IsLocked(absPath)
}

// releaseLocks 调用注入的解除占用能力，并把 ConfirmLock 透传给平台层。
func (h ExecHooks) releaseLocks(absPath string) error {
	if h.ReleaseLocks == nil {
		return errors.New("未接入平台解除占用能力")
	}
	return h.ReleaseLocks(absPath, h.ConfirmLock)
}

// sameVolume 判断同卷；未注入时按盘符兜底（大小写不敏感）。
func (h ExecHooks) sameVolume(a, b string) (bool, error) {
	if h.SameVolume != nil {
		return h.SameVolume(a, b)
	}
	return strings.EqualFold(filepath.VolumeName(a), filepath.VolumeName(b)), nil
}

// link 创建硬链接；未注入时退化为 os.Link。
func (h ExecHooks) link(oldname, newname string) error {
	if h.Link != nil {
		return h.Link(oldname, newname)
	}
	return os.Link(oldname, newname)
}

// hardlinkMerge 对齐 a/K.cs L452-547：
// 同卷校验 → 双向存在性 → 改名到 GUID 备份 → 建硬链接 → 删备份；
// 建链接失败时把备份改回原名（回滚），删备份失败只残留一个额外链接。
func hardlinkMerge(src, keep string, hooks ExecHooks) error {
	same, err := hooks.sameVolume(src, keep)
	if err != nil {
		return fmt.Errorf("同卷判定失败：%w", err)
	}
	if !same {
		return errors.New("跨卷，无法创建硬链接")
	}
	for _, p := range []string{keep, src} {
		if _, statErr := os.Lstat(p); statErr != nil {
			return fmt.Errorf("文件不存在：%s（%w）", filepath.Base(p), statErr)
		}
	}
	bak, err := backupName(src)
	if err != nil {
		return err
	}
	if err := renameWithRetry(src, bak); err != nil {
		return fmt.Errorf("改名备份失败：%w", err)
	}
	if err := hooks.link(keep, src); err != nil {
		if rerr := renameWithRetry(bak, src); rerr != nil {
			return fmt.Errorf("建链接失败（%v），回滚失败（%v），备份保留在 %s", err, rerr, bak)
		}
		return fmt.Errorf("建链接失败：%w", err)
	}
	// 原版 R17：删备份失败只残留一个额外硬链接，不影响数据完整性。
	_ = os.Remove(bak)
	return nil
}

// backupName 生成与原版同级的 GUID 备份名，避免与现有文件冲突。
func backupName(src string) (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成备份名失败：%w", err)
	}
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, "."+stem+".fdedup-bak-"+hex.EncodeToString(buf[:])+ext), nil
}

// renameWithRetry 对齐原版 A_m_1：4 次改名重试，仍失败则复制后删除源文件。
func renameWithRetry(from, to string) error {
	var last error
	for i := 0; i < 4; i++ {
		if err := os.Rename(from, to); err == nil {
			return nil
		} else {
			last = err
		}
		if i < 3 {
			time.Sleep(120 * time.Millisecond)
		}
	}
	if err := copyFile(from, to); err != nil {
		return last
	}
	return os.Remove(from)
}

// copyFile 用于改名失败后的兜底（对齐原版 File.Copy + File.Delete）。
func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
