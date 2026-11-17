//go:build desktop

package main

// 诊断：日志查看/清除 + 运行环境自检。全部为只读检查，唯一会写盘的动作是
// 清除日志（只删 logs 目录下的 *.log/*.jsonl/*.txt 文件，不删目录）。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"filededup/internal/brand"
)

// diagPathWithin 判断 child 是否位于 parent 之内（含相等）。
func diagPathWithin(child, parent string) bool {
	c := strings.ToLower(filepath.Clean(child))
	p := strings.ToLower(filepath.Clean(parent))
	if c == p {
		return true
	}
	return strings.HasPrefix(c, p+string(filepath.Separator))
}

// LogFileView 是日志列表项。
type LogFileView struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// LogOverview 是日志目录概览。
type LogOverview struct {
	Dir       string        `json:"dir"`
	Files     []LogFileView `json:"files"`
	TotalSize int64         `json:"totalSize"`
}

// ClearLogsResult 是清除日志的汇总。
type ClearLogsResult struct {
	Deleted int      `json:"deleted"`
	Freed   int64    `json:"freed"`
	Errors  []string `json:"errors"`
}

// EnvCheckItem 是一项自检结果。Status 取 ok / warn / fail。
type EnvCheckItem struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

// logsRoot 返回日志根目录（<数据目录>/logs，操作清单在其 ops 子目录）。
func logsRoot() (string, error) {
	return brand.DataSub("logs")
}

// logExt 判断是否是日志文件。
func logExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".log", ".jsonl", ".txt":
		return true
	default:
		return false
	}
}

// GetLogOverview 列出日志根目录（含 ops 子目录）下的日志文件，按修改时间倒序。
func (a *App) GetLogOverview() (LogOverview, error) {
	root, err := logsRoot()
	if err != nil {
		return LogOverview{}, err
	}
	out := LogOverview{Dir: root}
	for _, dir := range []string{root, filepath.Join(root, "ops")} {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !logExt(d.Name()) {
				return nil
			}
			// 只收集这两个目录下的直接文件，嵌套目录留给将来的日志分类。
			if filepath.Dir(path) != dir {
				return nil
			}
			info, ierr := d.Info()
			if ierr != nil {
				return nil
			}
			out.Files = append(out.Files, LogFileView{
				Name:    strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(root)+"/"),
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
			})
			out.TotalSize += info.Size()
			return nil
		})
	}
	sort.Slice(out.Files, func(i, j int) bool {
		return out.Files[i].ModTime > out.Files[j].ModTime
	})
	if out.Files == nil {
		out.Files = []LogFileView{}
	}
	return out, nil
}

// readLogTailLimit 是单次查看的最大尾部长度。
const readLogTailLimit = 512 << 10

// ReadLogFile 读取日志文件尾部；name 是 GetLogOverview 返回的相对路径。
func (a *App) ReadLogFile(name string) (string, error) {
	root, err := logsRoot()
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(name)
	if clean == "" || filepath.IsAbs(clean) || strings.Contains(clean, "..") || !logExt(clean) {
		return "", errors.New("日志文件名不合法")
	}
	full := filepath.Join(root, clean)
	if !diagPathWithin(full, root) {
		return "", errors.New("日志文件不在日志目录内")
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("读取日志失败：%w", err)
	}
	if len(data) > readLogTailLimit {
		head := "（文件较大，仅显示最后 " + fmt.Sprintf("%d KB", readLogTailLimit/1024) + "）\n"
		return head + string(data[len(data)-readLogTailLimit:]), nil
	}
	if len(data) == 0 {
		return "（日志为空）", nil
	}
	return string(data), nil
}

// ClearLogs 删除日志文件；正在被占用的文件会列在 Errors 里，不影响其它文件。
func (a *App) ClearLogs() (ClearLogsResult, error) {
	if _, err := logsRoot(); err != nil {
		return ClearLogsResult{}, err
	}
	ov, err := a.GetLogOverview()
	if err != nil {
		return ClearLogsResult{}, err
	}
	res := ClearLogsResult{Errors: []string{}}
	for _, f := range ov.Files {
		if err := os.Remove(f.Path); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s：%v", f.Name, err))
			continue
		}
		res.Deleted++
		res.Freed += f.Size
	}
	// 目录本身保留，仅清空文件。
	return res, nil
}

// RunEnvironmentCheck 执行一组轻量自检，返回可直接展示的明细。
func (a *App) RunEnvironmentCheck() []EnvCheckItem {
	items := []EnvCheckItem{
		{
			Name:   "应用版本",
			Status: "ok",
			Detail: fmt.Sprintf("%s %s（%s/%s）", brand.DisplayName, appVersion, runtime.GOOS, runtime.GOARCH),
		},
	}

	cfg, err := os.UserConfigDir()
	if err != nil {
		items = append(items, EnvCheckItem{
			Name: "配置目录", Status: "fail",
			Detail: "无法定位用户配置目录：" + err.Error(),
			Fix:    "检查系统环境变量 APPDATA / HOME 是否正常。",
		})
	} else {
		base := filepath.Join(cfg, brand.DataDirName)
		if err := os.MkdirAll(base, 0o755); err != nil {
			items = append(items, EnvCheckItem{
				Name: "配置目录", Status: "fail", Detail: err.Error(),
				Fix: "确认当前用户对该目录有写权限：" + base,
			})
		} else {
			probe := filepath.Join(base, ".diag-probe")
			werr := os.WriteFile(probe, []byte("ok"), 0o600)
			_ = os.Remove(probe)
			if werr != nil {
				items = append(items, EnvCheckItem{
					Name: "配置目录", Status: "fail", Detail: base + " 不可写：" + werr.Error(),
					Fix: "检查磁盘只读属性、安全软件拦截或目录权限。",
				})
			} else {
				items = append(items, EnvCheckItem{
					Name: "配置目录", Status: "ok", Detail: "可写：" + base,
				})
			}
		}
	}

	if root, err := logsRoot(); err != nil {
		items = append(items, EnvCheckItem{
			Name: "日志目录", Status: "warn", Detail: err.Error(),
			Fix: "日志目录不可用时，操作清单将无法写入；检查配置目录。",
		})
	} else if ov, err := a.GetLogOverview(); err == nil {
		items = append(items, EnvCheckItem{
			Name:   "日志目录",
			Status: "ok",
			Detail: fmt.Sprintf("%s，%d 个文件，共 %.1f KB", root, len(ov.Files), float64(ov.TotalSize)/1024),
		})
	}

	if runtime.GOOS == "windows" {
		items = append(items, EnvCheckItem{
			Name: "回收站", Status: "ok",
			Detail: "使用 Windows 回收站（SHFileOperation），删除项可在资源管理器中还原。",
		})
	} else {
		items = append(items, EnvCheckItem{
			Name: "回收站", Status: "warn",
			Detail: "非 Windows 平台使用 XDG/系统回收站；若桌面环境不支持，删除会直接失败而不会静默丢失文件。",
		})
	}

	if settingsFile, err := brand.DataSub("ui-settings.json"); err == nil {
		if _, statErr := os.Stat(settingsFile); statErr == nil {
			items = append(items, EnvCheckItem{
				Name: "配置文件", Status: "ok", Detail: "ui-settings.json 可读取，旧配置缺失键按默认值处理。",
			})
		} else {
			items = append(items, EnvCheckItem{
				Name: "配置文件", Status: "warn", Detail: "尚未生成 ui-settings.json，首次启动会使用默认设置。",
			})
		}
	}

	items = append(items, everythingCheck())

	if cfg != "" {
		if free, err := diskFree(cfg); err == nil {
			status := "ok"
			if free < 500<<20 {
				status = "warn"
			}
			items = append(items, EnvCheckItem{
				Name: "配置盘剩余空间", Status: status,
				Detail: fmt.Sprintf("%.1f GB 可用", float64(free)/(1<<30)),
				Fix:    "空间不足时清理更新缓存或旧日志。",
			})
		}
	}

	if up, err := updateCacheDir(); err == nil {
		var size int64
		var count int
		_ = filepath.WalkDir(up, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if info, ierr := d.Info(); ierr == nil {
				size += info.Size()
				count++
			}
			return nil
		})
		items = append(items, EnvCheckItem{
			Name: "自动更新缓存", Status: "ok",
			Detail: fmt.Sprintf("%s：%d 个文件，共 %.1f MB", up, count, float64(size)/(1<<20)),
		})
	}
	return items
}

func everythingCheck() EnvCheckItem {
	if p, err := exec.LookPath("es.exe"); err == nil {
		return EnvCheckItem{Name: "Everything 搜索后端", Status: "ok", Detail: "已检测到 " + p + "（供搜索功能选用）。"}
	}
	return EnvCheckItem{
		Name: "Everything 搜索后端", Status: "warn",
		Detail: "未在 PATH 中找到 es.exe；搜索页仍可使用本地文件系统后端。",
		Fix:    "如需 Everything 后端，请安装 Everything 并把 es.exe 放入 PATH。",
	}
}

// diskFree 返回路径所在卷的可用字节数（Windows 用 GetDiskFreeSpaceEx，其它平台用 Statfs 近似）。
func diskFree(path string) (int64, error) {
	return diskFreeImpl(path)
}
