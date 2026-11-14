//go:build windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkCrossVolumeRejected(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "keep.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	same, err := SameVolume(src, `D:\fdedup-nonexistent\dup.bin`)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Skip("本机 C: 与 D: 不是不同卷，跳过跨卷判定")
	}
	if err := Link(src, `D:\fdedup-nonexistent\dup.bin`); !errors.Is(err, ErrCrossVolume) {
		t.Fatalf("跨卷应返回 ErrCrossVolume，实际 %v", err)
	}
}
