package plan

import (
	"errors"
	"testing"
	"time"

	"filededup/internal/engine/model"
)

func sampleResult() *model.Result {
	return &model.Result{
		SchemaVersion: model.SchemaVersion,
		RequestID:     "req-1",
		Groups: []model.Group{
			{
				ID:   "exact:aaa",
				Kind: model.GroupExact,
				Key:  "aaa",
				Keep: `C:\r\b.txt`,
				Members: []model.Member{
					{Path: `C:\r\b.txt`, Size: 10, ModTime: "2026-01-01T00:00:00Z", Hash: "aaa", Keep: true, Reason: "keep:pathShortest",
						Snapshot: &model.Precheck{Size: 10, ModTime: "2026-01-01T00:00:00Z", Hash: "aaa"}},
					{Path: `C:\r\a.txt`, Size: 10, ModTime: "2026-01-01T00:00:00Z", Hash: "aaa", Deletable: true, Reason: "duplicate"},
					{Path: `C:\r\c.txt`, Size: 10, ModTime: "2026-01-01T00:00:00Z", Hash: "aaa", Deletable: true, Reason: "duplicate"},
				},
				Reclaimable: 20,
			},
			{
				ID:   "exact:bbb",
				Kind: model.GroupExact,
				Key:  "bbb",
				Keep: `C:\r\d\keep.bin`,
				Members: []model.Member{
					{Path: `C:\r\d\dup.bin`, Size: 7, ModTime: "2026-01-02T00:00:00Z", Hash: "bbb", Deletable: true},
					{Path: `C:\r\d\keep.bin`, Size: 7, ModTime: "2026-01-02T00:00:00Z", Hash: "bbb", Keep: true},
				},
				Reclaimable: 7,
			},
		},
	}
}

func TestFromScanTrashDefaults(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC) }
	p, err := FromScan(sampleResult(), Options{Now: clock})
	if err != nil {
		t.Fatalf("生成计划失败：%v", err)
	}
	if p.SchemaVersion != model.SchemaVersion {
		t.Fatalf("schemaVersion=%d", p.SchemaVersion)
	}
	if p.RequestID != "req-1" {
		t.Fatalf("requestId=%q", p.RequestID)
	}
	if p.CreatedAt != "2026-11-14T09:00:00Z" {
		t.Fatalf("createdAt=%q", p.CreatedAt)
	}
	if p.Source != model.PlanFromScan {
		t.Fatalf("source=%q", p.Source)
	}
	if len(p.Actions) != 3 {
		t.Fatalf("动作数=%d，期望 3", len(p.Actions))
	}
	wantSrc := []string{`C:\r\a.txt`, `C:\r\c.txt`, `C:\r\d\dup.bin`}
	for i, a := range p.Actions {
		if a.Seq != i+1 {
			t.Fatalf("第 %d 条序号=%d", i+1, a.Seq)
		}
		if a.Src != wantSrc[i] {
			t.Fatalf("第 %d 条 src=%q，期望 %q", i+1, a.Src, wantSrc[i])
		}
		if a.Kind != model.ActionDeleteTrash {
			t.Fatalf("第 %d 条 kind=%q", i+1, a.Kind)
		}
		if a.Irreversible {
			t.Fatalf("回收站删除不应标记不可逆")
		}
		if a.Status != model.StatusPlanned {
			t.Fatalf("第 %d 条 status=%q", i+1, a.Status)
		}
		if a.Precheck == nil {
			t.Fatalf("第 %d 条缺少 precheck", i+1)
		}
	}
	if p.Actions[0].Precheck.Size != 10 || p.Actions[0].Precheck.Hash != "aaa" {
		t.Fatalf("第一条 precheck 未取自 Snapshot：%+v", p.Actions[0].Precheck)
	}
	if p.Actions[2].Precheck.Size != 7 {
		t.Fatalf("无 Snapshot 成员未回退为 size/modTime/hash：%+v", p.Actions[2].Precheck)
	}
	if p.Summary.Actions != 3 || p.Summary.Deletes != 3 || p.Summary.Trash != 3 {
		t.Fatalf("summary=%+v", p.Summary)
	}
	if p.Summary.ReclaimableBytes != 27 {
		t.Fatalf("reclaimableBytes=%d，期望 27", p.Summary.ReclaimableBytes)
	}
	if p.Summary.Irreversible != 0 {
		t.Fatalf("不可逆计数=%d", p.Summary.Irreversible)
	}
	if err := Validate(p); err != nil {
		t.Fatalf("生成的计划应通过校验：%v", err)
	}
}

func TestFromScanExplicitSelection(t *testing.T) {
	p, err := FromScan(sampleResult(), Options{DeletePaths: []string{`C:\r\a.txt`, `C:\r\d\dup.bin`}})
	if err != nil {
		t.Fatalf("FromScan: %v", err)
	}
	if len(p.Actions) != 2 {
		t.Fatalf("动作数 = %d，期望 2", len(p.Actions))
	}
	if p.Actions[0].Src != `C:\r\a.txt` || p.Actions[1].Src != `C:\r\d\dup.bin` {
		t.Fatalf("动作顺序不符合预期：%+v", p.Actions)
	}
	if p.Actions[0].Seq != 1 || p.Actions[1].Seq != 2 {
		t.Fatalf("序号不连续：%d, %d", p.Actions[0].Seq, p.Actions[1].Seq)
	}

	// 允许改留另一份：勾选保留项、留下其他副本。
	keepOnly, err := FromScan(sampleResult(), Options{DeletePaths: []string{`C:\r\b.txt`}})
	if err != nil {
		t.Fatalf("勾选保留项失败：%v", err)
	}
	if len(keepOnly.Actions) != 1 || keepOnly.Actions[0].Src != `C:\r\b.txt` {
		t.Fatalf("勾选保留项结果不符合预期：%+v", keepOnly.Actions)
	}

	// 空切片合法，表示一个都不删。
	none, err := FromScan(sampleResult(), Options{DeletePaths: []string{}})
	if err != nil {
		t.Fatalf("空勾选不应报错：%v", err)
	}
	if len(none.Actions) != 0 {
		t.Fatalf("空勾选动作数 = %d，期望 0", len(none.Actions))
	}
}

func TestFromScanExplicitSelectionRejects(t *testing.T) {
	if _, err := FromScan(sampleResult(), Options{DeletePaths: []string{
		`C:\r\a.txt`, `C:\r\b.txt`, `C:\r\c.txt`,
	}}); !errors.Is(err, ErrGroupFullySelected) {
		t.Fatalf("整组勾选应返回 ErrGroupFullySelected，实际 %v", err)
	}
	if _, err := FromScan(sampleResult(), Options{DeletePaths: []string{`C:\r\不存在.txt`}}); !errors.Is(err, ErrSelectionUnknown) {
		t.Fatalf("未知路径应返回 ErrSelectionUnknown，实际 %v", err)
	}
}

func TestFromScanPermanent(t *testing.T) {
	p, err := FromScan(sampleResult(), Options{DeleteMode: model.DeletePermanent})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range p.Actions {
		if a.Kind != model.ActionDeletePermanent || !a.Irreversible {
			t.Fatalf("永久删除动作不正确：%+v", a)
		}
	}
	if p.Summary.Permanent != 3 || p.Summary.Irreversible != 3 {
		t.Fatalf("summary=%+v", p.Summary)
	}
}

func TestFromScanEmpty(t *testing.T) {
	p, err := FromScan(&model.Result{SchemaVersion: model.SchemaVersion}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Actions == nil || len(p.Actions) != 0 {
		t.Fatalf("空结果应生成空动作切片（非 nil）：%#v", p.Actions)
	}
	if p.Summary.Actions != 0 {
		t.Fatalf("summary=%+v", p.Summary)
	}
}

func TestFromScanRejectsInvalidGroup(t *testing.T) {
	res := sampleResult()
	res.Groups[0].Keep = `C:\r\missing.txt`
	if _, err := FromScan(res, Options{}); !errors.Is(err, ErrGroupInvalid) {
		t.Fatalf("期望 ErrGroupInvalid，实际 %v", err)
	}
	if _, err := FromScan(nil, Options{}); !errors.Is(err, ErrNilResult) {
		t.Fatalf("期望 ErrNilResult，实际 %v", err)
	}
}

func TestValidateRejectsBadPlan(t *testing.T) {
	base := func() *model.Plan {
		p, err := FromScan(sampleResult(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	bad := base()
	bad.Actions[0].Seq = 9
	if err := Validate(bad); err == nil {
		t.Fatal("序号不连续应被拒绝")
	}

	bad = base()
	bad.Actions[0].Precheck = nil
	if err := Validate(bad); err == nil {
		t.Fatal("删除动作缺少 precheck 应被拒绝")
	}

	bad = base()
	bad.Actions[0].Status = model.StatusApplied
	if err := Validate(bad); err == nil {
		t.Fatal("非 planned 状态应被拒绝")
	}

	bad = base()
	bad.Actions[0].Kind = model.ActionMove
	bad.Actions[0].Dst = ""
	if err := Validate(bad); err == nil {
		t.Fatal("move 缺少 dst 应被拒绝")
	}
}
