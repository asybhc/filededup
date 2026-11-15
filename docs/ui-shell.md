# 桌面 UI 外壳（Wails v2 + React/TS）

当前版本：`0.8.0`（Go 版独立版本序列，从 `0.6.0-ui` 起算；见文末“版本号”）。功能范围见“已接通 / 未实现”。

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
| `cmd/filededup/main_desktop.go` | Wails 入口（`//go:build desktop`） |
| `cmd/filededup/license.go` | 授权门面：状态查询、机器码、导入许可证 |
| `cmd/filededup/app_smoke_test.go` | 后端接线冒烟测试（`-tags desktop`） |
| `internal/auth/` | 机器码、Ed25519/旧 ECDSA 验签、试用记录、授权状态 |
| `cmd/fdedup-keygen/` | 离线签发工具（密钥与私钥操作只在仓库外进行） |
| `web/src/App.tsx` | 模块切换与查重五步状态机 |
| `web/src/Organizer.tsx` | 九步整理页 |
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
- 内容：`{ schemaVersion, scan, organize }`，覆盖查重页与整理页的全部选项（阈值、删除方式、保留策略、排除目录、九步开关等）。
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
- 版本号只在 `cmd/filededup/app.go` 的 `appVersion` 维护；打包脚本从中读取，前端版本见 `web/package.json`，两处需同步。
- 安装包（[scripts/installer.nsi](../scripts/installer.nsi)）：
  - NSIS 3.x，`RequestExecutionLevel user`，**只写 HKCU**，不需要管理员权限。
  - 安装到 `%LOCALAPPDATA%\Programs\FileDedupGo`；快捷方式为开始菜单“文件去重 Go”文件夹 + 桌面。
  - 与 C# 版 `FileDedupTool`（文件去重）共存：安装目录、`HKCU\Software\FileDedupGo`、
    `...\Uninstall\FileDedupGo`、exe 名 `fdedup-ui.exe` 全部与之错开。
  - 卸载删除安装目录、快捷方式与上述两个注册表项；**保留** `%AppData%\FileDedup`（设置、许可证）与
    `%AppData%\FileDedupTool`（与 C# 版共用的试用记录），避免卸载后重装被当成新用户重置试用。
- `makensis` 查找顺序：`-Makensis` 参数 > `C:\Users\chong.YUJIANG\.tools\nsis-3.13\makensis.exe` > PATH。

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

## 中文排版

字体栈 `Microsoft YaHei UI / Microsoft YaHei / PingFang SC / Noto Sans CJK SC`，行高 1.65；
表格列宽 `84px 300px minmax(420px,1fr) 110px 168px`，容器最小宽度 1240px。
路径列在行内 2 行截断，完整路径点击行后在右侧抽屉查看——这是为滚动性能做的取舍，不是排版缺陷。

## 已接通 / 未实现

界面首屏底部与整理页底部固定展示这两行，改功能时同步改这里，不要留下“点了没反应”的入口。

- 已接通：目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、
  九步整理（两套顺序 / 步骤开关 / 试运行 / 真实执行 / 冲突与保留策略 / 结果明细 / 仅规划 / 存疑项逐条确认或驳回 /
  最近一次整理撤销 / 规则导入导出）、授权与试用（Ed25519 导入与状态、旧版只读兼容、机器码、试用加固、未授权门控）。
- 未实现：
  - 全局：系统托盘、自动更新、蓝奏云上传、自定义保留项、查重侧执行撤销、撤销历史。

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
8. 便携版与安装包里的 exe 没有内嵌版本资源（右键属性看不到版本号），版本号在安装包资源与界面“关于”里显示。

## 版本号

Go 版走自己的序列，当前 `0.8.0`：

- 上一版是 `0.7.0`，本版补齐整理器的存疑项逐条确认、最近一次整理撤销与规则导入导出，按语义化版本升次版本号；
- 同时补上 Linux / macOS 的回收站、机器码与单实例实现（交叉编译与 `go vet` 通过，未真机运行）；
- 仍是 `0.x`：整理器的撤销范围有限（删除与硬链接替换不可恢复），macOS / Linux 未做真机验证，不足以标 1.0；
- 刻意不用 C# 版的 `1.2.x`，避免两个实现共用同一版本号造成混淆（安装包、日志、问题反馈都以 `0.8.0` 为准）。
