# 桌面版唯一受支持的构建入口。
#
# 为什么必须走这里：
#   - Wails v2 的运行时代码用 build tag 区分实现，缺少 `production` 时
#     internal/app/app_default_windows.go 会被编进来，运行后只弹
#     “Wails applications will not build without the correct build tags” 对话框，
#     不会创建窗口。只加 `desktop` 是不够的。
#   - 不加 `-H windowsgui` 会编成控制台程序，双击时先出现黑窗。
# 因此这里固定 `desktop,production` + GUI 子系统，并在构建后校验产物，
# 防止再把 stub 当成可交付物。
#
# 版本号唯一来源：cmd/filededup/app.go 的 appVersion。
# 本脚本据此生成 Windows 版本资源（.syso），构建完成后立即删除 .syso，
# 避免仓库里出现第二份版本号。右键 exe 属性即可看到文件/产品版本与公司、产品名。
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
$syso = Join-Path $root "cmd/filededup/rsrc_windows_amd64.syso"
try {
    if (-not (Test-Path "web/dist/index.html")) {
        throw "web/dist 不存在：请先执行 cd web; npm install; npm run build"
    }

    $appGo = Join-Path $root "cmd/filededup/app.go"
    $m = Select-String -Path $appGo -Pattern 'const appVersion = "([^"]+)"' | Select-Object -First 1
    if (-not $m) { throw "未能从 $appGo 解析 appVersion" }
    $version = $m.Matches[0].Groups[1].Value
    if ($version -notmatch '^(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?') {
        throw "appVersion 不是 1–4 段数字版本：$version"
    }
    if ($Matches[4]) {
        $version4 = "$($Matches[1]).$($Matches[2]).$($Matches[3]).$($Matches[4])"
    } else {
        $version4 = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"
    }

    # 版本资源工具：优先 PATH/GOPATH 里已有的 go-winres，缺失时自动安装。
    $winres = Join-Path (go env GOPATH) "bin/go-winres.exe"
    if (-not (Test-Path $winres)) {
        $onPath = Get-Command go-winres.exe -ErrorAction SilentlyContinue
        if ($onPath) {
            $winres = $onPath.Source
        } else {
            Write-Output "go-winres 未安装，正在执行 go install github.com/tc-hib/go-winres@latest ..."
            & go install github.com/tc-hib/go-winres@latest
            if ($LASTEXITCODE -ne 0) { throw "go-winres 安装失败（退出码 $LASTEXITCODE）" }
        }
    }
    if (-not (Test-Path $winres)) { throw "找不到 go-winres：$winres" }

    if (Test-Path $syso) { Remove-Item $syso -Force }
    $winresJSON = Join-Path $root "cmd/filededup/winres.generated.json"
    $json = @"
{
  "RT_MANIFEST": {
    "#1": {
      "0409": {
        "identity": { "name": "filededup", "version": "$version4" },
        "description": "文件去重 Go",
        "execution-level": "as invoker",
        "ui-access": false,
        "auto-elevate": false,
        "dpi-awareness": "system",
        "long-path-aware": true
      }
    }
  },
  "RT_VERSION": {
    "#1": {
      "0000": {
        "fixed": { "file_version": "$version4", "product_version": "$version4" },
        "info": {
          "0409": {
            "CompanyName": "一杯好茶",
            "FileDescription": "一杯好茶.文件整理 Go",
            "FileVersion": "$version4",
            "InternalName": "fdedup-ui",
            "LegalCopyright": "Copyright © 一杯好茶",
            "OriginalFilename": "fdedup-ui.exe",
            "ProductName": "一杯好茶.文件整理 Go",
            "ProductVersion": "$version4"
          }
        }
      }
    }
  }
}
"@
    [IO.File]::WriteAllText($winresJSON, $json, (New-Object Text.UTF8Encoding($false)))
    Push-Location (Join-Path $root "cmd/filededup")
    try {
        & $winres make --in "winres.generated.json" --arch amd64 --out "rsrc"
        if ($LASTEXITCODE -ne 0) { throw "go-winres 生成版本资源失败（退出码 $LASTEXITCODE）" }
    } finally {
        Pop-Location
        if (Test-Path $winresJSON) { Remove-Item $winresJSON -Force }
    }
    if (-not (Test-Path $syso)) { throw "go-winres 未产出 $syso" }

    $out = Join-Path $root "bin/fdedup-ui.exe"
    & go build -tags "desktop,production" -trimpath -ldflags "-s -w -H windowsgui" -o $out ./cmd/filededup
    if ($LASTEXITCODE -ne 0) { throw "go build 失败（退出码 $LASTEXITCODE）" }

    $bytes = [IO.File]::ReadAllBytes($out)

    # 校验 1：不得包含 Wails 的 missing-tags stub 文案。
    $text = [Text.Encoding]::ASCII.GetString($bytes)
    if ($text.Contains("will not build without the correct build tags")) {
        throw "产物是 Wails stub：缺少 production build tag"
    }

    # 校验 2：PE Subsystem 必须为 2 (Windows GUI)，否则双击会先出黑窗。
    $pe = [BitConverter]::ToInt32($bytes, 0x3C)
    $subsystem = [BitConverter]::ToUInt16($bytes, $pe + 0x5C)
    if ($subsystem -ne 2) { throw "PE Subsystem=$subsystem，不是 Windows GUI 子系统" }

    # 校验 3：版本资源必须写入成功。
    $vi = (Get-Item $out).VersionInfo
    if ($vi.FileVersion -ne $version4) { throw "FileVersion=$($vi.FileVersion)，期望 $version4" }
    if ($vi.ProductVersion -ne $version4) { throw "ProductVersion=$($vi.ProductVersion)，期望 $version4" }
    if ($vi.CompanyName -ne "一杯好茶") { throw "CompanyName=$($vi.CompanyName)，期望 一杯好茶" }
    if ($vi.ProductName -ne "一杯好茶.文件整理 Go") { throw "ProductName=$($vi.ProductName)" }

    $item = Get-Item $out
    $hash = (Get-FileHash $out -Algorithm SHA256).Hash
    Write-Output ("OK  " + $item.FullName)
    Write-Output ("VERSION " + $version + "  (" + $version4 + ")")
    Write-Output ("VERINFO FileVersion=" + $vi.FileVersion + " ProductVersion=" + $vi.ProductVersion)
    Write-Output ("VERINFO Company=" + $vi.CompanyName + " Product=" + $vi.ProductName)
    Write-Output ("SIZE " + $item.Length + " bytes")
    Write-Output ("SHA256 " + $hash)
} finally {
    if (Test-Path $syso) { Remove-Item $syso -Force }
    Pop-Location
}
