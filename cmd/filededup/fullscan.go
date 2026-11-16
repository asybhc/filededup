//go:build desktop

package main

import (
	"path/filepath"
)

// FullDiskVolume 是一个参与全盘查重的本地固定卷。
type FullDiskVolume struct {
	Path string `json:"path"`
	Free int64  `json:"free"`
}

// FullDiskPlan 是全盘查重的预检结果：可扫描的卷与强制排除项。
type FullDiskPlan struct {
	Supported    bool             `json:"supported"`
	Volumes      []FullDiskVolume `json:"volumes"`
	HardExcludes []string         `json:"hardExcludes"`
	Message      string           `json:"message,omitempty"`
}

// hardExcludeDirs 是全盘查重始终跳过的系统目录。引擎的排除项按目录名匹配，
// 因此这些名称在任意层级都会被跳过，不需要拼出每个卷下的绝对路径。
var hardExcludeDirs = []string{
	"Windows",
	"Program Files",
	"Program Files (x86)",
	"ProgramData",
	"$Recycle.Bin",
	"System Volume Information",
	"Recovery",
	"PerfLogs",
	"MSOCache",
	"Windows.old",
	"Config.Msi",
	"$WinREAgent",
}

// hardExcludeRootFiles 是卷根下的系统文件：被系统独占打开，既读不到也不该参与查重。
var hardExcludeRootFiles = []string{
	"pagefile.sys",
	"hiberfil.sys",
	"swapfile.sys",
	"DumpStack.log.tmp",
}

// fullDiskExcludes 汇集全盘查重的强制排除项：目录名 + 每个卷根下的系统文件绝对路径。
func fullDiskExcludes(volumes []string) []string {
	out := make([]string, 0, len(hardExcludeDirs)+len(volumes)*len(hardExcludeRootFiles))
	out = append(out, hardExcludeDirs...)
	for _, v := range volumes {
		for _, name := range hardExcludeRootFiles {
			out = append(out, filepath.Join(v, name))
		}
	}
	return out
}

// GetFullDiskPlan 返回当前可用的本地固定卷与强制排除项，供界面在勾选“全盘查重”时展示。
func (a *App) GetFullDiskPlan() FullDiskPlan {
	volumes, err := listFixedVolumes()
	if err != nil {
		return FullDiskPlan{
			Supported:    false,
			Volumes:      []FullDiskVolume{},
			HardExcludes: []string{},
			Message:      err.Error(),
		}
	}
	items := make([]FullDiskVolume, 0, len(volumes))
	for _, v := range volumes {
		// 取不到可用空间时不阻塞计划展示，Free 保持 0。
		free, _ := diskFree(v)
		items = append(items, FullDiskVolume{Path: v, Free: free})
	}
	return FullDiskPlan{
		Supported:    true,
		Volumes:      items,
		HardExcludes: fullDiskExcludes(volumes),
	}
}
