package scan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"filededup/internal/engine/model"
)

func testRequest(t *testing.T, root string) model.Request {
	t.Helper()
	req := model.DefaultRequest()
	req.Roots = []string{root}
	req.Exact = true
	return req
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := dir + string(os.PathSeparator) + name
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func testDeps() deps {
	return deps{
		hidden: func(string, fs.FileInfo) bool { return false },
		stat: func(path string, de fs.DirEntry) (fs.FileInfo, error) {
			if de != nil {
				return de.Info()
			}
			return os.Stat(path)
		},
		open: func(path string) (io.ReadCloser, error) { return os.Open(path) },
	}
}

// 空结果必须序列化为 []，不能是 null，否则下游按数组解析会失败。
func TestEmptyCollectionsMarshalAsArrays(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only.txt", "unique")

	res, err := Run(context.Background(), testRequest(t, root))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, `"groups":null`) || strings.Contains(s, `"skipped":null`) {
		t.Fatalf("empty collections serialized as null: %s", s)
	}
	if !strings.Contains(s, `"groups":[]`) || !strings.Contains(s, `"skipped":[]`) {
		t.Fatalf("empty collections not serialized as []: %s", s)
	}
}

func TestScanBasicDuplicates(t *testing.T) {
	root := t.TempDir()
	a := writeFile(t, root, "a.txt", "same")
	writeFile(t, root, "b.txt", "same")
	writeFile(t, root, "c.txt", "diff")
	writeFile(t, root, "d.txt", "longer")

	res, err := Run(context.Background(), testRequest(t, root))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Stats.FilesEnumerated != 4 {
		t.Fatalf("filesEnumerated=%d", res.Stats.FilesEnumerated)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("groups=%d", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Kind != model.GroupExact || len(g.Members) != 2 {
		t.Fatalf("unexpected group: %+v", g)
	}
	if g.Keep != a {
		t.Fatalf("keep=%s want %s", g.Keep, a)
	}
	if g.Reclaimable != 4 {
		t.Fatalf("reclaimable=%d", g.Reclaimable)
	}
	if res.Stats.DuplicateFiles != 1 || res.Stats.ReclaimableBytes != 4 {
		t.Fatalf("stats=%+v", res.Stats)
	}
	for _, m := range g.Members {
		if m.Hash == "" || m.Snapshot == nil {
			t.Fatalf("member missing hash/snapshot: %+v", m)
		}
	}
}

func TestSkippedSizeFailed(t *testing.T) {
	root := t.TempDir()
	bad := writeFile(t, root, "bad.bin", "1234")
	d := testDeps()
	d.stat = func(path string, de fs.DirEntry) (fs.FileInfo, error) {
		if path == bad {
			return nil, errors.New("cannot read size")
		}
		return de.Info()
	}
	res, err := run(context.Background(), testRequest(t, root), d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stats.FilesEnumerated != 1 {
		t.Fatalf("filesEnumerated=%d", res.Stats.FilesEnumerated)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Code != model.SkipSizeFailed {
		t.Fatalf("skipped=%+v", res.Skipped)
	}
}

func TestSkippedPermissionAndPathTooLong(t *testing.T) {
	root := t.TempDir()
	denied := writeFile(t, root, "denied.bin", "xx")
	long := writeFile(t, root, "long.bin", "xx")
	d := testDeps()
	d.stat = func(path string, de fs.DirEntry) (fs.FileInfo, error) {
		switch path {
		case denied:
			return nil, fs.ErrPermission
		case long:
			return nil, errors.New("filename or extension is too long")
		}
		return de.Info()
	}
	res, err := run(context.Background(), testRequest(t, root), d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	codes := map[model.SkipCode]int{}
	for _, s := range res.Skipped {
		codes[s.Code]++
	}
	if codes[model.SkipPermissionDenied] != 1 || codes[model.SkipPathTooLong] != 1 {
		t.Fatalf("codes=%v", codes)
	}
}

func TestSkippedHashFailed(t *testing.T) {
	root := t.TempDir()
	a := writeFile(t, root, "a.bin", "1234")
	writeFile(t, root, "b.bin", "1234")
	d := testDeps()
	d.open = func(path string) (io.ReadCloser, error) {
		if path == a {
			return nil, errors.New("boom")
		}
		return os.Open(path)
	}
	res, err := run(context.Background(), testRequest(t, root), d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("groups=%d", len(res.Groups))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != a || res.Skipped[0].Code != model.SkipReadFailed {
		t.Fatalf("skipped=%+v", res.Skipped)
	}
	if res.Stats.FilesHashed != 1 {
		t.Fatalf("filesHashed=%d", res.Stats.FilesHashed)
	}
}

func TestHiddenFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "visible.txt", "same")
	writeFile(t, root, "secret.txt", "same")
	req := testRequest(t, root)
	d := testDeps()
	d.hidden = func(p string, _ fs.FileInfo) bool { return strings.HasSuffix(p, "secret.txt") }
	res, err := run(context.Background(), req, d)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stats.FilesEnumerated != 1 {
		t.Fatalf("filesEnumerated=%d", res.Stats.FilesEnumerated)
	}
}

func TestMinSize(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "small.txt", "ab")
	writeFile(t, root, "big.txt", "abcdef")
	req := testRequest(t, root)
	req.MinSize = 3
	res, err := Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Stats.FilesEnumerated != 1 {
		t.Fatalf("filesEnumerated=%d", res.Stats.FilesEnumerated)
	}
}

func TestCanceledContext(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.bin", strings.Repeat("x", 4096))
	writeFile(t, root, "b.bin", strings.Repeat("x", 4096))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, err := Run(ctx, testRequest(t, root))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cancel took %v", time.Since(start))
	}
}

type slowReader struct {
	io.ReadCloser
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(s.delay)
	return s.ReadCloser.Read(p)
}

// 扫描进行中取消：100ms 后 cancel，必须在 2s 内返回 context.Canceled 且带部分结果。
func TestCancelDuringScan(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 8; i++ {
		writeFile(t, root, fmt.Sprintf("f%d.bin", i), strings.Repeat("x", 4<<20))
	}
	d := testDeps()
	d.open = func(path string) (io.ReadCloser, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return &slowReader{ReadCloser: f, delay: 30 * time.Millisecond}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res, err := run(ctx, testRequest(t, root), d)
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancel took %v", elapsed)
	}
	if res == nil {
		t.Fatal("取消时应返回部分结果")
	}
}
