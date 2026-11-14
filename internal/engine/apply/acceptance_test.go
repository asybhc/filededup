package apply_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/engine/apply"
	"filededup/internal/engine/model"
	"filededup/internal/engine/plan"
	"filededup/internal/engine/scan"
)

const acceptanceGroups = 500

// buildAcceptanceCorpus 生成 500 组重复文件，每组 2~3 个成员，内容按组确定性生成。
func buildAcceptanceCorpus(t *testing.T, root string) (files, deletable int) {
	t.Helper()
	for i := 0; i < acceptanceGroups; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", i/50))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		size := 512 + (i%7)*64
		data := genData(i, size)
		members := 2 + i%2
		for k := 0; k < members; k++ {
			name := fmt.Sprintf("g%03d_m%d.bin", i, k)
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			files++
			if k > 0 {
				deletable++
			}
		}
	}
	return files, deletable
}

func genData(group, size int) []byte {
	data := make([]byte, size)
	x := uint32(group)*2654435761 + 1
	for i := range data {
		x = x*1664525 + 1013904223
		data[i] = byte(x >> 24)
	}
	return data
}

func remainingPerGroup(t *testing.T, root string) map[int]int {
	t.Helper()
	counts := make(map[int]int, acceptanceGroups)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		var idx int
		if _, scanErr := fmt.Sscanf(base, "g%03d_m", &idx); scanErr != nil {
			return fmt.Errorf("无法解析文件名 %q：%w", base, scanErr)
		}
		counts[idx]++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

// TestAcceptance500GroupsEndState 是 M2 的终态验收：
// 生成 500 组 → 扫描 → 生成计划 → 演练（不改动）→ 执行 → 逐项核对文件系统终态 → 复扫为 0 组。
func TestAcceptance500GroupsEndState(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 500 组终态验收（-short）")
	}
	ctx := context.Background()
	root := t.TempDir()
	wantFiles, wantDeleted := buildAcceptanceCorpus(t, root)

	req := model.DefaultRequest()
	req.Roots = []string{root}
	req.Exact = true
	res, err := scan.Run(ctx, req)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if res.Stats.Groups != acceptanceGroups {
		t.Fatalf("重复组数=%d，期望 %d", res.Stats.Groups, acceptanceGroups)
	}
	if res.Stats.FilesEnumerated != int64(wantFiles) {
		t.Fatalf("文件数=%d，期望 %d", res.Stats.FilesEnumerated, wantFiles)
	}

	p, err := plan.FromScan(res, plan.Options{DeleteMode: model.DeletePermanent})
	if err != nil {
		t.Fatalf("生成计划失败：%v", err)
	}
	if err := plan.Validate(p); err != nil {
		t.Fatalf("计划校验失败：%v", err)
	}
	if len(p.Actions) != wantDeleted {
		t.Fatalf("动作数=%d，期望 %d", len(p.Actions), wantDeleted)
	}

	// 1. 演练：文件系统必须完全不变。
	dry, err := (&apply.Executor{}).Execute(ctx, p, apply.Options{DryRun: true})
	if err != nil {
		t.Fatalf("演练失败：%v", err)
	}
	if dry.DryRuns != wantDeleted || dry.Failed != 0 {
		t.Fatalf("演练报告不符：dry=%d failed=%d", dry.DryRuns, dry.Failed)
	}
	if got := totalFiles(t, root); got != wantFiles {
		t.Fatalf("演练后文件数=%d，期望 %d", got, wantFiles)
	}

	// 2. 真实执行（永久删除，避免污染 CI 回收站）。
	report, err := (&apply.Executor{}).Execute(ctx, p, apply.Options{})
	if err != nil {
		t.Fatalf("执行失败：%v", err)
	}
	if report.Applied != wantDeleted || report.Failed != 0 || report.Skipped != 0 {
		t.Fatalf("执行报告不符：applied=%d failed=%d skipped=%d", report.Applied, report.Failed, report.Skipped)
	}

	// 3. 逐项核对终态：每组恰好保留 1 个文件，且保留项就是计划里的 keep。
	counts := remainingPerGroup(t, root)
	for i := 0; i < acceptanceGroups; i++ {
		if counts[i] != 1 {
			t.Fatalf("第 %d 组剩余文件 %d 个，期望 1 个", i, counts[i])
		}
	}
	if got := totalFiles(t, root); got != acceptanceGroups {
		t.Fatalf("执行后文件数=%d，期望 %d", got, acceptanceGroups)
	}
	for _, a := range report.Actions {
		if _, err := os.Stat(a.Src); !os.IsNotExist(err) {
			t.Fatalf("已删除路径仍存在：%s（err=%v）", a.Src, err)
		}
	}

	// 4. 复扫：应无重复组。
	res2, err := scan.Run(ctx, req)
	if err != nil {
		t.Fatalf("复扫失败：%v", err)
	}
	if res2.Stats.Groups != 0 {
		t.Fatalf("复扫仍有 %d 个重复组", res2.Stats.Groups)
	}
	if res2.Stats.FilesEnumerated != int64(acceptanceGroups) {
		t.Fatalf("复扫文件数=%d，期望 %d", res2.Stats.FilesEnumerated, acceptanceGroups)
	}
	if p.Summary.ReclaimableBytes <= 0 {
		t.Fatalf("计划可释放字节数应大于 0：%d", p.Summary.ReclaimableBytes)
	}
}

func totalFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !strings.HasSuffix(path, ".fdedup-bak") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
