package model_test

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"filededup/internal/engine/model"
)

//go:embed schema/v1/filededup.schema.json
var schemaJSON []byte

func compileSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		t.Fatalf("解析 schema 失败: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("filededup.schema.json", doc); err != nil {
		t.Fatalf("注册 schema 失败: %v", err)
	}
	sch, err := c.Compile("filededup.schema.json")
	if err != nil {
		t.Fatalf("编译 schema 失败: %v", err)
	}
	return sch
}

func mustValidate(t *testing.T, sch *jsonschema.Schema, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("反序列化实例失败: %v", err)
	}
	if err := sch.Validate(inst); err != nil {
		t.Fatalf("Schema 校验失败: %v\nJSON: %s", err, raw)
	}
}

func sampleScanResult() model.Result {
	req := model.DefaultRequest()
	req.Roots = []string{`C:\corpus`}
	req.RequestID = "req-1"
	const hash = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	return model.Result{
		SchemaVersion: model.SchemaVersion,
		RequestID:     "req-1",
		Request:       req,
		Stats: model.Stats{
			FilesEnumerated: 2, BytesEnumerated: 20, FilesHashed: 2, BytesHashed: 20,
			Groups: 1, ExactGroups: 1, DuplicateFiles: 1, ReclaimableBytes: 10, Skipped: 0,
		},
		Groups: []model.Group{{
			ID:   "exact:" + hash,
			Kind: model.GroupExact,
			Key:  hash,
			Members: []model.Member{
				{Path: `C:\corpus\a.txt`, Size: 10, ModTime: "2026-01-02T03:04:05.000000123Z", Hash: hash, Keep: true, Deletable: false},
				{Path: `C:\corpus\b.txt`, Size: 10, ModTime: "2026-01-02T03:04:06.000000123Z", Hash: hash, Keep: false, Deletable: true},
			},
			Keep:        `C:\corpus\a.txt`,
			Reclaimable: 10,
		}},
		Skipped: []model.Skip{},
		Runtime: &model.Runtime{
			StartedAt:  "2026-01-02T03:04:05Z",
			FinishedAt: "2026-01-02T03:04:06Z",
			ElapsedMS:  1000,
			Workers:    4,
			GoVersion:  "go1.27.0",
		},
	}
}

func sampleOrganizeResult() model.OrganizeResult {
	req := model.DefaultOrganizeRequest(`C:\corpus`, `C:\corpus\out`)
	req.RequestID = "org-1"
	steps := make([]model.StepResult, 0, len(model.StandaloneOrder))
	for _, id := range model.StandaloneOrder {
		steps = append(steps, model.StepResult{ID: id, Title: string(id), Enabled: true, Executed: true})
	}
	return model.OrganizeResult{
		SchemaVersion: model.SchemaVersion,
		RequestID:     "org-1",
		Request:       req,
		DryRun:        false,
		Order:         model.StandaloneOrder,
		Steps:         steps,
		Counters:      model.OrganizeCounters{RenamedPrefix: 1, Moved: 2, EmptyDirsRemoved: 3},
		Actions: []model.Action{
			{Seq: 1, Step: string(model.StepRenamePrefix), Kind: model.ActionRename, Src: `C:\corpus\1 a.txt`, Dst: `C:\corpus\a.txt`, Rollback: model.RollbackRenameBack, Status: model.StatusApplied},
			{Seq: 2, Step: string(model.StepOrganize), Kind: model.ActionMove, Src: `C:\corpus\a.txt`, Dst: `C:\corpus\out\a.txt`, Rollback: model.RollbackRenameBack, Status: model.StatusApplied},
			{Seq: 3, Step: string(model.StepEmptyDirs), Kind: model.ActionRemoveDir, Src: `C:\corpus\empty`, Rollback: model.RollbackNone, Status: model.StatusApplied},
		},
		Skipped: []model.Skip{},
	}
}

func samplePlan() model.Plan {
	p := model.Plan{
		SchemaVersion: model.SchemaVersion,
		RequestID:     "req-1",
		CreatedAt:     time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339Nano),
		Source:        model.PlanFromScan,
		Actions: []model.Action{
			{Seq: 1, Kind: model.ActionDeleteTrash, Src: `C:\corpus\b.txt`, Reason: "重复文件，保留 a.txt", Irreversible: false, Rollback: model.RollbackNone, Status: model.StatusPlanned,
				Precheck: &model.Precheck{Size: 10, ModTime: "2026-01-02T03:04:06.000000123Z", Hash: strings.Repeat("a", 64)}},
			{Seq: 2, Kind: model.ActionHardlink, Src: `C:\corpus\c.txt`, Dst: `C:\corpus\a.txt`, Irreversible: false, Rollback: model.RollbackDropLink, Status: model.StatusPlanned},
			{Seq: 3, Kind: model.ActionDeletePermanent, Src: `C:\corpus\d.txt`, Irreversible: true, Rollback: model.RollbackNone, Status: model.StatusPlanned},
		},
	}
	p.Summarize()
	return p
}

func TestSchemaAcceptsContractSamples(t *testing.T) {
	sch := compileSchema(t)
	mustValidate(t, sch, sampleScanResult())
	mustValidate(t, sch, sampleOrganizeResult())
	mustValidate(t, sch, samplePlan())
}

func TestSchemaRejectsUnknownField(t *testing.T) {
	sch := compileSchema(t)
	raw, err := json.Marshal(sampleScanResult())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["unknownField"] = true
	if err := validateAny(sch, m); err == nil {
		t.Fatal("包含未知字段时应校验失败")
	}
}

func TestSchemaRejectsNilSlices(t *testing.T) {
	sch := compileSchema(t)
	res := sampleScanResult()
	res.Groups = nil
	res.Skipped = nil
	if err := validateAny(sch, mustJSON(t, res)); err == nil {
		t.Fatal("groups/skipped 为 null 时应校验失败：引擎必须输出 [] 而不是 null")
	}
}

func TestSchemaRejectsBadEnum(t *testing.T) {
	sch := compileSchema(t)
	res := sampleScanResult()
	res.Request.KeepPolicy = "random"
	if err := validateAny(sch, mustJSON(t, res)); err == nil {
		t.Fatal("非法 keepPolicy 应校验失败")
	}
}

func validateAny(sch *jsonschema.Schema, m any) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return sch.Validate(inst)
}

func mustJSON(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
