//go:build windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

// shQueryRBInfo 是 SHQUERYRBINFO 的布局（cbSize + 两个 int64）。
type shQueryRBInfo struct {
	CbSize   uint32
	_        uint32
	Size     int64
	NumItems int64
}

// recycleBinItems 返回当前用户回收站项目数；查询失败时跳过测试。
func recycleBinItems(t *testing.T) int64 {
	t.Helper()
	proc := syscall.NewLazyDLL("shell32.dll").NewProc("SHQueryRecycleBinW")
	info := shQueryRBInfo{CbSize: uint32(unsafe.Sizeof(shQueryRBInfo{}))}
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&info)))
	if int32(r) < 0 {
		t.Skipf("SHQueryRecycleBinW 不可用（0x%08X）", uint32(r))
	}
	return info.NumItems
}

// TestTrashWindowsRecycleBin 验证真实删除：原路径消失，回收站项目 +1。
func TestTrashWindowsRecycleBin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "中文 名 空格.txt")
	if err := os.WriteFile(path, []byte("hello recycle bin"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := recycleBinItems(t)
	if err := Trash(path); err != nil {
		t.Fatalf("移入回收站失败：%v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("原路径仍存在（err=%v）", err)
	}
	after := recycleBinItems(t)
	if after != before+1 {
		t.Fatalf("回收站项目数未增加：before=%d after=%d", before, after)
	}
}

// TestTrashWindowsRejectsUNC 验证网络路径被明确拒绝，而不是静默永久删除。
func TestTrashWindowsRejectsUNC(t *testing.T) {
	err := Trash(`\\server\share\a.txt`)
	if err == nil {
		t.Fatal("UNC 路径应返回错误")
	}
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("UNC 错误不应是 ErrUnsupported：%v", err)
	}
	if _, statErr := os.Lstat(`\\server\share\a.txt`); statErr != nil {
		t.Logf("预期：UNC 路径不可达（%v）", statErr)
	}
}
