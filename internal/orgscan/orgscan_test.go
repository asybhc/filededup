package orgscan

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectTree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "deep", "b.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree, err := Collect(root)
	if err != nil {
		t.Fatal(err)
	}
	wantDirs := []string{"sub", "sub/deep"}
	if len(tree.Dirs) != len(wantDirs) || tree.Dirs[0] != wantDirs[0] || tree.Dirs[1] != wantDirs[1] {
		t.Fatalf("目录=%v，期望 %v", tree.Dirs, wantDirs)
	}
	if len(tree.Files) != 2 || len(tree.Entries) != 2 {
		t.Fatalf("文件=%d 条目=%d，期望各 2", len(tree.Files), len(tree.Entries))
	}
	for _, f := range tree.Files {
		switch f.Path {
		case "a.txt":
			if f.Size != 3 || f.SHA256 != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
				t.Fatalf("a.txt 枚举结果异常：%+v", f)
			}
		case "sub/deep/b.txt":
			if f.Size != 5 {
				t.Fatalf("b.txt 大小=%d", f.Size)
			}
		default:
			t.Fatalf("意外路径 %q", f.Path)
		}
	}
}
