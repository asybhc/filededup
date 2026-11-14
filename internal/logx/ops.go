// Package logx 负责运行日志与操作清单（撤销依据）。
//
// M2 交付 ops JSONL：每条动作一行，包含 planId/seq/kind/src/dst/status/error
// 与前后指纹，供"出问题后人工核对/撤销"使用。撤销本身属后续里程碑。
package logx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"filededup/internal/engine/model"
)

// Record 是操作清单的一行。
type Record struct {
	TS     string             `json:"ts"`
	PlanID string             `json:"planId"`
	Seq    int                `json:"seq"`
	Kind   model.ActionKind   `json:"kind"`
	Src    string             `json:"src"`
	Dst    string             `json:"dst,omitempty"`
	Status model.ActionStatus `json:"status"`
	Error  string             `json:"error,omitempty"`
	Pre    *model.Precheck    `json:"pre,omitempty"`
	Post   *model.Precheck    `json:"post,omitempty"`
}

// OpsWriter 把动作终态逐行追加到 ops_YYYYMMDD.jsonl。
type OpsWriter struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	planID string
	now    func() time.Time
}

// DefaultDir 返回默认操作清单目录：<UserConfigDir>/filededup/logs/ops。
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "filededup", "logs", "ops"), nil
}

// OpenOps 打开（或创建）当天的操作清单。dir 为空时使用 DefaultDir。
func OpenOps(dir, planID string, now time.Time) (*OpsWriter, error) {
	if dir == "" {
		d, err := DefaultDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("ops_%s.jsonl", now.UTC().Format("20060102")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &OpsWriter{f: f, path: path, planID: planID, now: time.Now}, nil
}

// Path 返回当前写入的文件路径。
func (w *OpsWriter) Path() string { return w.path }

// Record 实现 apply.Journal。
func (w *OpsWriter) Record(a model.Action) error {
	now := w.now
	if now == nil {
		now = time.Now
	}
	rec := Record{
		TS:     now().UTC().Format(time.RFC3339Nano),
		PlanID: w.planID,
		Seq:    a.Seq,
		Kind:   a.Kind,
		Src:    a.Src,
		Dst:    a.Dst,
		Status: a.Status,
		Error:  a.Err,
		Pre:    a.Precheck,
		Post:   postFingerprint(a),
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.f.Write(raw)
	return err
}

// Close 关闭文件；重复调用安全。
func (w *OpsWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// postFingerprint 记录动作执行后的落点指纹：移动/改名取 Dst，其余取 Src（已删除则为 nil）。
func postFingerprint(a model.Action) *model.Precheck {
	target := a.Src
	switch a.Kind {
	case model.ActionMove, model.ActionRename:
		if a.Dst != "" {
			target = a.Dst
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil
	}
	return &model.Precheck{
		Size:    info.Size(),
		ModTime: info.ModTime().UTC().Format(time.RFC3339Nano),
	}
}
