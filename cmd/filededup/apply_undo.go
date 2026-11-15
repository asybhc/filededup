//go:build desktop

package main

// 查重执行的“最近一次可撤销删除”持久化。
//
// 与整理侧的 organize-undo.json 是两个独立文件，互不覆盖：
//   - 只记录真实执行（非试运行）且成功的删除动作；
//   - 只保留最近一次“有回收站删除”的执行；如果新的执行只做了永久删除，
//     不会覆盖旧记录（旧的回收站删除仍然可以撤销）；
//   - 永久删除不可撤销，记录里逐条标 Unrestorable 并给出原因，不尝试伪造恢复。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"filededup/internal/engine/apply"
	"filededup/internal/engine/model"
	"filededup/internal/platform"
)

const (
	dedupUndoVersion = 1
	// dedupUndoListLimit 限制界面一次展示的明细条数，避免上万条把界面拖垮。
	dedupUndoListLimit = 200
)

// dedupUndoEntry 是一个已成功执行、可尝试撤销的删除动作。
type dedupUndoEntry struct {
	Path      string `json:"path"`
	Dir       string `json:"dir,omitempty"`
	Size      int64  `json:"size,omitempty"`
	ModTime   string `json:"modTime,omitempty"`
	DeletedAt string `json:"deletedAt,omitempty"`
	Reason    string `json:"reason,omitempty"`
	// Unrestorable 为 true 表示永久删除，撤销时只能报告，不能还原。
	Unrestorable       bool   `json:"unrestorable,omitempty"`
	UnrestorableReason string `json:"unrestorableReason,omitempty"`
}

// dedupUndoFile 是 dedup-undo.json 的结构。
type dedupUndoFile struct {
	SchemaVersion int              `json:"schemaVersion"`
	RequestID     string           `json:"requestId"`
	StartedAt     string           `json:"startedAt"`
	FinishedAt    string           `json:"finishedAt"`
	DeleteMode    string           `json:"deleteMode"`
	Trash         []dedupUndoEntry `json:"trash"`
	Permanent     []dedupUndoEntry `json:"permanent"`
}

// DedupUndoItem 是界面展示用的单条明细。
type DedupUndoItem struct {
	Path    string `json:"path"`
	Dir     string `json:"dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime,omitempty"`
}

// DedupUndoInfo 供界面判断“撤销上次删除”是否可用。
type DedupUndoInfo struct {
	Available       bool            `json:"available"`
	RequestID       string          `json:"requestId,omitempty"`
	StartedAt       string          `json:"startedAt,omitempty"`
	FinishedAt      string          `json:"finishedAt,omitempty"`
	DeleteMode      string          `json:"deleteMode,omitempty"`
	Trash           int             `json:"trash"`
	Permanent       int             `json:"permanent"`
	PermanentReason string          `json:"permanentReason,omitempty"`
	Items           []DedupUndoItem `json:"items"`
	Truncated       bool            `json:"truncated"`
}

// DedupUndoResult 是一次回滚的统计。Unrestorable 是当时永久删除的条目数。
type DedupUndoResult struct {
	RequestID    string   `json:"requestId"`
	Total        int      `json:"total"`
	Restored     int      `json:"restored"`
	Failed       int      `json:"failed"`
	Unrestorable int      `json:"unrestorable"`
	BinItems     int      `json:"binItems"`
	BinEmpty     bool     `json:"binEmpty"`
	Errors       []string `json:"errors"`
}

// dedupUndoJournal 包装执行器日志：既把终态写进操作清单（sink），
// 又在内存里累计本次可撤销的删除动作，执行结束后一次性落盘。
type dedupUndoJournal struct {
	requestID string
	sink      apply.Journal
	trash     []dedupUndoEntry
	permanent []dedupUndoEntry
}

func (j *dedupUndoJournal) Record(a model.Action) error {
	var firstErr error
	if j.sink != nil {
		if err := j.sink.Record(a); err != nil {
			firstErr = err
		}
	}
	if a.Status != model.StatusApplied {
		return firstErr
	}
	entry := dedupUndoEntry{Path: a.Src, Dir: filepath.Dir(a.Src), Reason: a.Reason}
	if a.Precheck != nil {
		entry.Size = a.Precheck.Size
		entry.ModTime = a.Precheck.ModTime
	}
	switch a.Kind {
	case model.ActionDeleteTrash:
		j.trash = append(j.trash, entry)
	case model.ActionDeletePermanent:
		entry.Unrestorable = true
		entry.UnrestorableReason = "永久删除未进入回收站，无法撤销"
		j.permanent = append(j.permanent, entry)
	}
	return firstErr
}

func dedupUndoPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "FileDedup", "dedup-undo.json"), nil
}

// saveDedupUndo 落盘本次执行记录。没有回收站删除动作时不写，
// 以免覆盖掉上一次仍可撤销的记录。
func saveDedupUndo(j *dedupUndoJournal, started, finished time.Time, mode model.DeleteMode) error {
	if j == nil || len(j.trash) == 0 {
		return nil
	}
	p, err := dedupUndoPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	at := finished.UTC().Format(time.RFC3339)
	for i := range j.trash {
		j.trash[i].DeletedAt = at
	}
	for i := range j.permanent {
		j.permanent[i].DeletedAt = at
	}
	raw, err := json.MarshalIndent(dedupUndoFile{
		SchemaVersion: dedupUndoVersion,
		RequestID:     j.requestID,
		StartedAt:     started.UTC().Format(time.RFC3339),
		FinishedAt:    at,
		DeleteMode:    string(mode),
		Trash:         j.trash,
		Permanent:     j.permanent,
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

func loadDedupUndo() (dedupUndoFile, error) {
	var f dedupUndoFile
	p, err := dedupUndoPath()
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
	if f.SchemaVersion != dedupUndoVersion {
		return f, fmt.Errorf("撤销记录版本 %d 不受支持", f.SchemaVersion)
	}
	return f, nil
}

// GetDedupUndoInfo 返回最近一次可撤销删除的概要。
func (a *App) GetDedupUndoInfo() (DedupUndoInfo, error) {
	f, err := loadDedupUndo()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DedupUndoInfo{Items: []DedupUndoItem{}}, nil
		}
		return DedupUndoInfo{}, err
	}
	info := DedupUndoInfo{
		Available:  len(f.Trash) > 0,
		RequestID:  f.RequestID,
		StartedAt:  f.StartedAt,
		FinishedAt: f.FinishedAt,
		DeleteMode: f.DeleteMode,
		Trash:      len(f.Trash),
		Permanent:  len(f.Permanent),
		Items:      make([]DedupUndoItem, 0, len(f.Trash)),
	}
	if len(f.Permanent) > 0 {
		info.PermanentReason = "永久删除未进入回收站，无法撤销"
	}
	for i, e := range f.Trash {
		if i >= dedupUndoListLimit {
			info.Truncated = true
			break
		}
		info.Items = append(info.Items, DedupUndoItem{
			Path:    e.Path,
			Dir:     e.Dir,
			Size:    e.Size,
			ModTime: e.ModTime,
		})
	}
	return info, nil
}

// UndoLastDedup 把最近一次查重执行中“移入回收站”的文件恢复到原路径。
// 永久删除的条目无法恢复，只计入 Unrestorable。
func (a *App) UndoLastDedup() (DedupUndoResult, error) {
	// 与整理侧撤销共用一把锁：两者都可能恢复/移动同一批文件。
	a.undoMu.Lock()
	defer a.undoMu.Unlock()

	f, err := loadDedupUndo()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DedupUndoResult{}, errors.New("没有可撤销的查重删除记录")
		}
		return DedupUndoResult{}, err
	}
	if len(f.Trash) == 0 {
		return DedupUndoResult{}, errors.New("没有可撤销的查重删除记录")
	}

	paths := make([]string, 0, len(f.Trash))
	for _, e := range f.Trash {
		paths = append(paths, e.Path)
	}
	rep := platform.RestoreFromTrash(paths)

	res := DedupUndoResult{
		RequestID:    f.RequestID,
		Total:        len(paths),
		Unrestorable: len(f.Permanent),
		BinItems:     rep.BinItems,
		BinEmpty:     rep.BinItems == 0,
		Errors:       []string{},
	}
	// 恢复结果按路径对回输入顺序；平台实现保证一一对应，这里仍按路径查找以容错。
	byPath := make(map[string]platform.RestoreResult, len(rep.Results))
	for _, r := range rep.Results {
		byPath[r.Path] = r
	}
	failed := make([]dedupUndoEntry, 0, len(f.Trash))
	for _, e := range f.Trash {
		r, ok := byPath[e.Path]
		switch {
		case ok && r.OK:
			res.Restored++
		default:
			res.Failed++
			reason := "未返回恢复结果"
			if ok && r.Err != "" {
				reason = r.Err
			}
			if res.BinEmpty {
				reason = "回收站已被清空，无法恢复"
			}
			res.Errors = append(res.Errors, e.Path+"："+reason)
			failed = append(failed, e)
		}
	}
	if res.Unrestorable > 0 {
		res.Errors = append(res.Errors,
			fmt.Sprintf("另有 %d 个文件是永久删除，无法撤销", res.Unrestorable))
	}

	// 全部恢复成功才删除记录；有失败时保留失败项，修好阻塞后可重试。
	if res.Failed == 0 {
		if p, perr := dedupUndoPath(); perr == nil {
			if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
				res.Errors = append(res.Errors, "撤销记录清理失败："+rerr.Error())
			}
		}
	} else {
		f.Trash = failed
		if p, perr := dedupUndoPath(); perr == nil {
			raw, merr := json.MarshalIndent(f, "", "  ")
			if merr == nil {
				if werr := os.WriteFile(p, append(raw, '\n'), 0o600); werr != nil {
					res.Errors = append(res.Errors, "撤销记录回写失败："+werr.Error())
				}
			}
		}
	}
	return res, nil
}
