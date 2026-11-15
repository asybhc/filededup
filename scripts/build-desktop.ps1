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
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    if (-not (Test-Path "web/dist/index.html")) {
        throw "web/dist 不存在：请先执行 cd web; npm install; npm run build"
    }

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

    $item = Get-Item $out
    $hash = (Get-FileHash $out -Algorithm SHA256).Hash
    Write-Output ("OK  " + $item.FullName)
    Write-Output ("SIZE " + $item.Length + " bytes")
    Write-Output ("SHA256 " + $hash)
} finally {
    Pop-Location
}
