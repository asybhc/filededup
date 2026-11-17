//go:build desktop

package main

// 整理执行的“最近一次撤销”持久化。
//
// 只保留最近一次真实执行（非试运行）且产生过反向动作的记录；新的一次可撤销
// 执行会覆盖旧记录。删除/硬链接替换无法还原，回滚时如实计入 unrestorable。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"filededup/internal/brand"
	"filededup/internal/engine/organize"
)

const organizeUndoVersion = 1

type organizeUndoFile struct {
	SchemaVersion int                   `json:"schemaVersion"`
	ID            string                `json:"id"`
	Root          string                `json:"root"`
	StartedAt     string                `json:"startedAt"`
	Records       []organize.UndoRecord `json:"records"`
}

// OrganizeUndoInfo 供界面判断“撤销上次整理”是否可用。
type OrganizeUndoInfo struct {
	Available bool   `json:"available"`
	ID        string `json:"id,omitempty"`
	Root      string `json:"root,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	Records   int    `json:"records"`
}

// OrganizeUndoResult 是一次回滚的统计。
type OrganizeUndoResult struct {
	ID           string   `json:"id"`
	Root         string   `json:"root"`
	Total        int      `json:"total"`
	Restored     int      `json:"restored"`
	Failed       int      `json:"failed"`
	Skipped      int      `json:"skipped"`
	Unrestorable int      `json:"unrestorable"`
	Errors       []string `json:"errors"`
}

// organizeUndoJournal 在执行线程内累计反向动作，执行结束后一次性落盘。
type organizeUndoJournal struct {
	records []organize.UndoRecord
}

func (j *organizeUndoJournal) record(r organize.UndoRecord) {
	j.records = append(j.records, r)
}

func (j *organizeUndoJournal) snapshot() []organize.UndoRecord {
	return append([]organize.UndoRecord(nil), j.records...)
}

func organizeUndoPath() (string, error) {
	return brand.DataSub("organize-undo.json")
}

func saveOrganizeUndo(id, root string, started time.Time, records []organize.UndoRecord) error {
	if len(records) == 0 {
		return nil
	}
	p, err := organizeUndoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(organizeUndoFile{
		SchemaVersion: organizeUndoVersion,
		ID:            id,
		Root:          root,
		StartedAt:     started.Format(time.RFC3339),
		Records:       records,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	// Windows 的 os.Rename 不能覆盖已存在文件，先删除再替换。
	if err := os.Rename(tmp, p); err != nil {
		if rmErr := os.Remove(p); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			os.Remove(tmp)
			return err
		}
		if err := os.Rename(tmp, p); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return nil
}

func loadOrganizeUndo() (organizeUndoFile, error) {
	var f organizeUndoFile
	p, err := organizeUndoPath()
	if err != nil {
		return f, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("撤销记录损坏：%w", err)
	}
	if f.SchemaVersion != organizeUndoVersion {
		return f, fmt.Errorf("撤销记录版本 %d 不受支持", f.SchemaVersion)
	}
	return f, nil
}

// GetOrganizeUndoInfo 返回最近一次可撤销整理的概要。
func (a *App) GetOrganizeUndoInfo() (OrganizeUndoInfo, error) {
	f, err := loadOrganizeUndo()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OrganizeUndoInfo{}, nil
		}
		return OrganizeUndoInfo{}, err
	}
	return OrganizeUndoInfo{
		Available: len(f.Records) > 0,
		ID:        f.ID,
		Root:      f.Root,
		StartedAt: f.StartedAt,
		Records:   len(f.Records),
	}, nil
}

// UndoLastOrganize 回滚最近一次真实整理。删除项无法还原，会计入 Unrestorable。
func (a *App) UndoLastOrganize() (OrganizeUndoResult, error) {
	a.undoMu.Lock()
	defer a.undoMu.Unlock()

	f, err := loadOrganizeUndo()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OrganizeUndoResult{}, errors.New("没有可撤销的整理记录")
		}
		return OrganizeUndoResult{}, err
	}
	if len(f.Records) == 0 {
		return OrganizeUndoResult{}, errors.New("没有可撤销的整理记录")
	}
	rep, err := organize.Undo(f.Root, f.Records, organize.ExecOptions{})
	if err != nil {
		return OrganizeUndoResult{}, fmt.Errorf("撤销失败：%w", err)
	}
	res := OrganizeUndoResult{
		ID:           f.ID,
		Root:         f.Root,
		Total:        rep.Total,
		Restored:     rep.Restored,
		Failed:       rep.Failed,
		Skipped:      rep.Skipped,
		Unrestorable: rep.Unrestorable,
		Errors:       append([]string(nil), rep.Errors...),
	}
	// 全部成功才删除记录；有失败时保留，修好阻塞后可重试（重复撤销会按跳过处理）。
	if rep.Failed == 0 {
		if p, perr := organizeUndoPath(); perr == nil {
			if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				res.Errors = append(res.Errors, "撤销记录清理失败："+rerr.Error())
			}
		}
	}
	return res, nil
}
