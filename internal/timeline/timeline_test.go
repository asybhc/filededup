package timeline

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBucketDir(t *testing.T) {
	when := time.Date(2024, 3, 8, 12, 30, 0, 0, time.Local)
	cases := map[Bucket]string{
		BucketDay:   "2024-03-08",
		BucketWeek:  "2024-W10",
		BucketMonth: "2024-03",
		BucketYear:  "2024",
	}
	for b, want := range cases {
		if got := BucketDir(when, b); got != want {
			t.Fatalf("%s: got %q want %q", b, got, want)
		}
	}
}

func TestPlanUsesEXIFDate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "out")
	photo := filepath.Join(root, "photo.jpg")
	if err := os.WriteFile(photo, exifJPEG("2023:05:06 07:08:09"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 把 txt 的修改时间固定到 2021 年，验证非图片走 mtime。
	old := time.Date(2021, 7, 1, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(filepath.Join(root, "note.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanTree(Options{
		Root: root, Target: target, Bucket: BucketYear, Action: ActionCopy, Conflict: ConflictRename,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(plan.Items))
	}
	for _, it := range plan.Items {
		switch filepath.Base(it.Source) {
		case "photo.jpg":
			if it.Bucket != "2023" || it.WhenFrom != "exif" {
				t.Fatalf("photo bucket=%s from=%s, want 2023/exif", it.Bucket, it.WhenFrom)
			}
		case "note.txt":
			if it.Bucket != "2021" || it.WhenFrom != "mtime" {
				t.Fatalf("note bucket=%s from=%s, want 2021/mtime", it.Bucket, it.WhenFrom)
			}
		}
	}
}

func TestConflictRenameAndSkip(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	when := time.Date(2022, 1, 2, 3, 4, 5, 0, time.Local)
	old := when
	// 目标目录里预置同名文件：年桶 2022。
	if err := os.MkdirAll(filepath.Join(target, "2022"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "2022", "a.txt"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("src"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "a.txt"), old, old); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanTree(Options{
		Root: root, Target: target, Bucket: BucketYear, Action: ActionCopy, Conflict: ConflictRename,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Status != "conflict" {
		t.Fatalf("rename plan = %+v", plan.Items)
	}
	if want := filepath.Join(target, "2022", "a (2).txt"); plan.Items[0].Dest != want {
		t.Fatalf("dest = %s, want %s", plan.Items[0].Dest, want)
	}

	planSkip, err := PlanTree(Options{
		Root: root, Target: target, Bucket: BucketYear, Action: ActionCopy, Conflict: ConflictSkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(planSkip.Items) != 1 || planSkip.Items[0].Status != "skipped" {
		t.Fatalf("skip plan = %+v", planSkip.Items)
	}
}

func TestExecuteCopyKeepsSource(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	src := filepath.Join(root, "b.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2020, 2, 3, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(src, when, when); err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: root, Target: target, Bucket: BucketMonth, Action: ActionCopy, Conflict: ConflictRename}
	res, err := Execute(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("copy should keep source: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "2020-02", "b.txt")); err != nil {
		t.Fatalf("dest missing: %v", err)
	}
}

func TestExecuteMoveRemovesSource(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	src := filepath.Join(root, "c.txt")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2020, 2, 3, 0, 0, 0, 0, time.Local)
	if err := os.Chtimes(src, when, when); err != nil {
		t.Fatal(err)
	}
	opts := Options{Root: root, Target: target, Bucket: BucketDay, Action: ActionMove, Conflict: ConflictRename}
	res, err := Execute(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Moved != 1 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("move should remove source, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "2020-02-03", "c.txt")); err != nil {
		t.Fatalf("dest missing: %v", err)
	}
}

// exifJPEG 构造一个只含 EXIF APP1（IFD0 内放 DateTimeOriginal）的最小 JPEG 头。
func exifJPEG(date string) []byte {
	// TIFF 小端：header(8) + IFD0(count 1 + entry 12 + next 4) + 时间字符串。
	tiff := make([]byte, 8)
	tiff[0], tiff[1] = 'I', 'I'
	binary.LittleEndian.PutUint16(tiff[2:4], 42)
	binary.LittleEndian.PutUint32(tiff[4:8], 8)

	ifd := make([]byte, 2+12+4)
	binary.LittleEndian.PutUint16(ifd[0:2], 1)
	e := ifd[2:14]
	binary.LittleEndian.PutUint16(e[0:2], 0x9003) // DateTimeOriginal
	binary.LittleEndian.PutUint16(e[2:4], 2)      // ASCII
	binary.LittleEndian.PutUint32(e[4:8], uint32(len(date)+1))
	binary.LittleEndian.PutUint32(e[8:12], uint32(8+len(ifd)))
	// next IFD offset 已为零值。
	tiff = append(tiff, ifd...)
	tiff = append(tiff, []byte(date)...)
	tiff = append(tiff, 0)

	app1 := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	size := len(app1) + 2
	out = append(out, byte(size>>8), byte(size))
	out = append(out, app1...)
	out = append(out, 0xFF, 0xD9)
	return out
}
