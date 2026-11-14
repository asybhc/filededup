package engine_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEngineHasNoPlatformOrUIDependencies 强制"引擎纯 Go、无 cgo、无 UI/平台依赖"的架构约束。
// 这条测试是路线文档 6.1 的可执行版本：engine 只允许 stdlib 与 engine 内部包。
func TestEngineHasNoPlatformOrUIDependencies(t *testing.T) {
	forbiddenPrefixes := []string{
		"filededup/internal/platform",
		"filededup/internal/api",
		"filededup/internal/store",
		"filededup/internal/auth",
		"filededup/internal/cloud",
		"filededup/internal/update",
		"filededup/internal/ai",
		"filededup/internal/logx",
		"github.com/wailsapp/",
	}
	forbiddenExact := map[string]string{
		"C":                "engine 禁止 cgo",
		"unsafe":           "engine 禁止 unsafe",
		"os/exec":          "engine 禁止启动子进程",
		"net/http":         "engine 禁止直接使用 HTTP",
		"net":              "engine 禁止网络",
		"syscall":          "engine 禁止直接系统调用（应走 platform）",
		"golang.org/x/sys": "engine 禁止直接系统调用（应走 platform）",
	}

	// 已登记的纯 Go 官方扩展：WebP/TIFF 解码（标准库 image/* 不含这两种格式）。
	allowedPrefixes := []string{
		"golang.org/x/image/",
	}

	root := "."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range f.Imports {
			imp, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if msg, bad := forbiddenExact[imp]; bad {
				t.Errorf("%s 导入了 %q：%s", path, imp, msg)
			}
			for _, p := range forbiddenPrefixes {
				if imp == p || strings.HasPrefix(imp, p+"/") {
					t.Errorf("%s 导入了 %q：engine 不得依赖 %s", path, imp, p)
				}
			}
			allowed := false
			for _, p := range allowedPrefixes {
				if strings.HasPrefix(imp, p) {
					allowed = true
					break
				}
			}
			if allowed {
				continue
			}
			if strings.HasPrefix(imp, "filededup/internal/engine/") || !strings.Contains(imp, ".") {
				continue
			}
			t.Errorf("%s 导入了第三方包 %q：engine 只允许 stdlib 与 engine 内部包", path, imp)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 engine 源码失败: %v", err)
	}
}
