//go:build desktop && windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// everythingWindowsExtraDirs 收集注册表、服务与常见安装目录里的候选目录。
// es.exe 不随 Everything 1.5 安装包分发，实际可能被放在任意程序目录，
// 所以这里把能可靠读到的来源全部列出来，再由调用方逐个探测。
func everythingWindowsExtraDirs() []string {
	var dirs []string
	dirs = append(dirs, everythingRegistryDirs()...)
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		for _, name := range []string{"Everything", "Everything 1.5a", "Everything 1.4"} {
			dirs = append(dirs, filepath.Join(base, name))
		}
	}
	return dirs
}

// everythingRegistryDirs 从卸载项与服务 ImagePath 推断 Everything 安装目录。
func everythingRegistryDirs() []string {
	var dirs []string
	type view struct {
		root registry.Key
		sub  string
	}
	views := []view{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
	}
	for _, v := range views {
		k, err := registry.OpenKey(v.root, v.sub, registry.READ)
		if err != nil {
			continue
		}
		names, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, name := range names {
			sk, err := registry.OpenKey(v.root, v.sub+`\`+name, registry.READ)
			if err != nil {
				continue
			}
			display, _, _ := sk.GetStringValue("DisplayName")
			lower := strings.ToLower(display)
			if !strings.Contains(lower, "everything") {
				sk.Close()
				continue
			}
			if loc, _, err := sk.GetStringValue("InstallLocation"); err == nil && strings.TrimSpace(loc) != "" {
				dirs = append(dirs, strings.TrimSpace(loc))
			}
			if icon, _, err := sk.GetStringValue("DisplayIcon"); err == nil && strings.TrimSpace(icon) != "" {
				dirs = append(dirs, filepath.Dir(unquoteExePath(icon)))
			}
			sk.Close()
		}
	}
	// Everything 作为服务安装时，ImagePath 指向真实的 Everything.exe。
	svcKeys := []string{
		`SYSTEM\CurrentControlSet\Services\Everything`,
		`SYSTEM\CurrentControlSet\Services\Everything 1.5a`,
	}
	for _, svc := range svcKeys {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, svc, registry.READ)
		if err != nil {
			continue
		}
		if img, _, err := k.GetStringValue("ImagePath"); err == nil && strings.TrimSpace(img) != "" {
			dirs = append(dirs, filepath.Dir(unquoteExePath(img)))
		}
		k.Close()
	}
	return dirs
}

// unquoteExePath 去掉 ImagePath 常见的引号与命令行参数。
func unquoteExePath(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
		return strings.Trim(v, `"`)
	}
	return v
}

var (
	everythingScanMu   sync.Mutex
	everythingScanAt   time.Time
	everythingScanPath string
	everythingScanDone bool
)

// everythingScannedES 在常见程序目录里有界搜索 es.exe，作为最后兜底。
// 只在没有更可靠来源时执行，结果缓存 60 秒，避免每次刷新后端都全盘扫描。
func everythingScannedES() string {
	everythingScanMu.Lock()
	defer everythingScanMu.Unlock()
	if everythingScanDone && time.Since(everythingScanAt) < time.Minute {
		return everythingScanPath
	}
	everythingScanPath = scanESIn(everythingScanRoots(), 4, 20000)
	everythingScanAt = time.Now()
	everythingScanDone = true
	return everythingScanPath
}

func everythingScanRoots() []string {
	var roots []string
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		roots = append(roots, filepath.Join(dir, "Programs"))
	}
	for _, env := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if dir := os.Getenv(env); dir != "" {
			roots = append(roots, dir)
		}
	}
	return roots
}

// scanESIn 按目录深度上限搜索 es.exe；遇到权限错误直接跳过，不跟着符号链接走。
func scanESIn(roots []string, maxDepth, budget int) string {
	for _, root := range roots {
		if root == "" {
			continue
		}
		found := ""
		visited := 0
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			visited++
			if visited > budget {
				return filepath.SkipAll
			}
			if d.IsDir() {
				if path != root && dirDepth(root, path) >= maxDepth {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.EqualFold(d.Name(), "es.exe") {
				found = path
				return filepath.SkipAll
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func dirDepth(root, dir string) int {
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// everythingProcessRunning 判断 Everything 客户端是否在运行。
// es.exe 依赖它的 IPC；没在运行时 es.exe 会静默返回 0 条结果，需要明确报错。
func everythingProcessRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return false
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "Everything.exe") {
			return true
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return false
		}
	}
}
