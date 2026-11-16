//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOrganizePresetsRoundTrip 覆盖保存 / 覆盖 / 列表 / 删除与根目录剥离。
func TestOrganizePresetsRoundTrip(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	var app App
	rules := DefaultSettings().Organize
	rules.Root = `C:\sample-root`

	saved, err := app.SaveOrganizePreset("  照片方案  ", rules)
	if err != nil {
		t.Fatalf("保存预设失败: %v", err)
	}
	if saved.ID == "" || saved.Name != "照片方案" {
		t.Fatalf("预设条目异常: %+v", saved)
	}
	if saved.Rules.Root != "" {
		t.Fatalf("预设不应保存根目录: %q", saved.Rules.Root)
	}
	if saved.CreatedAt == "" || saved.UpdatedAt == "" {
		t.Fatalf("预设时间戳缺失: %+v", saved)
	}

	list, err := app.ListOrganizePresets()
	if err != nil {
		t.Fatalf("列出预设失败: %v", err)
	}
	if list.Path == "" || len(list.Presets) != 1 || list.Presets[0].ID != saved.ID {
		t.Fatalf("预设列表异常: %+v", list)
	}

	// 同名（大小写不同）覆盖：保留 ID 与创建时间，规则更新。
	updated := rules
	updated.Mode = "extension"
	again, err := app.SaveOrganizePreset("照片方案", updated)
	if err != nil {
		t.Fatalf("覆盖预设失败: %v", err)
	}
	if again.ID != saved.ID || again.CreatedAt != saved.CreatedAt {
		t.Fatalf("覆盖应保留 ID/创建时间: %+v vs %+v", again, saved)
	}
	if again.Rules.Mode != "extension" {
		t.Fatalf("覆盖后规则未更新: %q", again.Rules.Mode)
	}
	list, err = app.ListOrganizePresets()
	if err != nil || len(list.Presets) != 1 {
		t.Fatalf("覆盖后列表异常: err=%v list=%+v", err, list)
	}

	if err := app.DeleteOrganizePreset(saved.ID); err != nil {
		t.Fatalf("删除预设失败: %v", err)
	}
	list, err = app.ListOrganizePresets()
	if err != nil || len(list.Presets) != 0 {
		t.Fatalf("删除后列表异常: err=%v list=%+v", err, list)
	}
	if err := app.DeleteOrganizePreset("missing"); err == nil {
		t.Fatal("删除不存在的预设应报错")
	}
}

// TestOrganizePresetValidation 覆盖名称与规则校验。
func TestOrganizePresetValidation(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	var app App
	rules := DefaultSettings().Organize

	if _, err := app.SaveOrganizePreset("   ", rules); err == nil {
		t.Fatal("空名称应报错")
	}
	if _, err := app.SaveOrganizePreset("坏\n名称", rules); err == nil {
		t.Fatal("含换行名称应报错")
	}
	bad := rules
	bad.Mode = "unknown"
	if _, err := app.SaveOrganizePreset("坏规则", bad); err == nil {
		t.Fatal("非法整理方式应报错")
	}
}

// TestOrganizePresetsCorruptFile 覆盖损坏文件：列表与保存都报错，不静默覆盖。
func TestOrganizePresetsCorruptFile(t *testing.T) {
	t.Setenv("AppData", t.TempDir())
	var app App
	path, err := organizePresetsPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"kind":"filededup.organize.presets","presets":[`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ListOrganizePresets(); err == nil {
		t.Fatal("损坏的预设文件应返回错误")
	}
	if _, err := app.SaveOrganizePreset("方案", DefaultSettings().Organize); err == nil {
		t.Fatal("损坏的预设文件下保存应报错而不是覆盖")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"schemaVersion":1,"kind":"filededup.organize.presets","presets":[` {
		t.Fatalf("损坏文件不应被改写: %q", string(raw))
	}
}
