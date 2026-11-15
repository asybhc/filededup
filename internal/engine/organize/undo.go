package organize

// 本文件提供整理执行的“反向动作”记录与回滚。
//
// 记录由 exec.go 在真实执行成功后通过 ExecHooks.Undo 交给调用方持久化；
// 回滚时按记录逆序执行。删除（回收站/永久）与硬链接替换无法还原，
// 记录为 unrestorable，回滚统计里如实计入，不假装成功。

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// UndoKind 是一条撤销记录的动作类型。
type UndoKind string

const (
	// UndoMoveBack 把 From（当前路径）改回 To（原路径）。
	UndoMoveBack UndoKind = "move_back"
	// UndoMkdirBack 重新创建被清理的空目录。
	UndoMkdirBack UndoKind = "mkdir_back"
	// UndoRemoveDirIfEmpty 删除本次执行新建的目录；非空时跳过。
	UndoRemoveDirIfEmpty UndoKind = "remove_dir_if_empty"
	// UndoRestoreMode 恢复文件权限位。
	UndoRestoreMode UndoKind = "restore_mode"
	// UndoUnrestorable 无法还原的动作（删除、硬链接替换）。
	UndoUnrestorable UndoKind = "unrestorable"
)

// UndoRecord 是一条可持久化的反向动作。路径均为相对根目录、'/' 分隔。
type UndoRecord struct {
	Kind   UndoKind `json:"kind"`
	From   string   `json:"from,omitempty"`
	To     string   `json:"to,omitempty"`
	Mode   uint32   `json:"mode,omitempty"`
	Reason string   `json:"reason,omitempty"`
}

// UndoReport 是回滚统计。
type UndoReport struct {
	Total        int      `json:"total"`
	Restored     int      `json:"restored"`
	Failed       int      `json:"failed"`
	Skipped      int      `json:"skipped"`
	Unrestorable int      `json:"unrestorable"`
	Errors       []string `json:"errors"`
}

var errUndoSkip = errors.New("无需撤销")

// Undo 按记录逆序回滚。根目录不可用时返回 error；单条失败写入报告。
func Undo(root string, records []UndoRecord, opt ExecOptions) (UndoReport, error) {
	rep := UndoReport{Total: len(records), Errors: []string{}}
	absRoot, err := execRoot(root)
	if err != nil {
		return rep, err
	}
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.Kind == UndoUnrestorable {
			rep.Unrestorable++
			continue
		}
		if err := undoOne(absRoot, rec); err != nil {
			if errors.Is(err, errUndoSkip) {
				rep.Skipped++
				continue
			}
			rep.Failed++
			rep.Errors = append(rep.Errors, fmt.Sprintf("第 %d 条 %s %s：%v", i+1, rec.Kind, rec.From, err))
			continue
		}
		rep.Restored++
	}
	return rep, nil
}

func undoOne(root string, rec UndoRecord) error {
	switch rec.Kind {
	case UndoMoveBack:
		cur, err := resolveRel(root, rec.From)
		if err != nil {
			return err
		}
		dst, err := resolveRel(root, rec.To)
		if err != nil {
			return err
		}
		_, curErr := os.Lstat(cur)
		_, dstErr := os.Lstat(dst)
		if errors.Is(curErr, fs.ErrNotExist) && dstErr == nil {
			// 已经还原过：把重复撤销当作跳过，便于失败后重试。
			return errUndoSkip
		}
		if curErr != nil {
			return fmt.Errorf("当前路径不可访问：%w", curErr)
		}
		if dstErr == nil {
			return fmt.Errorf("还原目标已存在：%s", rec.To)
		}
		if !errors.Is(dstErr, fs.ErrNotExist) {
			return dstErr
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.Rename(cur, dst)

	case UndoMkdirBack:
		d, err := resolveRel(root, rec.From)
		if err != nil {
			return err
		}
		return os.MkdirAll(d, 0o755)

	case UndoRemoveDirIfEmpty:
		d, err := resolveRel(root, rec.From)
		if err != nil {
			return err
		}
		entries, err := os.ReadDir(d)
		if errors.Is(err, fs.ErrNotExist) {
			return errUndoSkip
		}
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return fmt.Errorf("%w：目录非空，保留 %s", errUndoSkip, rec.From)
		}
		return os.Remove(d)

	case UndoRestoreMode:
		p, err := resolveRel(root, rec.From)
		if err != nil {
			return err
		}
		return os.Chmod(p, fs.FileMode(rec.Mode))

	default:
		return fmt.Errorf("未知撤销类型：%q", rec.Kind)
	}
}

// preUndo 在执行一条动作前采集反向动作所需的旧状态。
// 无法确定旧状态（路径解析失败等）时返回 nil，宁可少记也不记错。
func preUndo(root string, a Action) []UndoRecord {
	switch a.Kind {
	case ActionEnsureDir:
		if a.To == "" {
			return nil
		}
		p, err := resolveRel(root, a.To)
		if err != nil {
			return nil
		}
		return missingDirUndo(root, p)

	case ActionRename, ActionMove:
		if a.From == "" || a.To == "" {
			return nil
		}
		to, err := resolveRel(root, a.To)
		if err != nil {
			return nil
		}
		recs := missingDirUndo(root, filepath.Dir(to))
		return append(recs, UndoRecord{Kind: UndoMoveBack, From: a.To, To: a.From})

	case ActionDelete:
		if a.From == "" {
			return nil
		}
		return []UndoRecord{{
			Kind:   UndoUnrestorable,
			From:   a.From,
			Reason: "已删除（回收站或永久删除均无法由本程序还原）",
		}}

	case ActionClearReadonly:
		if a.From == "" {
			return nil
		}
		p, err := resolveRel(root, a.From)
		if err != nil {
			return nil
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil
		}
		perm := info.Mode().Perm()
		if perm&0o200 == 0 {
			return []UndoRecord{{Kind: UndoRestoreMode, From: a.From, Mode: uint32(perm)}}
		}
		return nil

	case ActionHardlink:
		if a.From == "" {
			return nil
		}
		return []UndoRecord{{
			Kind:   UndoUnrestorable,
			From:   a.From,
			Reason: "已替换为硬链接，原文件内容不可由本程序还原",
		}}

	case ActionRemoveDir:
		if a.From == "" {
			return nil
		}
		return []UndoRecord{{Kind: UndoMkdirBack, From: a.From}}
	}
	return nil
}

// missingDirUndo 记录 absDir 及其尚不存在的祖先目录，返回父目录在前的顺序，
// 便于逆序回滚时先撤销文件移动、再从深到浅删除新建目录。
func missingDirUndo(root, absDir string) []UndoRecord {
	var recs []UndoRecord
	for cur := absDir; ; cur = filepath.Dir(cur) {
		if _, err := os.Lstat(cur); !errors.Is(err, fs.ErrNotExist) {
			break
		}
		rel, err := filepath.Rel(root, cur)
		if err != nil || rel == "." || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			break
		}
		recs = append(recs, UndoRecord{Kind: UndoRemoveDirIfEmpty, From: filepath.ToSlash(rel)})
		if cur == root {
			break
		}
	}
	for i, j := 0, len(recs)-1; i < j; i, j = i+1, j-1 {
		recs[i], recs[j] = recs[j], recs[i]
	}
	return recs
}
