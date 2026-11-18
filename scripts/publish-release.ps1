# 发布单个版本的唯一入口：生成签名清单、校验一致性、创建/更新 GitHub release。
#
# 用法：
#   $env:FILEDEDUP_UPDATE_KEY = "$env:USERPROFILE\.tools\filededup-update-key\update.key"
#   powershell -File scripts/publish-release.ps1 -Version 0.10.18 -Notes "更新说明"
#
#   powershell -File scripts/publish-release.ps1 -Version 0.10.18 -Rebuild   # 先重建 exe 与安装包
#   powershell -File scripts/publish-release.ps1 -Version 0.10.18 -VerifyOnly # 只校验，不上传
#
# 约定：
#   - 私钥路径只能通过 -PrivateKey 或环境变量 FILEDEDUP_UPDATE_KEY 传入；
#     脚本里没有默认私钥路径，也不会把私钥复制进仓库（发现私钥在仓库内直接报错）。
#   - 清单资产固定为安装包，资产名是 ASCII 的 filededup-<version>-setup.exe；
#     便携版同时上传，但清单只指向安装包（客户端走静默安装器更新）。
#   - 版本号唯一来源是 cmd/filededup/app.go 的 appVersion；-Version 必须与它一致，
#     tag 必须与版本号一致，否则直接报错退出。
param(
    [string]$Version = "",
    [string]$PrivateKey = $env:FILEDEDUP_UPDATE_KEY,
    [string]$PortableExe = "",
    [string]$SetupExe = "",
    [string]$Tag = "",
    [string]$Repo = "asybhc/filededup",
    [string]$Notes = "",
    [string]$NotesFile = "",
    [string]$ManifestOut = "",
    [switch]$Rebuild,
    [switch]$SkipUpload,
    [switch]$VerifyOnly
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

function Assert-Condition([bool]$ok, [string]$message) {
    if (-not $ok) { throw $message }
}

function Get-FileMeta([string]$path) {
    $item = Get-Item -LiteralPath $path
    return [pscustomobject]@{
        Path   = $item.FullName
        Name   = $item.Name
        Size   = [int64]$item.Length
        SHA256 = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    }
}

Push-Location $root
$tempDir = $null
try {
    # ---- 1. 版本号与 tag：appVersion 是唯一来源 ----
    $appGo = Join-Path $root "cmd/filededup/app.go"
    $m = Select-String -Path $appGo -Pattern 'const appVersion = "([^"]+)"' | Select-Object -First 1
    if (-not $m) { throw "未能从 $appGo 解析 appVersion" }
    $appVersion = $m.Matches[0].Groups[1].Value

    if (-not $Version) { $Version = $appVersion }
    Assert-Condition ($Version -eq $appVersion) "-Version=$Version 与 app.go 的 appVersion=$appVersion 不一致：版本号只能有一个来源"

    $tagVersion = $Tag -replace '^[vV]', ''
    if (-not $Tag) { $Tag = $Version; $tagVersion = $Version }
    Assert-Condition ($tagVersion -eq $Version) "tag=$Tag 与版本号 $Version 不一致"

    if ($Version -notmatch '^(\d+)\.(\d+)\.(\d+)(?:\.(\d+))?$') {
        throw "版本号不是 1–4 段数字：$Version"
    }
    if ($Matches[4]) {
        $version4 = "$($Matches[1]).$($Matches[2]).$($Matches[3]).$($Matches[4])"
    } else {
        $version4 = "$($Matches[1]).$($Matches[2]).$($Matches[3]).0"
    }

    # ---- 2. 产物路径 ----
    if ($Rebuild) {
        & (Join-Path $PSScriptRoot "build-installer.ps1")
        if ($LASTEXITCODE -ne 0) { throw "build-installer.ps1 失败（退出码 $LASTEXITCODE）" }
    }
    if (-not $PortableExe) { $PortableExe = Join-Path $root "bin/文件整理.一杯好茶.exe" }
    if (-not $SetupExe) { $SetupExe = Join-Path $root "bin/文件整理.一杯好茶-Setup-$Version.exe" }
    if (-not $ManifestOut) { $ManifestOut = Join-Path $root "bin/update-manifest.json" }

    Assert-Condition (Test-Path -LiteralPath $PortableExe) "缺少便携版：$PortableExe"
    Assert-Condition (Test-Path -LiteralPath $SetupExe) "缺少安装包：$SetupExe"

    $portable = Get-FileMeta $PortableExe
    $setup = Get-FileMeta $SetupExe

    # 产物版本资源必须与版本号一致，防止拿错版本的包去发布。
    $portableVI = (Get-Item -LiteralPath $PortableExe).VersionInfo
    $setupVI = (Get-Item -LiteralPath $SetupExe).VersionInfo
    Assert-Condition ($portableVI.FileVersion -eq $version4) "便携版 FileVersion=$($portableVI.FileVersion)，期望 $version4"
    # NSIS 用 APP_VERSION 写 FileVersion（三段），这里按版本号本体比较。
    Assert-Condition ($setupVI.FileVersion -eq $Version) "安装包 FileVersion=$($setupVI.FileVersion)，期望 $Version"

    # ---- 3. ASCII 资产名 ----
    $setupAsset = "filededup-$Version-setup.exe"
    $portableAsset = "filededup-$Version.exe"
    foreach ($name in @($setupAsset, $portableAsset)) {
        Assert-Condition ($name -match '^[\x21-\x7E]+$') "资产名必须是 ASCII：$name"
        Assert-Condition ($name -notmatch '[\\/]') "资产名不能包含路径分隔符：$name"
    }

    # ---- 4. 生成并签名清单 ----
    $url = "https://github.com/$Repo/releases/download/$Tag/$setupAsset"
    if (-not $VerifyOnly) {
        if (-not $PrivateKey) {
            throw "缺少更新私钥：请用 -PrivateKey 或环境变量 FILEDEDUP_UPDATE_KEY 传入（私钥必须放在仓库外）"
        }
        Assert-Condition (Test-Path -LiteralPath $PrivateKey) "私钥文件不存在：$PrivateKey"
        $keyFull = (Resolve-Path -LiteralPath $PrivateKey).Path
        if ($keyFull.StartsWith($root, [StringComparison]::OrdinalIgnoreCase)) {
            throw "私钥不能放在仓库内：$keyFull"
        }
        if (-not $Notes -and $NotesFile) {
            Assert-Condition (Test-Path -LiteralPath $NotesFile) "更新说明文件不存在：$NotesFile"
            $Notes = Get-Content -LiteralPath $NotesFile -Raw
        }
        if (-not $Notes) { $Notes = "文件整理.一杯好茶 $Version" }

        $signArgs = @(
            "run", "./cmd/fdedup-update-sign", "make-manifest",
            "-key", $keyFull,
            "-version", $Version,
            "-file", $setup.Path,
            "-name", $setupAsset,
            "-url", $url,
            "-notes", $Notes,
            "-out", $ManifestOut
        )
        & go @signArgs
        if ($LASTEXITCODE -ne 0) { throw "生成签名清单失败（退出码 $LASTEXITCODE）" }
    }

    Assert-Condition (Test-Path -LiteralPath $ManifestOut) "缺少清单：$ManifestOut（-VerifyOnly 需要已有清单）"
    $manifest = Get-FileMeta $ManifestOut
    $man = [IO.File]::ReadAllText($ManifestOut, [Text.Encoding]::UTF8) | ConvertFrom-Json

    # ---- 5. 清单自校验：版本、资产名、大小、摘要、下载地址 ----
    Assert-Condition ([string]$man.version -eq $Version) "清单 version=$($man.version) 与 tag/appVersion=$Version 不一致"
    Assert-Condition ($man.assets.Count -eq 1) "清单应只有一个 asset，实际 $($man.assets.Count) 个"
    $asset = $man.assets[0]
    Assert-Condition ([string]$asset.name -eq $setupAsset) "清单资产名 $($asset.name) 与预期 $setupAsset 不一致"
    Assert-Condition ([string]$asset.name -match '^[\x21-\x7E]+$') "清单资产名不是 ASCII：$($asset.name)"
    Assert-Condition ([int64]$asset.size -eq $setup.Size) "清单 size=$($asset.size) 与本地安装包 $($setup.Size) 不一致"
    Assert-Condition ((([string]$asset.sha256).ToLowerInvariant()) -eq $setup.SHA256) "清单 sha256=$($asset.sha256) 与本地安装包 $($setup.SHA256) 不一致"
    Assert-Condition ([string]$asset.url -eq $url) "清单 url=$($asset.url) 与预期 $url 不一致"
    Assert-Condition ([string]$man.signature -ne "") "清单缺少 Ed25519 签名"

    Write-Output "清单自校验通过：version=$Version asset=$setupAsset size=$($setup.Size) sha256=$($setup.SHA256)"

    # ---- 6. 远端 release 现状 ----
    $releaseJson = $null
    $releaseExists = $false
    $raw = & gh api "repos/$Repo/releases/tags/$Tag" 2>$null
    if ($LASTEXITCODE -eq 0 -and $raw) {
        $releaseJson = $raw | ConvertFrom-Json
        $releaseExists = $true
    }
    if ($releaseExists) {
        Assert-Condition (-not $releaseJson.draft) "release $Tag 仍是 draft，请先发布"
        Assert-Condition (-not $releaseJson.prerelease) "release $Tag 被标记为预发布"
    }

    $expected = @(
        [pscustomobject]@{ Name = $setupAsset; Local = $setup },
        [pscustomobject]@{ Name = $portableAsset; Local = $portable },
        [pscustomobject]@{ Name = "update-manifest.json"; Local = $manifest }
    )

    $problems = New-Object System.Collections.Generic.List[string]
    if ($releaseExists) {
        foreach ($e in $expected) {
            $ra = $releaseJson.assets | Where-Object { $_.name -eq $e.Name } | Select-Object -First 1
            if (-not $ra) {
                $problems.Add("缺少线上资产 $($e.Name)")
                continue
            }
            if ([int64]$ra.size -ne $e.Local.Size) {
                $problems.Add("资产 $($e.Name) 大小不一致：线上=$($ra.size) 本地=$($e.Local.Size)")
            }
            $digest = ([string]$ra.digest) -replace '^sha256:', ''
            if ($digest -and $digest.ToLowerInvariant() -ne $e.Local.SHA256) {
                $problems.Add("资产 $($e.Name) SHA-256 不一致：线上=$digest 本地=$($e.Local.SHA256)")
            }
        }
    } else {
        $problems.Add("release $Tag 不存在")
    }

    if ($problems.Count -gt 0) {
        Write-Output "线上一致性检查："
        foreach ($p in $problems) { Write-Output "  - $p" }
    } else {
        Write-Output "线上一致性检查：三个资产的大小与 SHA-256 与本地一致"
    }

    if ($VerifyOnly) {
        if ($problems.Count -gt 0) { throw "只校验模式下线上资产与本地不一致（见上）" }
        Write-Output "VERIFY ONLY 通过：release $Tag 的清单与两个 exe 均与本地一致"
        return
    }

    # ---- 7. 创建/更新 release 并上传（exe 先传，清单最后传） ----
    if (-not $releaseExists) {
        $target = (git rev-parse HEAD).Trim()
        & gh release create $Tag --repo $Repo --title "文件整理.一杯好茶 $Version" --notes $Notes --target $target
        if ($LASTEXITCODE -ne 0) { throw "创建 release $Tag 失败（退出码 $LASTEXITCODE）" }
        $releaseExists = $true
        Write-Output "已创建 release $Tag（target=$target）"
    }

    $tempDir = Join-Path $env:TEMP ("filededup-publish-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

    if ($SkipUpload) {
        Write-Output "已指定 -SkipUpload：不执行上传"
    } else {
        Copy-Item -LiteralPath $portable.Path -Destination (Join-Path $tempDir $portableAsset) -Force
        Copy-Item -LiteralPath $setup.Path -Destination (Join-Path $tempDir $setupAsset) -Force
        Copy-Item -LiteralPath $manifest.Path -Destination (Join-Path $tempDir "update-manifest.json") -Force

        # 删除线上与本地不一致的资产，避免同名旧文件残留。
        foreach ($e in $expected) {
            $ra = $releaseJson.assets | Where-Object { $_.name -eq $e.Name } | Select-Object -First 1
            if (-not $ra) { continue }
            $digest = ([string]$ra.digest) -replace '^sha256:', ''
            $same = ([int64]$ra.size -eq $e.Local.Size) -and (-not $digest -or $digest.ToLowerInvariant() -eq $e.Local.SHA256)
            if (-not $same) {
                & gh api -X DELETE "repos/$Repo/releases/assets/$($ra.id)" | Out-Null
                if ($LASTEXITCODE -ne 0) { throw "删除旧资产 $($e.Name) 失败（退出码 $LASTEXITCODE）" }
                Write-Output "已删除线上旧资产 $($e.Name)"
            }
        }

        & gh release upload $Tag (Join-Path $tempDir $portableAsset) (Join-Path $tempDir $setupAsset) --repo $Repo --clobber
        if ($LASTEXITCODE -ne 0) { throw "上传 exe 失败（退出码 $LASTEXITCODE）" }
        & gh release upload $Tag (Join-Path $tempDir "update-manifest.json") --repo $Repo --clobber
        if ($LASTEXITCODE -ne 0) { throw "上传清单失败（退出码 $LASTEXITCODE）" }
        Write-Output "已上传：$portableAsset、$setupAsset、update-manifest.json（清单最后上传）"
    }

    # ---- 8. 上传后再核对一次线上资产 ----
    $finalRaw = & gh api "repos/$Repo/releases/tags/$Tag"
    if ($LASTEXITCODE -ne 0) { throw "核对 release $Tag 失败（退出码 $LASTEXITCODE）" }
    $final = $finalRaw | ConvertFrom-Json
    foreach ($e in $expected) {
        $ra = $final.assets | Where-Object { $_.name -eq $e.Name } | Select-Object -First 1
        Assert-Condition ($null -ne $ra) "线上缺少资产 $($e.Name)"
        Assert-Condition ([int64]$ra.size -eq $e.Local.Size) "线上 $($e.Name) 大小 $($ra.size) 与本地 $($e.Local.Size) 不一致"
        $digest = ([string]$ra.digest) -replace '^sha256:', ''
        if ($digest) {
            Assert-Condition ($digest.ToLowerInvariant() -eq $e.Local.SHA256) "线上 $($e.Name) SHA-256 与本地不一致"
        }
    }

    # 清单按 URL 再下载一次，逐字节比对（排除上传过程中被改写）。
    $manifestAsset = $final.assets | Where-Object { $_.name -eq "update-manifest.json" } | Select-Object -First 1
    $downloadURL = [string]$manifestAsset.browser_download_url
    $remoteManifest = Join-Path $tempDir "update-manifest.remote.json"
    for ($i = 0; $i -lt 5; $i++) {
        Invoke-WebRequest -Uri ($downloadURL + "?t=" + (Get-Random)) -OutFile $remoteManifest -UseBasicParsing
        $remoteHash = (Get-FileHash -LiteralPath $remoteManifest -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($remoteHash -eq $manifest.SHA256) { break }
        Start-Sleep -Seconds 2
    }
    $remoteHash = (Get-FileHash -LiteralPath $remoteManifest -Algorithm SHA256).Hash.ToLowerInvariant()
    Assert-Condition ($remoteHash -eq $manifest.SHA256) "线上清单内容与本地不一致：线上=$remoteHash 本地=$($manifest.SHA256)"

    $latestRaw = & gh api "repos/$Repo/releases/latest" 2>$null
    if ($LASTEXITCODE -eq 0 -and $latestRaw) {
        $latest = $latestRaw | ConvertFrom-Json
        if ($latest.tag_name -ne $Tag) {
            Write-Output "注意：当前 latest release 是 $($latest.tag_name)，不是 $Tag；客户端内置更新源只会看到 latest。"
        }
    }

    Write-Output ""
    Write-Output "发布完成：$Tag"
    Write-Output ("SETUP    " + $setupAsset + "  " + $setup.Size + " bytes  SHA256 " + $setup.SHA256)
    Write-Output ("PORTABLE " + $portableAsset + "  " + $portable.Size + " bytes  SHA256 " + $portable.SHA256)
    Write-Output ("MANIFEST " + "update-manifest.json" + "  " + $manifest.Size + " bytes  SHA256 " + $manifest.SHA256)
} finally {
    if ($tempDir -and (Test-Path -LiteralPath $tempDir)) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
    Pop-Location
}
