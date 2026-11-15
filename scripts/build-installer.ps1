# 打 NSIS 安装包（便携版 exe 已由 build-desktop.ps1 产出）。
#
# 用法：
#   powershell -File scripts/build-installer.ps1              # 先重建 exe 再打包
#   powershell -File scripts/build-installer.ps1 -SkipBuild   # 复用现有 bin\fdedup-ui.exe
#
# makensis 查找顺序：显式参数 -Makensis > 已知全路径 > PATH。
# 任何一步失败都直接报错，不会产出半成品。
param(
    [switch]$SkipBuild,
    [string]$Makensis = ""
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    # 版本号唯一来源：cmd/filededup/app.go 的 appVersion。
    $appGo = Join-Path $root "cmd/filededup/app.go"
    $m = Select-String -Path $appGo -Pattern 'const appVersion = "([^"]+)"' | Select-Object -First 1
    if (-not $m) { throw "未能从 $appGo 解析 appVersion" }
    $version = $m.Matches[0].Groups[1].Value
    $version4 = "$version.0"

    if (-not $SkipBuild) {
        & (Join-Path $PSScriptRoot "build-desktop.ps1")
        if ($LASTEXITCODE -ne 0) { throw "build-desktop.ps1 失败（退出码 $LASTEXITCODE）" }
    }

    $exe = Join-Path $root "bin/fdedup-ui.exe"
    if (-not (Test-Path $exe)) { throw "缺少 $exe：请先执行 build-desktop.ps1" }

    # 再挡一次 stub：即使有人绕过构建脚本，也不让普通 go build 的产物进安装包。
    $exeBytes = [IO.File]::ReadAllBytes($exe)
    if ([Text.Encoding]::ASCII.GetString($exeBytes).Contains("will not build without the correct build tags")) {
        throw "bin/fdedup-ui.exe 是 Wails stub（缺 production tag），拒绝打包"
    }

    # makensis：优先显式参数，其次用户级便携版全路径，最后 PATH。
    $makensisCandidates = @()
    if ($Makensis) { $makensisCandidates += $Makensis }
    $makensisCandidates += "C:\Users\chong.YUJIANG\.tools\nsis-3.13\makensis.exe"
    $onPath = Get-Command makensis.exe -ErrorAction SilentlyContinue
    if ($onPath) { $makensisCandidates += $onPath.Source }

    $makensisExe = $makensisCandidates | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
    if (-not $makensisExe) {
        throw "找不到 makensis.exe。请安装 NSIS 或用 -Makensis 指定全路径。"
    }

    $out = Join-Path $root "bin/FileDedupGo-Setup-$version.exe"
    if (Test-Path $out) { Remove-Item $out -Force }

    & $makensisExe "/V2" "/DAPP_VERSION=$version" "/DAPP_VERSION4=$version4" `
        "/DSRC_EXE=$exe" "/DOUT_FILE=$out" (Join-Path $PSScriptRoot "installer.nsi")
    if ($LASTEXITCODE -ne 0) { throw "makensis 失败（退出码 $LASTEXITCODE）" }
    if (-not (Test-Path $out)) { throw "makensis 未产出 $out" }

    Write-Output ""
    Write-Output "VERSION  $version"
    Write-Output ("PORTABLE " + $exe)
    Write-Output ("         " + (Get-Item $exe).Length + " bytes  SHA256 " + (Get-FileHash $exe -Algorithm SHA256).Hash)
    Write-Output ("SETUP    " + $out)
    Write-Output ("         " + (Get-Item $out).Length + " bytes  SHA256 " + (Get-FileHash $out -Algorithm SHA256).Hash)
    Write-Output ("MAKENSIS " + $makensisExe)
} finally {
    Pop-Location
}
