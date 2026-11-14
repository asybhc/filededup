// Package apply 是计划执行器：串行执行 model.Plan，逐条复核、逐条回滚。
//
// 依赖注入：回收站实现由外壳传入（engine 不得依赖 platform）；操作清单通过
// Journal 接口落盘（实现见 internal/logx）。本包不读时钟以外的时间，不联网。
//
// 安全规则：
//   - 每条动作执行前必须通过预检（大小、修改时间；永久删除额外复核 SHA-256）；
//   - 单条动作内部的中途失败自行回滚（改名/移动反向、硬链接删新链、只读恢复属性）；
//   - 任一动作失败后默认中止后续动作，已完成的动作保留并写日志（不跨动作回滚）。
package apply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filededup/internal/engine/model"
)

// TrashFunc 把路径移入回收站。由外壳注入平台实现。
type TrashFunc func(path string) error

// Journal 记录动作终态（操作清单，撤销依据）。
type Journal interface {
	Record(model.Action) error
}

// Options 是执行参数。
type Options struct {
	DryRun          bool // 只做预检，不改动文件系统
	ContinueOnError bool // 单条失败后继续（默认中止）
	Now             func() time.Time
}

// Report 是执行结果。Actions 是计划动作的终态副本（计划本身保持不变）。
type Report struct {
	SchemaVersion int            `json:"schemaVersion"`
	RequestID     string         `json:"requestId"`
	DryRun        bool           `json:"dryRun"`
	StartedAt     string         `json:"startedAt"`
	FinishedAt    string         `json:"finishedAt"`
	Total         int            `json:"total"`
	Applied       int            `json:"applied"`
	Failed        int            `json:"failed"`
	Skipped       int            `json:"skipped"`
	DryRuns       int            `json:"dryRuns"`
	RolledBack    int            `json:"rolledBack"`
	JournalError  string         `json:"journalError,omitempty"`
	Actions       []model.Action `json:"actions"`
}

// Executor 串行执行计划。
type Executor struct {
	Trash   TrashFunc
	Journal Journal
}

// 内部哨兵：表示"动作无需执行"，按 skipped 而非 failed 计。
var (
	errAlreadyWritable = errors.New("文件已是可写")
	errAlreadyExists   = errors.New("目标目录已存在")
)

// Execute 按序号执行计划。ctx 取消时剩余动作记为 skipped 并返回 ctx.Err()。
func (e *Executor) Execute(ctx context.Context, p *model.Plan, opt Options) (*Report, error) {
	if p == nil {
		return nil, errors.New("apply: 计划为空")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	trash := e.Trash
	if trash == nil {
		trash = func(string) error { return errors.New("apply: 未注入回收站实现") }
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	started := now()
	r := &Report{
		SchemaVersion: model.SchemaVersion,
		RequestID:     p.RequestID,
		DryRun:        opt.DryRun,
		StartedAt:     started.UTC().Format(time.RFC3339Nano),
		Total:         len(p.Actions),
		Actions:       make([]model.Action, len(p.Actions)),
	}
	copy(r.Actions, p.Actions)

	aborted := false
	for i := range r.Actions {
		a := &r.Actions[i]
		if err := ctx.Err(); err != nil {
			a.Status = model.StatusSkipped
			a.Err = "已取消"
			r.Skipped++
			continue
		}
		if aborted {
			a.Status = model.StatusSkipped
			a.Err = "已中止：前序动作失败"
			r.Skipped++
			continue
		}
		if err := precheck(a); err != nil {
			a.Status = model.StatusSkipped
			a.Err = "执行前复核失败：" + err.Error()
			r.Skipped++
			e.record(r, *a)
			continue
		}
		if opt.DryRun {
			a.Status = model.StatusDryRun
			r.DryRuns++
			continue // 演练不写操作清单（未改动文件系统）
		}

		rolledBack, err := e.applyOne(trash, a)
		if err == nil {
			a.Status = model.StatusApplied
			r.Applied++
			e.record(r, *a)
			continue
		}
		switch {
		case errors.Is(err, errAlreadyWritable), errors.Is(err, errAlreadyExists):
			a.Status = model.StatusSkipped
			a.Err = err.Error()
			r.Skipped++
		default:
			a.Status = model.StatusFailed
			a.Err = err.Error()
			r.Failed++
			if rolledBack {
				r.RolledBack++
			}
			if !opt.ContinueOnError {
				aborted = true
			}
		}
		e.record(r, *a)
	}

	r.FinishedAt = now().UTC().Format(time.RFC3339Nano)
	if err := ctx.Err(); err != nil {
		return r, err
	}
	return r, nil
}

// precheck 复核文件指纹。永久删除额外复核 SHA-256（不可逆，代价可接受）。
func precheck(a *model.Action) error {
	if a.Precheck == nil {
		if a.Kind == model.ActionDeleteTrash || a.Kind == model.ActionDeletePermanent {
			return errors.New("缺少预检快照")
		}
		return nil
	}
	info, err := os.Stat(a.Src)
	if err != nil {
		return err
	}
	if info.Size() != a.Precheck.Size {
		return fmt.Errorf("大小已变化：计划 %d，当前 %d", a.Precheck.Size, info.Size())
	}
	if a.Precheck.ModTime != "" {
		want, err := time.Parse(time.RFC3339Nano, a.Precheck.ModTime)
		if err != nil {
			return fmt.Errorf("快照时间非法：%w", err)
		}
		if !info.ModTime().UTC().Equal(want.UTC()) {
			return fmt.Errorf("修改时间已变化：计划 %s，当前 %s",
				want.UTC().Format(time.RFC3339Nano), info.ModTime().UTC().Format(time.RFC3339Nano))
		}
	}
	if a.Kind == model.ActionDeletePermanent && a.Precheck.Hash != "" {
		got, err := hashFile(a.Src)
		if err != nil {
			return err
		}
		if !strings.EqualFold(got, a.Precheck.Hash) {
			return fmt.Errorf("内容哈希已变化：计划 %s，当前 %s", a.Precheck.Hash, got)
		}
	}
	return nil
}

// applyOne 执行单条动作。失败时若已改动文件系统，内部先回滚并返回 rolledBack=true。
func (e *Executor) applyOne(trash TrashFunc, a *model.Action) (rolledBack bool, err error) {
	switch a.Kind {
	case model.ActionDeleteTrash:
		return false, trash(a.Src)

	case model.ActionDeletePermanent:
		// Windows 上直接 DeleteFile 会因只读属性失败（原版 File.Delete 同样抛异常）；
		// Go 的 os.Remove 会自动清除只读属性，这里显式拦下，保持"安全失败 + 明确提示"。
		info, err := os.Stat(a.Src)
		if err != nil {
			return false, err
		}
		if info.Mode().Perm()&0o200 == 0 {
			return false, fmt.Errorf("只读文件不能永久删除，需先执行 clear_readonly：%s", a.Src)
		}
		return false, os.Remove(a.Src)

	case model.ActionRename, model.ActionMove:
		if a.Dst == "" {
			return false, errors.New("目标路径为空")
		}
		if a.Kind == model.ActionMove {
			if err := os.MkdirAll(filepath.Dir(a.Dst), 0o755); err != nil {
				return false, err
			}
		}
		if _, statErr := os.Lstat(a.Dst); statErr == nil {
			return false, fmt.Errorf("目标已存在：%s", a.Dst)
		}
		if err := os.Rename(a.Src, a.Dst); err != nil {
			return false, err
		}
		return false, nil

	case model.ActionHardlink:
		if a.Dst == "" {
			return false, errors.New("硬链接源路径为空")
		}
		backup := a.Src + ".fdedup-bak"
		if _, statErr := os.Lstat(backup); statErr == nil {
			return false, fmt.Errorf("备份路径已存在：%s", backup)
		}
		if err := os.Rename(a.Src, backup); err != nil {
			return false, err
		}
		if err := os.Link(a.Dst, a.Src); err != nil {
			if restoreErr := os.Rename(backup, a.Src); restoreErr != nil {
				return true, fmt.Errorf("建链失败且恢复失败：%v（恢复错误：%v）", err, restoreErr)
			}
			return true, err
		}
		if err := os.Remove(backup); err != nil {
			_ = os.Remove(a.Src)
			if restoreErr := os.Rename(backup, a.Src); restoreErr != nil {
				return true, fmt.Errorf("删除备份失败且恢复失败：%v（恢复错误：%v）", err, restoreErr)
			}
			return true, err
		}
		return false, nil

	case model.ActionClearReadonly:
		info, err := os.Stat(a.Src)
		if err != nil {
			return false, err
		}
		perm := info.Mode().Perm()
		if perm&0o200 != 0 {
			return false, errAlreadyWritable
		}
		if err := os.Chmod(a.Src, perm|0o200); err != nil {
			return false, err
		}
		return false, nil

	case model.ActionMkdir:
		if info, err := os.Stat(a.Src); err == nil && info.IsDir() {
			return false, errAlreadyExists
		}
		if err := os.MkdirAll(a.Src, 0o755); err != nil {
			return false, err
		}
		return false, nil

	case model.ActionRemoveDir:
		return false, os.Remove(a.Src)

	default:
		return false, fmt.Errorf("不支持的动作类型：%q", a.Kind)
	}
}

// record 写操作清单；失败不中断执行，首个错误记录到 Report.JournalError。
func (e *Executor) record(r *Report, a model.Action) {
	if e.Journal == nil {
		return
	}
	if err := e.Journal.Record(a); err != nil && r.JournalError == "" {
		r.JournalError = err.Error()
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
