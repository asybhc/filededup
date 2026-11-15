# 桌面 UI 外壳（Wails v2 + React/TS）

当前版本：`0.8.1`（Go 版独立版本序列，从 `0.6.0-ui` 起算；见文末“版本号”）。功能范围见“已接通 / 未实现”。

## 构建与运行

```powershell
cd web && npm install && npm run build      # 产出 web/dist
cd .. && powershell -File scripts/build-desktop.ps1
./bin/fdedup-ui.exe
```

- 桌面版**必须**带 `desktop,production` 两个 tag 并用 GUI 子系统构建。只带 `desktop` 时，Wails 的
  [app_default_windows.go](https://github.com/wailsapp/wails/blob/v2.17.1/internal/app/app_default_windows.go)
  会被编进产物，运行后只弹 “Wails applications will not build without the correct build tags” 对话框，
  不会创建窗口；不加 `-H windowsgui` 还会先出现控制台黑窗。这两点都不再是环境问题。
- `scripts/build-desktop.ps1` 固定 tag 与子系统，并在构建后校验：产物不含上述 stub 文案、PE Subsystem=2、打印字节数与 SHA-256。
  构建桌面版只走这个脚本，不要直接用 `go build -tags desktop`。
- 不带 `desktop` tag 时，`cmd/filededup` 只有一个占位 main，三平台 CI 的 `go build ./...` 不会引入 WebView2 / WebKitGTK。
- 前端产物通过模块根的 `assets_desktop.go`（`//go:embed all:web/dist`）嵌入，因为 `go:embed` 不能引用上级目录。
- 浏览器预览：`cd web && npm run dev`，然后打开 `http://localhost:5173/`。
  没有 Wails 桥时会走 `src/mock.ts` 的合成后端，页面顶部显示“浏览器预览（合成数据）”。
  `?demo=results|preview|done|perf` 可直达对应步骤（只在非 Wails 环境生效）。

## 目录

| 路径 | 作用 |
| --- | --- |
| `cmd/filededup/app.go` | 绑定到前端的门面：扫描、分页、预览、执行、状态轮询 |
| `cmd/filededup/settings.go` | 设置持久化：读写用户配置目录、版本号与容错 |
| `cmd/filededup/organize.go` | 整理器门面：两套顺序、九步规划、试运行/真实执行、存疑项确认 |
| `cmd/filededup/organize_undo.go` | 最近一次整理的撤销记录落盘与回滚接口 |
| `cmd/filededup/organize_rules.go` | 整理规则导入导出（版本化 JSON、未知键与非法值校验） |
| `cmd/filededup/update.go` | 自动更新门面：检查、验签、下载、替换回滚、跳过版本 |
| `cmd/filededup/lanzou.go` | 蓝奏云门面：登录态、目录浏览、上传预检、上传执行与报告 |
| `internal/cloud/lanzou/` | 蓝奏云老版协议：挑战登录、列目录、上传、去重规则、DPAPI 凭据、上传缓存、报告 |
| `internal/update/` | 清单解析与验签、下载校验、替换与清理 |
| `cmd/fdedup-update-sign/` | 更新签名工具（私钥只在仓库外使用） |
| `cmd/filededup/main_desktop.go` | Wails 入口（`//go:build desktop`） |
| `cmd/filededup/license.go` | 授权门面：状态查询、机器码、导入许可证 |
| `cmd/filededup/app_smoke_test.go` | 后端接线冒烟测试（`-tags desktop`） |
| `internal/auth/` | 机器码、Ed25519/旧 ECDSA 验签、试用记录、授权状态 |
| `cmd/fdedup-keygen/` | 离线签发工具（密钥与私钥操作只在仓库外进行） |
| `web/src/App.tsx` | 模块切换与查重五步状态机 |
| `web/src/Organizer.tsx` | 九步整理页 |
| `web/src/Lanzou.tsx` | 蓝奏云上传页（登录、浏览、预检、上传进度、报告） |
| `web/src/api.ts` | Wails 桥 / 浏览器 mock 分派 |
| `web/src/components/VirtualList.tsx` | 固定行高虚拟滚动 |
| `web/src/styles.css` | 中文排版与布局 |

## 流程

选目录 → 扫描（实时进度、可取消）→ 结果列表（组 / 成员 / 保留项 / 可回收）→
选保留策略 → 预览 / 执行（回收站，执行前确认）→ 结束统计。

后端全程走现有引擎，未改引擎契约：

```
scan.RunWithOptions → plan.FromScan → plan.Validate → apply.Executor{Trash: platform.Trash, Journal: logx.OpenOps}
```

引擎唯一改动：`scan.Options` 新增**可选**回调 `Progress func(scan.Progress)`，零值无开销。

执行前有两道闸：前端确认弹窗 + 后端 `confirmed=false` 直接拒绝；永久删除还要额外勾选确认。

## 设置持久化

- 位置：`os.UserConfigDir()/FileDedup/ui-settings.json`（Windows 即 `%AppData%\FileDedup\ui-settings.json`），界面会显示真实路径。
- 内容：`{ schemaVersion, scan, organize, update, lanzou }`，覆盖查重页、整理页与蓝奏上传页的全部选项（阈值、删除方式、保留策略、排除目录、九步开关等）。
- 容错约定：
  - 缺键按默认值补齐（先填默认值，再用文件里存在的键覆盖）；
  - **多出来的键不判损坏**：读取时忽略，保存时原样保留；
  - 枚举/范围越界拉回默认值；
  - 只有整份文件不是合法 JSON 才回退默认并在界面上给出提示，下次保存会覆盖。
- 写入方式：临时文件 + `Rename` 原子替换，避免半截文件。
- 前端：启动时 `GetSettings` 恢复；扫描选项变更后 600ms 防抖写盘，整理页 500ms 防抖，输入过程不反复落盘。

## 九步整理

整理页接的是与 CLI 相同的引擎链路，UI 层不重新实现规则：

```
orgscan.Collect → orgscan.PrepareProviders → organize.Snapshot
  →（可选）organize.BuildPreviewPlan → organize.Plan → organize.Apply
```

- **两套顺序原样展示**：`GetOrganizeMeta` 对空快照分别按 `OrderStandalone` / `OrderPreview` 跑一次 `Plan`，直接取引擎返回的 ID 与标题，界面不复制顺序表，避免与引擎漂移。
  - 无预览计划（独立顺序）：改名前缀 → 改名后缀 → 特殊删除 → 去只读 → 内容/图片/音频去重 → 整理 → 清空目录。
  - 有预览计划：内容/图片/音频去重 → 改名 → 特殊删除 → 去只读 → 整理 → 清空目录。
- **步骤开关**：九步各自可关；关掉的步骤按 `skipped` 计数，不影响其余步骤顺序。
- **试运行与真跑**：
  - 试运行 `dryRun=true`，引擎只对 `move` / `ensure_dir` 建目录，其余动作计入 `skipped`，不动文件；结果页用橙色横幅与“试运行（未落盘）”标签区分。
  - 真实执行前弹出确认框，列出根目录、整理目标、顺序、删除方式和将执行的步骤；永久删除还需额外勾选。
  - 真实执行完成后用红色横幅与“真实执行”标签区分，并提示删除项去了回收站还是已永久删除。
- **仅规划（不改文件）**：`planOnly=true` 只跑 `Plan`、不跑 `Apply`，返回每步的存疑项与计数；用于先看清单再决定，零落盘。
- **存疑项逐条确认**：特殊删除中判为 `SpecialSuspect` 的文件以 `StepPlan.Suspects`（路径、原因、建议、评分）结构化回传。
  前端逐条“确认删除 / 保留（驳回）”，执行时只把确认项的 `organize.Fold(path)` 放进 `ConfirmedKeys`；
  未确认或驳回项保留在磁盘上，并计入 `SuspectsKept`。
- **整理后撤销**：真实执行成功后按反向动作持久化 `UndoRecord` 到 `os.UserConfigDir()/FileDedup/organize-undo.json`，只保留最近一次。
  `organize.Undo` 逆序回滚 rename / move / ensure_dir / clear_readonly；delete 与硬链接替换记为 `unrestorable` 单独计数，
  不假装恢复。全部成功才清空记录，有失败则保留以便重试。
- **规则导入导出**：导出为 `schemaVersion=1`、`kind: "filededup.organize.rules"` 的 JSON，根目录字段清空、不携带本机路径。
  导入用 `DisallowUnknownFields` 拒绝未知键，并校验版本、枚举、阈值范围、正则与 target 相对路径；失败不落盘、不改当前设置，
  成功导入后仍沿用当前根目录。
- 轮询：`GetOrganizeState` 400ms 一次，返回阶段（枚举 / 规划 / 执行）、每步动作数与计数、存疑项、前 200 条动作样例、执行统计与失败明细。

## 蓝奏云上传（一期）

一期范围与 C# 版对等：老版蓝奏云（`pc.woozooo.com` / `accounts.woozooo.com`）。优享版、网盘查重、分享创建都不在本期。

**联调状态**：登录、列目录、预检与**真实上传**都已用真实账号联调通过。上传联调在账号里建 `test-<日期>-<时间>` 临时目录，
只在该目录内操作，跑完删除文件与目录，并核对根目录条目数回到操作前（19 个文件夹 / 18 个文件）。
实测覆盖：正常上传、同名同大小命中云端重复并移动到「网盘重复文件」、>100 MiB 跳过、同目录二次预检缓存跳过、
首次读取失败后重试成功、上传中途取消、`upload-report-*.json/.csv` 落盘。
**未覆盖**：秒传（服务端从未返回秒传，按普通上传处理）、账号密码登录（联调用的是 C# 凭据导入）、被服务端限流（未触发 429）、
优享版与分享创建（不在本期）。

实测协议修正（与早期实现不同，已按此修正）：

- 新建文件夹 `task=2`（`folder_name` / `folder_description` / `parent_id=-1`），删除文件夹 `task=3`（`folder_id`），
  删除文件 `task=6`（`file_id`），三者成功都返回 `{"zt":1}`。
- 上传目标目录字段是 `folder_id`；文件部件必须叫 `upload_file`。写成 `folder_id_bb_n` 不报错但文件会落到根目录，
  文件部件叫 `file` 会返回 `{"zt":0,"info":"请选择需要上传的文件"}`。
- 子目录 `task=47` 为空时返回 `{"zt":2,"info":[{...目录自身...}],"text":[]}`，按空目录处理，不算登录失效。
- 子目录 `task=5` 的文件条目会带上级目录字段；`info` 是总页数，`page` 可能被忽略，翻页按首条 ID 去重后停止。
- 服务端会拒绝部分扩展名（实测 `.bin` 返回 `{"zt":0,"info":"不能上传.bin格式的文件"}`），失败按原文记入报告。
- 报告文件 `upload-report-*.json/.csv` 就写在本地目录里，预检会跳过它们，避免下一批把报告当成待上传文件。

- **登录态**：账号密码为主，粘贴 Cookie 为降级。
  - 登录：GET 登录页，命中阿里云 WAF 的 `acw_sc__v2` 挑战时本地算 Cookie 后重试；再 POST `task=uselogin`；成功时跟随 `msgs` 中转页收集 `ylogin` / `phpdisk_info`。
  - Cookie：接受整段 `a=1; b=2`、带换行或 `Cookie:` 前缀，至少要有 `ylogin` 与 `phpdisk_info`，否则原文报错。
  - 兼容导入：扫描 `%AppData%\FileDedupTool`，逐个 DPAPI 解密，取 `ylogin + phpdisk_info` 且 `platform` 为空或含 `lanzou` 的记录。
- **持久化**：DPAPI（CurrentUser）加密，落 `%AppData%\FileDedup\lanzou\lanzou.bin`；只存 Cookie，不存密码。
  目录 0700、临时文件 0600 + `Rename` 原子替换。界面只显示 `ylogin` 后四位，日志与报告不落明文。
  非 Windows 平台没有 DPAPI，`SupportsProtection=false`，保存会失败并在界面原文提示。
- **上传**：并发默认 3（1–5）、请求间隔默认 1500ms（1000–2000）、单文件 >100 MiB 直接跳过不发请求、失败重试 ≤3（429 与 4xx 不重试）、可取消。
  本机上传缓存 `%AppData%\FileDedup\lanzou\upload-cache.json`，键为「目录 + 路径 + 大小 + 修改时间」；
  **与 C# 版差异**：C# 键含 MD5，这里不额外整文件读取，内容变了但大小与修改时间没变会误判，可用「强制重传」绕过。
- **去重规则**：名字 `Trim` 后 `OrdinalIgnoreCase` 相等，且 `|本地 - 云端| <= max(2048, 本地/100)`；云端大小解析失败（`-1`）一律不匹配。
  缓存优先于云端判重：已经传过的文件不会被移动。重复项不上传，本地文件移动到上传目录**旁边**的「网盘重复文件」，重名追加序号不覆盖。
- **两道闸**：预检只读（不动本地文件）；开始上传要求后端 `confirmed=true`、设置里 `disclaimerAccepted=true`，并通过 `FeatureApply` 授权门控。
- **错误透传**：`APIError` 原样带出操作名、HTTP 状态码、服务端 `zt/info` 或 `code/msg` 与响应片段，不包装成「网络错误」，不假装成功。
- **结果分类**（沿用 C# 七种 + 汇总行）：上传成功、秒传、缓存跳过、超过 100 MiB 跳过、云端重复（本地已移动）、已取消、失败。
- **报告**：结束后在上传目录写 `upload-report-<yyyyMMdd-HHmmss>.json` 与同名 `.csv`（UTF-8 BOM，带引号转义），
  界面提供「打开报告」与「打开网页端」。分享创建不在本功能内，需要分享请到网页端手动操作。

## 授权与试用

细节（许可证格式、签发流程、机器码算法、试用加固）见 [license-and-trial.md](license-and-trial.md)。界面侧约定：

- 顶栏授权徽标始终可见（已授权 / 试用中 N 天 / 试用已到期 / 未授权），点击打开授权面板。
- 授权面板显示：状态、许可证来源（Ed25519 / 旧 ECDSA 只读兼容）、版本（standard/pro）、到期日（或永久）、
  本机机器码（可一键复制）、许可证文件路径、当前功能位、试用起止与剩余天数、以及未授权时的限制说明。
- 导入方式两种：文件选择框（`*.lic;*.fdlic;*.txt`，上限 64 KB）与直接粘贴文本。导入前先验签，验签失败不会落盘。
- **未授权不静默退化**：`apply`（执行删除）与 `organize`（整理真实执行）在后端被拒绝并返回中文原因（含机器码），
  前端同时禁用按钮并显示红色横幅；扫描与试运行始终可用。
- 启动时调用 `auth.TouchTrial()` 创建/刷新试用记录；记录与 C# 版共用存储，切换版本不会重置试用。

## 打包与分发

```powershell
cd web && npm install && npm run build
cd .. && powershell -File scripts/build-desktop.ps1     # 便携版 bin\fdedup-ui.exe
powershell -File scripts/build-installer.ps1            # 安装包 bin\FileDedupGo-Setup-<版本>.exe
```

- `build-desktop.ps1` 是 exe 的唯一入口；`build-installer.ps1` 先调用它，并在打包前再校验一次产物不是 stub。
- 版本号只在 `cmd/filededup/app.go` 的 `appVersion` 维护，**这是唯一来源**：
  - `build-desktop.ps1` 从中读取并生成 Windows 版本资源（`.syso`，构建后立即删除，不提交）；
  - `build-installer.ps1` 从中读取并以 `/DAPP_VERSION`、`/DAPP_VERSION4` 注入 NSIS，未注入时 `installer.nsi` 直接报错；
  - 前端版本由 `GetAppInfo` 返回，`web/package.json` 的 `0.0.0` 只是占位，不参与发布版本。
- exe 版本资源包含 CompanyName（一杯好茶）、ProductName（一杯好茶.文件整理 Go）、FileVersion/ProductVersion（四段版本）；
  构建后脚本会读取 `VersionInfo` 校验，不一致直接失败。
- 安装包（[scripts/installer.nsi](../scripts/installer.nsi)）：
  - NSIS 3.x，`RequestExecutionLevel user`，**只写 HKCU**，不需要管理员权限。
  - 安装到 `%LOCALAPPDATA%\Programs\FileDedupGo`；快捷方式为开始菜单“文件去重 Go”文件夹 + 桌面。
  - 与 C# 版 `FileDedupTool`（文件去重）共存：安装目录、`HKCU\Software\FileDedupGo`、
    `...\Uninstall\FileDedupGo`、exe 名 `fdedup-ui.exe` 全部与之错开。
  - 卸载删除安装目录、快捷方式与上述两个注册表项；**保留** `%AppData%\FileDedup`（设置、许可证）与
    `%AppData%\FileDedupTool`（与 C# 版共用的试用记录），避免卸载后重装被当成新用户重置试用。
- `makensis` 查找顺序：`-Makensis` 参数 > `C:\Users\chong.YUJIANG\.tools\nsis-3.13\makensis.exe` > PATH。

## 自动更新

- 入口：顶栏“检查更新”；有新版时按钮显示“有更新 vX”。更新源默认为空，**留空不检查**。
- 配置（`ui-settings.json` 的 `update` 段）：`manifestUrl`、`checkMode`（`auto` / `manual`）、
  `intervalHours`（1–720，默认 18）、`skippedVersion`。启动后按间隔自动检查一次，有更新只点亮入口，不主动弹窗。
- 信任链（全部失败即拒绝，没有“忽略签名继续”的开关）：
  1. 清单签名：Ed25519（更新专用密钥，与许可证密钥分开），签名覆盖除顶层 `signature` 外的全部字段；
  2. 包签名：Ed25519，覆盖包的 SHA-256 摘要；
  3. 下载后校验长度与 SHA-256，再校验包签名；
  4. `Replace` 前再次校验安装包，复制出 `.new` 后再校验一次。
- 清单格式（`schemaVersion: 1`、`kind: "filededup.update.manifest"`）：

```json
{
  "schemaVersion": 1,
  "kind": "filededup.update.manifest",
  "version": "0.8.1",
  "releasedAt": "2026-11-15T00:00:00+08:00",
  "notes": "更新说明",
  "assets": [
    {
      "name": "fdedup-ui.exe",
      "os": "windows",
      "arch": "amd64",
      "url": "https://example.com/fdedup-ui-0.8.1.exe",
      "size": 13021696,
      "sha256": "…64 位十六进制…",
      "signature": "…base64url…"
    }
  ],
  "signature": "…base64url…"
}
```

- 签发（私钥在仓库外，默认路径 `%USERPROFILE%\.tools\filededup-update-key\update.key`）：

```powershell
go run ./cmd/fdedup-update-sign genkey -out "$env:USERPROFILE\.tools\filededup-update-key\update.key"
go run ./cmd/fdedup-update-sign make-manifest -key <私钥> -version 0.8.1 `
  -file bin\fdedup-ui.exe -url https://example.com/fdedup-ui-0.8.1.exe -out bin\update.json
go run ./cmd/fdedup-update-sign sign-manifest -key <私钥> -in bin\update.json
```

- 替换与回滚：当前 exe 改名为 `.old` → 安装包复制为 `.new` 并校验 → 原子改名为正式 exe → 启动新程序；
  启动失败则删除新文件并把 `.old` 改回。成功后的 `.old` 在下次启动 `Cleanup` 时删除；若 exe 缺失则用 `.old` 恢复。
- 跳过此版本：写入 `skippedVersion`，相同版本的后续检查直接标为“已跳过”，界面可“取消跳过”。
- 安装包缓存：`os.UserCacheDir()/FileDedup/updates`（Windows 即 `%LOCALAPPDATA%\FileDedup\updates`）。
  启动时与安装成功后各清理一次，`update.PruneCache(dir, 2)` 的规则是：
  - `*.part`、`*.new`、`*.old` 残留一律删除（下载没完成、替换中断留下的），不看时间；
  - 其余普通文件按修改时间从新到旧，**保留最近 2 个安装包**（当前版本 + 上一个），更旧的删除；
  - 子目录与非普通文件不动；单个文件删不掉只记日志，不影响启动与更新主流程。
  运行中的旧进程会占住 `.old` 映像导致删不掉，因此 `.old` 始终留到下次启动由 `update.Cleanup` 处理。

验证状态（如实区分）：

| 环节 | 验证方式 |
| --- | --- |
| 真实密钥生成、清单签名、内置公钥验签、HTTP 取清单、下载、长度/摘要/包签名校验 | **实测**：`FILEDEDUP_UPDATE_KEY=… go test ./internal/update -run TestEmbeddedKeyEndToEnd` |
| 临时目录中的替换、启动失败回滚、`.old` 清理/恢复 | **实测**：`TestReplaceAndRollback`、`TestCleanupRestoresMissingExe` |
| 篡改清单、未知字段、错误公钥、摘要不符、非法 asset、版本比较 | 单元测试 |
| 真实安装目录下对**运行中** exe 的“取清单→验签→下载→复验→替换→重启→版本变更” | **实测**（0.8.0 → 0.8.1，替换后 FileVersion=0.8.1.0，`.old` 已清理） |
| 真实安装目录下对已签名但非 PE 的伪包替换，启动失败后回滚 | **实测**（回滚后 exe 哈希与替换前逐字节一致，无 `.old`/`.new` 残留） |
| 界面“下载并安装”按钮的人工点击路径 | **未实测**：上述闭环用命令行驱动同一套 `internal/update` 代码，未点界面按钮 |
| macOS / Linux 自更新 | **未实测**；目前只按单文件可执行程序替换实现，未处理 .app 包与代码签名 |

## 大目录性能

虚拟滚动：固定行高 72px、绝对定位、只渲染可视窗口 + overscan，滚动用 rAF 节流；行组件 `memo`，行容器 `contain: layout paint`。

实测（Edge headless + CDP，`--disable-gpu` 软件渲染，窗口 1440×900，空白页对照组 61 fps）：

| 指标 | 数值 |
| --- | --- |
| 合成数据规模 | 133,335 行（33,334 组） |
| 滚动容器总高度 | 9,600,120 px |
| 任意时刻 DOM 行数 | 18–28 |
| 页面自带帧率采样 | 60 fps（滚动突发瞬间 41 fps） |
| 单次滚动更新间隔 | 均值 21–25 ms，p95 35 ms，max 42 ms（含 16.7 ms 固定帧等待） |
| 滚到底部 | scrollTop 9,599,590 / 9,600,120，末行内容正确 |

结果页右上角有“性能自检：载入 10 万行合成数据”，可随时重跑。

### 扫描吞吐（8 万文件实测）

环境：Ryzen 7 5800H（8C/16T）、NVMe SSD、Defender 实时防护关闭。
数据：80,000 文件 / 400 目录 / 1.30 GB，含 25,000 个重复组；内容查重，未开相似图片/音频。

| 指标 | 第一次（冷） | 第二次 |
| --- | --- | --- |
| 枚举阶段 | 383 ms | 311 ms |
| 总耗时 | 13.1 s | 14.5 s |
| 吞吐 | 约 101 MB/s、5,800 文件/秒 | 约 90 MB/s |
| 峰值工作集 | 74.9 MB | 78.4 MB |
| 峰值分页私有 | 113.4 MB | 117.9 MB |

内存口径都是**峰值**：外部脚本每 100 ms 采样进程的 `PeakWorkingSet64` / `PeakPagedMemorySize64` 取最大，
不是常驻；扫描结束后常驻工作集只有几 MB。

取消：引擎从 `cancel()` 到 `RunWithOptions` 返回实测 **1 ms**（检查粒度是每个文件开始前 + 文件内每 1 MiB）。
界面点击取消后**本地立即**把 `scan.phase` 置为 `cancelling`，按钮当帧变灰，不等轮询；
300 ms 的 `getScanState` 轮询保持不变，只负责进度数字与最终 `cancelled` 状态——下调它只会增加 IPC 调用和重渲染，
取消的即时反馈已经不依赖它。

**哈希 worker 默认值 `min(NumCPU,4)` 是实测结论，不要调大**：同一份 8 万文件数据，
4 个 worker 13.2 s、8 个 14.7 s、16 个 15.8 s（峰值内存 75.6 / 82.8 / 104.1 MB）。
另用只做 `open/read/sha256/close` 的裸程序对照：4 worker 13.7 s（96.7 MB/s），16 worker 15.97 s，
与引擎耗时基本一致。说明瓶颈是每文件约 0.17 ms 的打开/读取开销（约 5–6 千文件/秒），不是 CPU 并行度；
并发再高只会加剧文件系统争用。

## 中文排版

字体栈 `Microsoft YaHei UI / Microsoft YaHei / PingFang SC / Noto Sans CJK SC`，行高 1.65；
表格列宽 `84px 300px minmax(420px,1fr) 110px 168px`，容器最小宽度 1240px。
路径列在行内 2 行截断，完整路径点击行后在右侧抽屉查看——这是为滚动性能做的取舍，不是排版缺陷。

## 已接通 / 未实现

界面首屏底部与整理页底部固定展示这两行，改功能时同步改这里，不要留下“点了没反应”的入口。

- 已接通：目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、
  九步整理（两套顺序 / 步骤开关 / 试运行 / 真实执行 / 冲突与保留策略 / 结果明细 / 仅规划 / 存疑项逐条确认或驳回 /
  最近一次整理撤销 / 规则导入导出）、蓝奏云上传一期（老版登录 / Cookie 降级 / C# 凭据导入 / 目录浏览 / 只读预检 /
  并发上传与取消 / 云端同名同大小去重 / 七类结果与汇总 / 报告导出，**登录、预检与上传均已用真实账号实测**）、
  自动更新（ed25519 清单与安装包验签、长度与 SHA-256 校验、备份与回滚、跳过版本、安装包缓存清理）、
  exe 与安装包版本资源、授权与试用（Ed25519 导入与状态、旧版只读兼容、机器码、试用加固、未授权门控）。
- 未实现：
  - 全局：系统托盘、自定义保留项、查重侧执行撤销、撤销历史。
  - 蓝奏云：优享版（二期）、网盘查重（三期，原版默认只列不删、删除需二次确认）、分享创建（C# 版没有、服务端接口不可用）。

## 已知限制

1. 桌面窗口在自动化会话里不做人工交互验证，桌面显示由用户复核；已确认 production 构建的进程能稳定存活并创建标题为
   “文件去重 FileDedup”的窗口（此前“环境承载不了窗口、3 秒退出”的结论来自缺少 `production` tag 的 stub，已作废）。
   后端用 `go test -tags desktop -run "TestAppSmoke|TestSettingsTolerance|TestOrganizeJob" ./cmd/filededup` 覆盖真实目录的扫描→预览→执行闭环、设置容错与整理执行。
2. 结果列表一次性载入全部组（分页 500 逐步拉取），超大结果集的内存占用未单独测量。
3. 预览只展示前 200 条动作样例，完整计划在执行时才交给执行器。
4. 整理器动作样例回传前 200 条；存疑项（`Suspects`）单独结构化回传，逐条确认后才会进入删除动作，未确认项保留在磁盘上。
5. 整理后撤销只保留最近一次真实执行，且只能回滚 rename / move / ensure_dir / clear_readonly；
   删除与硬链接替换记为“不可恢复”，统计里单列，不回滚。
6. Linux 与 macOS 的回收站（XDG Trash / `~/.Trash` / `.Trashes/$uid`）、机器码采集与单实例锁已实现，
   **只做了交叉编译与 `go vet` 验证，没有在真机运行过**；Windows 端仍是唯一经过真机验证的平台。
7. 旧 ECDSA 许可证只做了算法与格式层面的单元测试（临时密钥 + 合成 payload/P1363 签名），**没有用真实签发的旧许可证样本验证**：
   历史私钥不在手边，无法生成真样本。
8. exe 与安装包均内嵌版本资源（FileVersion/ProductVersion 为四段，如 `0.8.1.0`；界面显示三段 `0.8.1`）。
   版本资源在构建时由 `app.go` 的 `appVersion` 生成，`.syso` 不提交。
9. 更新签名私钥保存在仓库外（`%USERPROFILE%\.tools\filededup-update-key\update.key`），客户端只内置公钥；
   私钥一旦泄露需要换钥并重新发布客户端，内置公钥不支持远程轮换。
10. 自动更新的“下载→验签→替换运行中的 exe→重启”已在真实安装目录（`%LOCALAPPDATA%\Programs\FileDedupGo`）实测通过，
    含启动失败回滚；**界面“下载并安装”按钮本身未人工点击**，闭环用命令行驱动同一套 `internal/update` 代码。
11. macOS / Linux 的自更新按单文件可执行程序替换实现，未处理 `.app` 包、代码签名与权限提升，**未真机验证**。
12. 蓝奏云上传一期已用真实账号做过**上传联调**（临时 `test-` 目录，跑完删文件删目录并核对根目录回到 19/18）：
    正常上传、云端重复+本地移动、>100 MiB 跳过、缓存跳过、失败重试、中途取消、报告落盘都实测通过。
    **仍未覆盖**：秒传（服务端没有返回秒传）、账号密码登录、限流 429、优享版与分享创建。
    协议实测修正：根目录 `folder_id` 必须传 `-1`（传 `0` 返回 `{"zt":2}`），文件夹 ID 字段是 `fol_id`；
    上传目录字段是 `folder_id`、文件部件必须叫 `upload_file`；子目录为空时 `task=47` 返回 `zt=2` 但带目录自身 info；
    文件列表 `page` 会被服务端忽略、`info` 是总页数，已按此停止翻页。
    本机缓存键不含 MD5（与 C# 版不同），内容变但大小/修改时间不变会误判为已传，可用「强制重传」绕过。

## 版本号

Go 版走自己的序列，当前 `0.8.1`：

- `0.7.0` 补齐整理器三项（存疑项逐条确认、最近一次整理撤销、规则导入导出）；
- `0.8.0` 加自动更新与 exe/安装包版本资源，并按语义化版本升次版本号；
- `0.8.1` 是补丁版：取消按钮本地即时置为 `cancelling`（不等 300 ms 轮询）、更新包缓存清理
  （保留最近 2 个包，删除 `.part`/`.new`/`.old`），并补上真实安装目录的更新闭环与 8 万文件扫描实测数据；
- 仍是 `0.x`：整理器撤销范围有限（删除与硬链接替换不可恢复），macOS / Linux 未做真机验证，不足以标 1.0；
- 刻意不用 C# 版的 `1.2.x`，避免两个实现共用同一版本号造成混淆（安装包、日志、问题反馈都以 `0.8.1` 为准）。
