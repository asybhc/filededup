package scan

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filededup/internal/engine/model"
)

// 快筛只能缩小候选集合，不能改变分组：同一目录下开/关快筛必须得到等价结果。
func TestQuickHashMatchesFullHash(t *testing.T) {
	root := t.TempDir()
	// 1) 小文件内容相同。
	writeFile(t, root, "small-a.txt", "same")
	writeFile(t, root, "small-b.txt", "same")
	// 2) 小文件同尺寸但内容不同。
	writeFile(t, root, "diff-a.txt", "abc")
	writeFile(t, root, "diff-b.txt", "abd")
	// 3) 超过 64KB、前 64KB 相同但尾部不同：快筛会误判为候选，全量复核必须能区分。
	head := strings.Repeat("h", quickHashBytes)
	writeFile(t, root, "big-a.bin", head+"tail-1")
	writeFile(t, root, "big-b.bin", head+"tail-2")
	// 4) 超过 64KB 且完整内容相同。
	writeFile(t, root, "big-same-a.bin", head+"shared")
	writeFile(t, root, "big-same-b.bin", head+"shared")
	// 5) 尺寸唯一，任何模式都不应成组。
	writeFile(t, root, "unique.bin", head+"only")
	// 6) 空文件。
	writeFile(t, root, "empty-a", "")
	writeFile(t, root, "empty-b", "")

	quickReq := testRequest(t, root)
	quickReq.QuickHash = true
	quickRes, err := Run(context.Background(), quickReq)
	if err != nil {
		t.Fatalf("quick run: %v", err)
	}
	fullRes, err := Run(context.Background(), testRequest(t, root))
	if err != nil {
		t.Fatalf("full run: %v", err)
	}

	if len(quickRes.Groups) != len(fullRes.Groups) {
		t.Fatalf("groups quick=%d full=%d", len(quickRes.Groups), len(fullRes.Groups))
	}
	for i := range fullRes.Groups {
		q, f := quickRes.Groups[i], fullRes.Groups[i]
		if q.Kind != f.Kind || q.Key != f.Key || q.Keep != f.Keep || q.Reclaimable != f.Reclaimable {
			t.Fatalf("group %d differs: quick=%+v full=%+v", i, q, f)
		}
		if len(q.Members) != len(f.Members) {
			t.Fatalf("group %d members quick=%d full=%d", i, len(q.Members), len(f.Members))
		}
		for j := range f.Members {
			if q.Members[j].Path != f.Members[j].Path || q.Members[j].Hash != f.Members[j].Hash {
				t.Fatalf("group %d member %d differs: quick=%+v full=%+v", i, j, q.Members[j], f.Members[j])
			}
		}
	}
	if quickRes.Stats.Groups != fullRes.Stats.Groups ||
		quickRes.Stats.DuplicateFiles != fullRes.Stats.DuplicateFiles ||
		quickRes.Stats.ReclaimableBytes != fullRes.Stats.ReclaimableBytes {
		t.Fatalf("stats differ: quick=%+v full=%+v", quickRes.Stats, fullRes.Stats)
	}
	// 前 64KB 相同但尾部不同的两个大文件必须保持独立，不能因快筛被合并。
	for _, g := range quickRes.Groups {
		for _, m := range g.Members {
			if strings.HasPrefix(m.Path, filepath.Join(root, "big-a")) ||
				strings.HasPrefix(m.Path, filepath.Join(root, "big-b")) {
				t.Fatalf("quick hash 误合并尾部不同的文件：%s", m.Path)
			}
		}
	}
}

// 快筛应该减少进入全量哈希的文件数：同尺寸但前 64KB 均不同时，一个文件都不必全量哈希。
func TestQuickHashSkipsNonCandidates(t *testing.T) {
	root := t.TempDir()
	// 四个文件尺寸一致、前 64KB 各不相同，因此没有一个能进入全量复核。
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("prefix-%d", i) + strings.Repeat("x", 1024)
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%d.bin", i)), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	quickReq := testRequest(t, root)
	quickReq.QuickHash = true
	quickRes, err := Run(context.Background(), quickReq)
	if err != nil {
		t.Fatalf("quick run: %v", err)
	}
	if quickRes.Stats.FilesHashed != 0 {
		t.Fatalf("filesHashed=%d，快筛后不该有文件进入全量哈希", quickRes.Stats.FilesHashed)
	}
	if len(quickRes.Groups) != 0 {
		t.Fatalf("groups=%d", len(quickRes.Groups))
	}

	fullRes, err := Run(context.Background(), testRequest(t, root))
	if err != nil {
		t.Fatalf("full run: %v", err)
	}
	if fullRes.Stats.FilesHashed != 4 {
		t.Fatalf("full filesHashed=%d want 4", fullRes.Stats.FilesHashed)
	}
	if len(fullRes.Groups) != 0 {
		t.Fatalf("full groups=%d", len(fullRes.Groups))
	}
}

// 快筛读取失败的文件要登记为 Skipped，且不参与分组。
func TestQuickHashReadFailure(t *testing.T) {
	root := t.TempDir()
	bad := writeFile(t, root, "bad.bin", "1234")
	writeFile(t, root, "good.bin", "1234")
	quickReq := testRequest(t, root)
	quickReq.QuickHash = true

	d := testDeps()
	d.open = func(path string) (io.ReadCloser, error) {
		if path == bad {
			return nil, os.ErrPermission
		}
		return os.Open(path)
	}
	res, err := run(context.Background(), quickReq, d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != bad {
		t.Fatalf("skipped=%+v", res.Skipped)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("groups=%d，读失败的文件不应成组（good 只剩一个成员）", len(res.Groups))
	}
	if res.Stats.FilesHashed != 0 {
		t.Fatalf("filesHashed=%d want 0", res.Stats.FilesHashed)
	}
}

// 快筛模式下结果仍必须是可继续执行的精确组，成员快照与全量哈希一致。
func TestQuickHashFillsSnapshots(t *testing.T) {
	root := t.TempDir()
	a := writeFile(t, root, "a.bin", strings.Repeat("z", 4096))
	writeFile(t, root, "b.bin", strings.Repeat("z", 4096))
	quickReq := testRequest(t, root)
	quickReq.QuickHash = true
	res, err := Run(context.Background(), quickReq)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups=%d", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Kind != model.GroupExact || g.Keep != a {
		t.Fatalf("unexpected group: %+v", g)
	}
	for _, m := range g.Members {
		if m.Hash == "" || m.Snapshot == nil {
			t.Fatalf("member missing hash/snapshot: %+v", m)
		}
	}
}
