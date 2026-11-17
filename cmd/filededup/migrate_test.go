//go:build desktop

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/brand"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败：%v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入文件失败：%v", err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取文件失败：%v", err)
	}
	return string(b)
}

func TestMigrateCopiesOldDirsAndKeepsOriginals(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "文件整理.一杯好茶")
	oldData := filepath.Join(base, "FileDedup")
	oldTool := filepath.Join(base, "FileDedupTool")

	writeTestFile(t, filepath.Join(oldData, "ui-settings.json"), `{"schemaVersion":1}`)
	writeTestFile(t, filepath.Join(oldData, "lanzou", "lanzou.bin"), "encrypted")
	writeTestFile(t, filepath.Join(oldTool, "trial.dat"), "trial")

	results := migrateDataDirs(base, newDir)
	if len(results) != 2 {
		t.Fatalf("应记录两个来源，实际 %d", len(results))
	}
	for _, r := range results {
		if len(r.Failed) != 0 {
			t.Fatalf("迁移失败项：%v", r.Failed)
		}
	}
	if total := results[0].Files + results[1].Files; total != 3 {
		t.Fatalf("应复制 3 个文件，实际 %d", total)
	}

	// 新旧内容一致，旧目录保留。
	for _, rel := range []string{"ui-settings.json", "lanzou/lanzou.bin"} {
		if got, want := readTestFile(t, filepath.Join(newDir, filepath.FromSlash(rel))), readTestFile(t, filepath.Join(oldData, filepath.FromSlash(rel))); got != want {
			t.Fatalf("%s 复制内容不一致", rel)
		}
	}
	if got := readTestFile(t, filepath.Join(newDir, "trial.dat")); got != "trial" {
		t.Fatalf("trial.dat 未复制：%q", got)
	}
	if _, err := os.Stat(filepath.Join(oldData, "ui-settings.json")); err != nil {
		t.Fatalf("旧 FileDedup 目录被移动或删除：%v", err)
	}
	if _, err := os.Stat(filepath.Join(oldTool, "trial.dat")); err != nil {
		t.Fatalf("旧 FileDedupTool 目录被移动或删除：%v", err)
	}

	// 迁移日志写入新目录；测试中直接调用 copyLegacyDir，生产 wrapper 才写日志，
	// 这里补一次与生产一致的写日志调用并核对字段。
	writeMigrationLog(newDir, results)
	logText := readTestFile(t, filepath.Join(newDir, "logs", "migration.log"))
	for _, want := range []string{oldData, oldTool, "复制文件", "失败 0"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("迁移日志缺少 %q：%s", want, logText)
		}
	}
}

func TestMigrateSkipsWhenNewDirExists(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "文件整理.一杯好茶")
	oldData := filepath.Join(base, "FileDedup")

	writeTestFile(t, filepath.Join(oldData, "ui-settings.json"), "old")
	writeTestFile(t, filepath.Join(newDir, "ui-settings.json"), "new")

	results := migrateDataDirs(base, newDir)
	if len(results) != 0 {
		t.Fatalf("新目录已存在时不应迁移，实际返回 %d 个来源", len(results))
	}
	if got := readTestFile(t, filepath.Join(newDir, "ui-settings.json")); got != "new" {
		t.Fatalf("新目录被覆盖：%q", got)
	}
	// 旧目录继续保留。
	if got := readTestFile(t, filepath.Join(oldData, "ui-settings.json")); got != "old" {
		t.Fatalf("旧目录内容变化：%q", got)
	}
}

func TestMigrateRecordsConflictAsSkipped(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "文件整理.一杯好茶")
	oldData := filepath.Join(base, "FileDedup")
	oldTool := filepath.Join(base, "FileDedupTool")

	writeTestFile(t, filepath.Join(oldData, "shared.txt"), "from-data")
	writeTestFile(t, filepath.Join(oldTool, "shared.txt"), "from-tool")

	results := migrateDataDirs(base, newDir)
	if len(results) != 2 {
		t.Fatalf("应记录两个来源，实际 %d", len(results))
	}
	if results[1].Skipped != 1 {
		t.Fatalf("第二个来源应跳过 1 个冲突文件，实际 %d", results[1].Skipped)
	}
	if got := readTestFile(t, filepath.Join(newDir, "shared.txt")); got != "from-data" {
		t.Fatalf("冲突文件应保留先复制的内容，实际 %q", got)
	}
}

// TestMigrateStartupEntrypoint 直接调用生产入口 migrateLegacyData：
// 把 AppData 指到临时目录，构造两个旧目录后启动，核对复制结果、旧目录保留与迁移日志。
func TestMigrateStartupEntrypoint(t *testing.T) {
	base := t.TempDir()
	t.Setenv("AppData", base)

	oldData := filepath.Join(base, brand.LegacyDataDirName)
	oldTool := filepath.Join(base, brand.LegacyToolDirName)
	writeTestFile(t, filepath.Join(oldData, "ui-settings.json"), `{"schemaVersion":1}`)
	writeTestFile(t, filepath.Join(oldData, "logs", "ops", "ops_old.jsonl"), "ops")
	writeTestFile(t, filepath.Join(oldTool, "trial.dat"), "trial")

	migrateLegacyData()

	newDir := filepath.Join(base, brand.DataDirName)
	for rel, want := range map[string]string{
		"ui-settings.json":           `{"schemaVersion":1}`,
		filepath.Join("logs", "ops", "ops_old.jsonl"): "ops",
		"trial.dat": "trial",
	} {
		if got := readTestFile(t, filepath.Join(newDir, rel)); got != want {
			t.Fatalf("%s 复制内容不一致：%q", rel, got)
		}
	}
	if _, err := os.Stat(filepath.Join(oldData, "ui-settings.json")); err != nil {
		t.Fatalf("旧 FileDedup 目录被移动：%v", err)
	}
	if _, err := os.Stat(filepath.Join(oldTool, "trial.dat")); err != nil {
		t.Fatalf("旧 FileDedupTool 目录被移动：%v", err)
	}

	logText := readTestFile(t, filepath.Join(newDir, "logs", "migration.log"))
	for _, want := range []string{oldData, oldTool, "复制文件", "失败 0"} {
		if !strings.Contains(logText, want) {
			t.Fatalf("迁移日志缺少 %q：%s", want, logText)
		}
	}

	// 第二次启动：新目录已存在，必须完全不覆盖、也不追加日志。
	migrateLegacyData()
	if got := readTestFile(t, filepath.Join(newDir, "logs", "migration.log")); got != logText {
		t.Fatalf("新目录已存在时不应再次迁移：%q", got)
	}
}
