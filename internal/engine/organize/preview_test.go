package organize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func previewTestSnapshot(files ...FileEntry) Snapshot {
	return Snapshot{Root: `C:\语料`, Files: files, Dirs: []string{}}
}

func previewTestFile(path, sha string, size int64) FileEntry {
	return FileEntry{Path: path, Size: size, SHA256: sha, ModTime: time.Unix(0, 0)}
}

func TestBuildPreviewPlanContentGroup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Steps.ContentDedup = true
	cfg.Steps.ImageDedup = false
	cfg.Steps.AudioDedup = false
	snap := previewTestSnapshot(
		previewTestFile("deep/dir/a.txt", "h1", 10),
		previewTestFile("b.txt", "h1", 10),
		previewTestFile("c.txt", "h2", 10),
	)
	plan := BuildPreviewPlan(cfg, snap, Providers{})
	if plan.SchemaVersion != PreviewPlanSchemaVersion {
		t.Fatalf("schemaVersion=%d", plan.SchemaVersion)
	}
	if len(plan.Entries) != 2 {
		t.Fatalf("条目数=%d，期望 2（只包含同内容的两份）：%+v", len(plan.Entries), plan.Entries)
	}
	winner := "b.txt" // 路径最短者保留
	for _, e := range plan.Entries {
		if e.Keep != winner {
			t.Fatalf("%s 的保留项=%q，期望 %q", e.From, e.Keep, winner)
		}
	}
	m := plan.Map()
	if got, ok := m[Fold("deep/dir/a.txt")]; !ok || got != Fold(winner) {
		t.Fatalf("Map 未把重复项映射到保留项：%v", m)
	}
}

func TestPreviewPlanRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	want := PreviewPlan{
		SchemaVersion: PreviewPlanSchemaVersion,
		Root:          `C:\语料`,
		Entries: []PreviewEntry{
			{From: "A/B.TXT", Keep: "A/C.TXT"},
			{From: "x.txt", Keep: "y.txt"},
		},
	}
	if err := SavePreviewPlan(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPreviewPlan(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != len(want.Entries) || got.Entries[0] != want.Entries[0] || got.Entries[1] != want.Entries[1] {
		t.Fatalf("往返后条目变化：%+v", got.Entries)
	}
	m := got.Map()
	if m[Fold("A/B.TXT")] != Fold("A/C.TXT") {
		t.Fatalf("Map 未折叠大小写：%v", m)
	}
}

func TestLoadPreviewPlanRejectsUnsafePaths(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"绝对路径":  `{"schemaVersion":1,"entries":[{"from":"C:\\\\a.txt","keep":"b.txt"}]}`,
		"越界路径":  `{"schemaVersion":1,"entries":[{"from":"../a.txt","keep":"b.txt"}]}`,
		"空字段":   `{"schemaVersion":1,"entries":[{"from":"","keep":"b.txt"}]}`,
		"版本不匹配": `{"schemaVersion":99,"entries":[]}`,
	}
	for name, raw := range cases {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".json")
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadPreviewPlan(path); err == nil {
			t.Fatalf("%s：期望报错，实际通过", name)
		}
	}
}

func TestPlanUsesPreviewPlanForDedup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Order = OrderPreview
	cfg.Steps = StepSwitches{ContentDedup: true}
	cfg.KeepRule = KeepShortestPath
	snap := previewTestSnapshot(
		previewTestFile("a.txt", "h1", 10),
		previewTestFile("deep/b.txt", "h1", 10),
	)
	// 人工把保留项改成深路径的一份，规划器必须服从计划而不是保留规则。
	snap.PreviewPlan = map[string]string{
		Fold("a.txt"):      Fold("deep/b.txt"),
		Fold("deep/b.txt"): Fold("deep/b.txt"),
	}
	res := Plan(cfg, snap, Providers{})
	var deletes []Action
	for _, step := range res.Steps {
		for _, a := range step.Actions {
			if a.Kind == ActionDelete {
				deletes = append(deletes, a)
			}
		}
	}
	if len(deletes) != 1 || deletes[0].From != "a.txt" {
		t.Fatalf("期望只删除 a.txt，实际 %+v", deletes)
	}
}

func TestBuildPreviewPlanRespectsScanFilters(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Steps.ContentDedup = true
	cfg.SkipHiddenSystem = true
	cfg.Excludes = []string{"skipme"}
	snap := previewTestSnapshot(
		previewTestFile("a.txt", "h1", 10),
		FileEntry{Path: "hidden.txt", Size: 10, SHA256: "h1", Hidden: true},
		previewTestFile("skipme/b.txt", "h1", 10),
	)
	plan := BuildPreviewPlan(cfg, snap, Providers{})
	if len(plan.Entries) != 0 {
		t.Fatalf("过滤后不应产生分组：%+v", plan.Entries)
	}
}
