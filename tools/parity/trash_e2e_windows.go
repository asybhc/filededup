//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"filededup/internal/engine/organize"
	"filededup/internal/platform"
)

// shQueryRBInfo 是 SHQUERYRBINFO 的布局（cbSize + 两个 int64）。
type shQueryRBInfo struct {
	CbSize   uint32
	_        uint32
	Size     int64
	NumItems int64
}

func trashBinItems() (int64, error) {
	proc := syscall.NewLazyDLL("shell32.dll").NewProc("SHQueryRecycleBinW")
	info := shQueryRBInfo{CbSize: uint32(unsafe.Sizeof(shQueryRBInfo{}))}
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&info)))
	if int32(r) < 0 {
		return 0, fmt.Errorf("SHQueryRecycleBinW 失败（0x%08X）", uint32(r))
	}
	return info.NumItems, nil
}

// runTrashE2E 真实语料端到端：计划 → 执行（平台回收站）→ 回收站可见 → 还原 → 哈希校验。
func runTrashE2E(args []string) int {
	fs := flag.NewFlagSet("trash-e2e", flag.ContinueOnError)
	out := fs.String("out", "", "结果 JSON 输出路径（缺省打印到 stdout）")
	keep := fs.Bool("keep", false, "结束后保留临时目录，便于人工检查")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := os.MkdirTemp("", "filededup-trash-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if !*keep {
		defer os.RemoveAll(root)
	}

	name := fmt.Sprintf("copy-%d.txt", time.Now().UnixNano())
	keepPath := filepath.Join(root, "a.txt")
	dupPath := filepath.Join(root, "b", name)
	content := []byte("filededup trash e2e")
	if err := os.MkdirAll(filepath.Dir(dupPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, p := range []string{keepPath, dupPath} {
		if err := os.WriteFile(p, content, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
	}
	sum := sha256.Sum256(content)
	wantHash := hex.EncodeToString(sum[:])

	tree, err := collectOrgTree(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "枚举语料失败:", err)
		return 2
	}
	cfg := organize.DefaultConfig()
	// 语料步骤隔离：只跑内容去重，避免默认开启的改名/整理步骤先把文件名清洗或移动，
	// 否则进入回收站的路径就不是脚本创建的 b/copy-<nano>.txt。
	cfg.Steps = organize.StepSwitches{ContentDedup: true}
	cfg.Delete = organize.DeleteTrash
	snap := organize.Snapshot{Root: root, Target: "整理结果", Files: tree.Entries, Dirs: tree.Dirs}
	res := organize.Plan(cfg, snap, organize.Providers{})

	// 从计划中取实际被删除的文件名，便于报告直接引用。
	deletedName := name
	for _, sp := range res.Steps {
		for _, a := range sp.Actions {
			if a.Kind == organize.ActionDelete {
				deletedName = filepath.Base(filepath.FromSlash(a.From))
			}
		}
	}

	before, err := trashBinItems()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	hooks := orgExecHooks()
	hooks.Trash = platform.Trash
	rep, err := organize.Apply(root, res, organize.ExecOptions{}, hooks)
	if err != nil {
		fmt.Fprintln(os.Stderr, "执行失败:", err)
		return 1
	}
	after, err := trashBinItems()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}

	result := map[string]any{
		"root":           root,
		"deletedFile":    filepath.ToSlash(dupPath),
		"deletedName":    deletedName,
		"applied":        rep.Applied,
		"failed":         rep.Failed,
		"errors":         rep.Errors,
		"recycleBefore":  before,
		"recycleAfter":   after,
		"restored":       false,
		"restoredHashOK": false,
	}

	if _, statErr := os.Stat(dupPath); !os.IsNotExist(statErr) {
		result["error"] = "执行后原路径仍存在"
		return emitTrashE2E(result, *out)
	}
	if after != before+1 {
		result["error"] = fmt.Sprintf("回收站项目数未按预期 +1（%d → %d）", before, after)
		return emitTrashE2E(result, *out)
	}

	// 还原：Shell.Application → 回收站命名空间(10) → 先按唯一文件名匹配“还原”动词；
	// 找不到时回退到原始完整路径（TEMP 可能是 8.3 短名，故用叶名前后缀宽松匹配）。
	script := `
$ErrorActionPreference = 'Stop'
$shell = New-Object -ComObject Shell.Application
$bin = $shell.Namespace(10)
$item = $bin.Items() | Where-Object { $_.Name -eq $env:FILEDEDUP_E2E_NAME } | Select-Object -First 1
if (-not $item -and $env:FILEDEDUP_E2E_PATH) {
  $leaf = [System.IO.Path]::GetFileName($env:FILEDEDUP_E2E_PATH)
  $item = $bin.Items() | Where-Object {
    $p = [string]$_.Path
    $p -and ([System.IO.Path]::GetFileName($p) -eq $leaf -or $p -like "*\$leaf")
  } | Select-Object -First 1
}
if (-not $item) { Write-Error '未在回收站找到目标文件'; exit 3 }
$verb = $item.Verbs() | Where-Object { ($_.Name -replace '&','') -match '还原|Restore|復原|复元' } | Select-Object -First 1
if (-not $verb) { Write-Error '未找到还原动词'; exit 4 }
$verb.DoIt()
exit 0
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(),
		"FILEDEDUP_E2E_NAME="+name,
		"FILEDEDUP_E2E_PATH="+dupPath,
	)
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		result["error"] = "还原失败：" + err.Error() + " " + strings.TrimSpace(string(outBytes))
		return emitTrashE2E(result, *out)
	}

	restored := false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(dupPath); err == nil {
			restored = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	result["restored"] = restored
	if !restored {
		result["error"] = "还原后文件未回到原路径"
		return emitTrashE2E(result, *out)
	}
	raw, err := os.ReadFile(dupPath)
	if err != nil {
		result["error"] = "读取还原文件失败：" + err.Error()
		return emitTrashE2E(result, *out)
	}
	got := sha256.Sum256(raw)
	result["restoredHashOK"] = hex.EncodeToString(got[:]) == wantHash
	if !result["restoredHashOK"].(bool) {
		result["error"] = "还原文件哈希不一致"
		return emitTrashE2E(result, *out)
	}
	return emitTrashE2E(result, *out)
}

func emitTrashE2E(result map[string]any, out string) int {
	raw, _ := json.MarshalIndent(result, "", "  ")
	raw = append(raw, '\n')
	if out != "" {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err == nil {
			_ = os.WriteFile(out, raw, 0o644)
		}
	}
	os.Stdout.Write(raw)
	if _, failed := result["error"]; failed {
		return 1
	}
	return 0
}
