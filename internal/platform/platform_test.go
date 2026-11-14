package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkCreatesHardlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "keep.bin")
	dst := filepath.Join(dir, "dup.bin")
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Link(src, dst); err != nil {
		t.Fatalf("创建硬链接失败：%v", err)
	}
	si, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(si, di) {
		t.Fatalf("两个路径未指向同一文件")
	}

	if err := Link(src, dst); !errors.Is(err, ErrLinkExists) {
		t.Fatalf("目标已存在时应返回 ErrLinkExists，实际 %v", err)
	}
}

func TestSameVolumeSameDir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	same, err := SameVolume(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatalf("同一临时目录应判定为同卷")
	}
}

func TestAcquireInstance(t *testing.T) {
	release, running, err := AcquireInstance()
	if err != nil {
		t.Fatalf("获取单实例失败：%v", err)
	}
	if running {
		t.Fatalf("首次获取不应判定为已有实例")
	}
	release2, running2, err := AcquireInstance()
	if err != nil {
		t.Fatalf("二次获取失败：%v", err)
	}
	if !running2 {
		t.Fatalf("第二次获取应判定为已有实例")
	}
	release2()
	release()
}
