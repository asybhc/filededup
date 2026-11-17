//go:build windows

package platform

// Windows 回收站恢复。
//
// 方案选择（0.9.6）：
//   - 没有公开的“按原路径恢复”Win32 API；IFileOperation 也没有还原语义，
//     还原动作只以 Shell 动词的形式挂在回收站项上。
//   - 因此走 Shell.Application：Namespace(10) 枚举回收站项，先按
//     System.Recycle.DeletedFrom + 文件名收窄，再用大小与 System.Recycle.DateDeleted
//     唯一定位到正确版本，最后调用该项的 Restore 动词。
//   - 上一版把结果直接写 stdout，出现“没输出”时无法区分是脚本没跑、COM 打不开，
//     还是项没找到；现在改为把结果写独立的 UTF-8 结果文件，并带 START/DONE
//     标记，Go 侧能明确区分“脚本没产生结果”和“逐条失败”。
//   - 动词名带快捷键符号（实测为 `R&estore`），匹配前先去掉 `&`。
//
// 依赖 PowerShell 5.1（Windows 10/11 自带），不依赖第三方模块。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const restoreScript = `$ErrorActionPreference = 'Continue'
$tab = [char]9
$out = New-Object System.Collections.Generic.List[string]
$out.Add('START')
$fatal = $false
$targets = @()
try {
  foreach ($line in [System.IO.File]::ReadAllLines($env:FDEDUP_RESTORE_LIST, [System.Text.Encoding]::UTF8)) {
    if ([string]::IsNullOrWhiteSpace($line)) { continue }
    $targets += ($line | ConvertFrom-Json)
  }
} catch {
  $out.Add('FATAL' + $tab + ('读取恢复清单失败：' + $_.Exception.Message))
  $fatal = $true
}
$items = @()
if (-not $fatal) {
  $shell = $null
  $bin = $null
  try { $shell = New-Object -ComObject Shell.Application } catch { $shell = $null }
  if ($null -ne $shell) { try { $bin = $shell.Namespace(10) } catch { $bin = $null } }
  if ($null -eq $bin) {
    $out.Add('FATAL' + $tab + '无法打开回收站（Shell.Application 命名空间 10）')
    $out.Add('BIN' + $tab + '-1')
    $fatal = $true
  } else {
    $items = @($bin.Items())
  }
}
if (-not $fatal) {
  $out.Add('BIN' + $tab + $items.Count)
  foreach ($t in $targets) {
    $p = [string]$t.path
    if ([string]::IsNullOrWhiteSpace($p)) { continue }
    $full = $null
    try { $full = [System.IO.Path]::GetFullPath($p) } catch { $out.Add('FAIL' + $tab + $p + $tab + '路径非法'); continue }
    if (Test-Path -LiteralPath $full) { $out.Add('EXISTS' + $tab + $p + $tab + '原位置已有同名文件，未覆盖'); continue }
    $dir = [System.IO.Path]::GetDirectoryName($full)
    $name = [System.IO.Path]::GetFileName($full)
    if ($dir -and -not (Test-Path -LiteralPath $dir)) {
      try { New-Item -ItemType Directory -Path $dir -Force | Out-Null } catch {
        $out.Add('FAIL' + $tab + $p + $tab + ('无法创建目标目录：' + $_.Exception.Message))
        continue
      }
    }
    $wantSize = 0
    if ($null -ne $t.size) { try { $wantSize = [long]$t.size } catch { $wantSize = 0 } }
    $wantDel = $null
    $delText = [string]$t.deletedAt
    if ($delText -ne '') {
      try { $wantDel = [datetimeoffset]::Parse($delText, [Globalization.CultureInfo]::InvariantCulture) } catch { $wantDel = $null }
    }

    $cand = @()
    foreach ($it in $items) {
      if ($it.Name -ne $name) { continue }
      $from = $null
      try { $from = $it.ExtendedProperty('System.Recycle.DeletedFrom') } catch { $from = $null }
      if ($null -eq $from) { continue }
      try { $fromFull = [System.IO.Path]::GetFullPath([string]$from) } catch { continue }
      if ([string]::Equals($fromFull.TrimEnd('\'), $dir.TrimEnd('\'), [System.StringComparison]::OrdinalIgnoreCase)) { $cand += $it }
    }
    if ($cand.Count -eq 0) { $out.Add('MISS' + $tab + $p + $tab + '回收站中找不到原文件'); continue }
    if ($wantSize -gt 0) {
      $sized = @($cand | Where-Object {
        $s = $null
        try { $s = $_.ExtendedProperty('System.Size') } catch { $s = $null }
        if ($null -eq $s) { try { $s = $_.Size } catch { $s = $null } }
        return ($null -ne $s -and [long]$s -eq $wantSize)
      })
      if ($sized.Count -eq 0) {
        $out.Add('AMBIG' + $tab + $p + $tab + '回收站中的同名项大小与撤销记录不一致，无法确定要恢复的版本')
        continue
      }
      $cand = $sized
    }
    $hit = $null
    if ($cand.Count -eq 1) {
      $hit = $cand[0]
    } else {
      if ($null -ne $wantDel) {
        $best = $null
        $bestDiff = $null
        $tied = $false
        foreach ($it in $cand) {
          $d = $null
          try {
            $raw = $it.ExtendedProperty('System.Recycle.DateDeleted')
            if ($raw -is [datetime]) { $d = ([datetime]$raw).ToUniversalTime() }
            elseif ($raw) { $d = ([datetime]::Parse([string]$raw,
              [Globalization.CultureInfo]::InvariantCulture,
              ([Globalization.DateTimeStyles]::AdjustToUniversal -bor [Globalization.DateTimeStyles]::AssumeUniversal))) }
          } catch { $d = $null }
          if ($null -eq $d) { continue }
          $diff = [Math]::Abs(($d - $wantDel.UtcDateTime).TotalSeconds)
          if ($null -eq $bestDiff -or $diff -lt $bestDiff) { $bestDiff = $diff; $best = $it; $tied = $false }
          elseif ($diff -eq $bestDiff) { $tied = $true }
        }
        if ($null -ne $best -and -not $tied -and $bestDiff -le 300) { $hit = $best }
      }
    }
    if ($null -eq $hit) {
      $out.Add('AMBIG' + $tab + $p + $tab + ('回收站中有 ' + $cand.Count + ' 个同名项，无法确定要恢复的版本'))
      continue
    }
    $tried = $false
    $verbErr = $null
    foreach ($v in $hit.Verbs()) {
      $clean = $v.Name -replace '&', ''
      if ($clean -match '还原|Restore|复原') {
        $tried = $true
        try { $v.DoIt() } catch { $verbErr = $_.Exception.Message }
        break
      }
    }
    if (-not $tried) { $out.Add('NOVERB' + $tab + $p + $tab + '回收站项没有还原命令'); continue }
    if ($verbErr) { $out.Add('FAIL' + $tab + $p + $tab + ('还原命令执行失败：' + $verbErr)); continue }
    $ok = $false
    for ($i = 0; $i -lt 30; $i++) {
      Start-Sleep -Milliseconds 100
      if (Test-Path -LiteralPath $full) { $ok = $true; break }
    }
    if ($ok) { $out.Add('OK' + $tab + $p) } else { $out.Add('FAIL' + $tab + $p + $tab + '还原命令已执行但文件未出现') }
  }
}
$out.Add('DONE')
try {
  [System.IO.File]::WriteAllLines($env:FDEDUP_RESTORE_OUT, $out, (New-Object System.Text.UTF8Encoding($false)))
} catch { }
`

// RestoreFromTrash 按原路径把文件从回收站恢复。
//
// 逐条规则：
//   - 目标位置已有同名文件时不覆盖，直接给出 EXISTS 原因；
//   - 目标目录不存在时先创建目录再还原；
//   - 同名项可能有多条（同名文件被多次删除），按 Size 与 DeletedAt 唯一定位；
//     无法唯一确定时返回“无法确定要恢复的版本”，绝不静默取第一条；
//   - 结果文件缺失或没有 DONE 标记时，整批按“脚本未产生结果”失败，
//     不会把“没输出”当成“恢复成功”。
func RestoreFromTrash(targets []RestoreTarget) RestoreReport {
	rep := RestoreReport{Results: make([]RestoreResult, 0, len(targets)), BinItems: -1}
	if len(targets) == 0 {
		return rep
	}

	// 先在 Go 侧逐条核对目标位置，避免脚本跑起来再覆盖用户后来新建的同名文件。
	pending := make([]RestoreTarget, 0, len(targets))
	index := make(map[string]int, len(targets))
	for _, t := range targets {
		index[t.Path] = len(rep.Results)
		if _, err := os.Lstat(t.Path); err == nil {
			rep.Results = append(rep.Results, RestoreResult{Path: t.Path, Err: "原位置已有同名文件，未覆盖"})
			continue
		}
		rep.Results = append(rep.Results, RestoreResult{Path: t.Path})
		pending = append(pending, t)
	}
	if len(pending) == 0 {
		return rep
	}

	failAll := func(reason string) RestoreReport {
		for _, t := range pending {
			rep.Results[index[t.Path]] = RestoreResult{Path: t.Path, Err: reason}
		}
		return rep
	}

	list, err := os.CreateTemp("", "fdedup-restore-*.txt")
	if err != nil {
		return failAll("创建恢复清单失败：" + err.Error())
	}
	defer os.Remove(list.Name())
	// 每行一个 JSON 对象：路径里可能含制表符等分隔符，JSON 转义最稳。
	var body strings.Builder
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	for _, t := range pending {
		if err := enc.Encode(t); err != nil {
			list.Close()
			return failAll("写入恢复清单失败：" + err.Error())
		}
	}
	if _, err := list.WriteString(body.String()); err != nil {
		list.Close()
		return failAll("写入恢复清单失败：" + err.Error())
	}
	if err := list.Close(); err != nil {
		return failAll("写入恢复清单失败：" + err.Error())
	}

	script, err := os.CreateTemp("", "fdedup-restore-*.ps1")
	if err != nil {
		return failAll("创建恢复脚本失败：" + err.Error())
	}
	defer os.Remove(script.Name())
	// Windows PowerShell 5.1 按 ANSI 读取无 BOM 的 .ps1，含中文会被解码错位
	// （实测导致整个脚本语法错误、退出码 1 且不产生结果文件），因此必须带 UTF-8 BOM。
	if _, err := script.WriteString("\ufeff" + restoreScript); err != nil {
		script.Close()
		return failAll("写入恢复脚本失败：" + err.Error())
	}
	if err := script.Close(); err != nil {
		return failAll("写入恢复脚本失败：" + err.Error())
	}

	outFile, err := os.CreateTemp("", "fdedup-restore-*.out")
	if err != nil {
		return failAll("创建结果文件失败：" + err.Error())
	}
	outPath := outFile.Name()
	outFile.Close()
	os.Remove(outPath) // 让脚本自己创建，避免把空文件误当成“跑完了但没有结果”
	defer os.Remove(outPath)

	timeout := 15*time.Second + time.Duration(len(pending))*2*time.Second
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script.Name())
	cmd.Env = append(os.Environ(),
		"FDEDUP_RESTORE_LIST="+list.Name(),
		"FDEDUP_RESTORE_OUT="+outPath)
	runErr := cmd.Run()

	raw, readErr := os.ReadFile(outPath)
	if readErr != nil {
		reason := "恢复脚本未产生结果（可能被安全软件拦截）"
		if ctx.Err() == context.DeadlineExceeded {
			reason = fmt.Sprintf("恢复超时（%s）", timeout)
		} else if runErr != nil {
			reason = "恢复脚本未产生结果：" + runErr.Error()
		}
		return failAll(reason)
	}

	sawDone := false
	var fatal string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		switch parts[0] {
		case "START":
			continue
		case "DONE":
			sawDone = true
			continue
		case "FATAL":
			if len(parts) >= 2 {
				fatal = parts[1]
			}
			continue
		case "BIN":
			if len(parts) >= 2 {
				var n int
				if _, err := fmt.Sscanf(parts[1], "%d", &n); err == nil {
					rep.BinItems = n
				}
			}
			continue
		}
		if len(parts) < 2 {
			continue
		}
		i, ok := index[parts[1]]
		if !ok {
			continue
		}
		if parts[0] == "OK" {
			rep.Results[i] = RestoreResult{Path: parts[1], OK: true}
			continue
		}
		msg := parts[0]
		if len(parts) == 3 && parts[2] != "" {
			msg = parts[2]
		}
		rep.Results[i] = RestoreResult{Path: parts[1], Err: msg}
	}
	if !sawDone {
		reason := "恢复脚本未完成（结果文件缺少结束标记）"
		if fatal != "" {
			reason = fatal
		}
		for _, t := range pending {
			if rep.Results[index[t.Path]].OK {
				continue
			}
			rep.Results[index[t.Path]] = RestoreResult{Path: t.Path, Err: reason}
		}
	}
	return rep
}
