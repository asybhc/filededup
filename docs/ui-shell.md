# 桌面 UI 外壳（Wails v2 + React/TS）

当前版本：`0.10.10`（Go 版独立版本序列，从 `0.6.0-ui` 起算；见文末“版本号”）。功能范围见“已接通 / 未实现”。

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
| `cmd/filededup/settings.go` | 设置持久化：读写用户配置目录、版本号与容错；AI/窗口/定时/日志段 |
| `cmd/filededup/runtime_settings.go` | 设置落到运行时：关闭行为拦截、托盘启停、定时任务重启 |
| `cmd/filededup/tray_windows.go` | Win32 托盘图标与右键菜单（打开 / 扫描 / 退出） |
| `cmd/filededup/tray_other.go` | 非 Windows 托盘占位（无托盘时关闭行为按退出处理） |
| `cmd/filededup/schedule.go` | 定时任务：按间隔或每日触发只读扫描，执行状态落盘 |
| `internal/secret/` | AI 密钥的 DPAPI 加密存储（非 Windows 返回不可用） |
| `internal/engine/search/` | 本地搜索：多根目录遍历、子串/通配符、扩展名与大小过滤、排序 |
| `web/src/Search.tsx` | 本地搜索页：服务端分页 + 虚拟列表按需取数 |
| `cmd/filededup/organize.go` | 整理器门面：两套顺序、九步规划、试运行/真实执行、存疑项确认 |
| `cmd/filededup/organize_undo.go` | 最近一次整理的撤销记录落盘与回滚接口 |
| `cmd/filededup/apply_undo.go` | 查重侧撤销：只记录成功的回收站删除，撤销时逐条恢复并保留失败项 |
| `cmd/filededup/export.go` | 查重结果导出：HTML/CSV，含分组、成员、大小、保留策略与扫描参数 |
| `internal/platform/restore*.go` | 回收站按原路径恢复（Windows 实现；其他平台明确不支持） |
| `cmd/filededup/organize_rules.go` | 整理规则导入导出（版本化 JSON、未知键与非法值校验） |
| `cmd/filededup/update.go` | 自动更新门面：检查、验签、下载、替换回滚、跳过版本 |
| `cmd/filededup/lanzou.go` | 双网盘门面：平台选择、登录态、目录浏览、上传预检、上传执行与报告 |
| `internal/cloud/lanzou/` | 蓝奏云老版协议：挑战登录、列目录、上传、去重规则、DPAPI 凭据、上传缓存、报告（优享复用其类型与报告） |
| `internal/cloud/lanzouyou/` | 蓝奏优享（ilanzou）协议：appToken 登录、列目录、七牛表单/分片上传、结果轮询、DPAPI 凭据 |
| `internal/update/` | 清单解析与验签、下载校验、替换与清理 |
| `cmd/fdedup-update-sign/` | 更新签名工具（私钥只在仓库外使用） |
| `cmd/filededup/main_desktop.go` | Wails 入口（`//go:build desktop`） |
| `cmd/filededup/license.go` | 授权门面：状态查询、机器码、导入许可证 |
| `cmd/filededup/app_smoke_test.go` | 后端接线冒烟测试（`-tags desktop`） |
| `internal/auth/` | 机器码、Ed25519/旧 ECDSA 验签、试用记录、授权状态 |
| `cmd/fdedup-keygen/` | 离线签发工具（密钥与私钥操作只在仓库外进行） |
| `web/src/App.tsx` | 模块切换与查重五步状态机 |
| `web/src/Organizer.tsx` | 九步整理页 |
| `web/src/Lanzou.tsx` | 双网盘上传页（平台选择、登录、浏览、预检、上传进度、报告） |
| `web/src/Settings.tsx` | 设置对话框（外观 / 窗口与托盘 / 定时任务 / AI / 日志） |
| `web/src/api.ts` | Wails 桥 / 浏览器 mock 分派 |
| `web/src/components/VirtualList.tsx` | 固定行高虚拟滚动 |
| `web/src/styles.css` | 中文排版与布局 |

## 流程

选目录 → 扫描（实时进度、可取消）→ 结果列表（组 / 成员 / 保留项 / 可回收）→
逐文件勾选要删除的副本 → 选删除方式 → 预览 / 执行（回收站，执行前确认）→ 结束统计。

后端全程走现有引擎，未改引擎契约：

```
scan.RunWithOptions → plan.FromScan → plan.Validate → apply.Executor{Trash: platform.Trash, Journal: logx.OpenOps}
```

引擎唯一改动：`scan.Options` 新增**可选**回调 `Progress func(scan.Progress)`，零值无开销。

执行前有两道闸：前端确认弹窗 + 后端 `confirmed=false` 直接拒绝；永久删除还要额外勾选确认。

## 结果页逐文件勾选（0.9.4）

- **勾选语义**：每个成员前的复选框 = **将删除**；未勾选 = 保留。首次载入结果时，默认勾选与旧版
  「整组删除」完全一致（即所有 `deletable=!keep` 的成员），因此不勾选任何东西直接预览，行为与 0.9.3 相同。
- **执行范围以勾选为准**：`PreviewPlan` / `StartExecute` 新增 `deletePaths []string` 参数。
  `nil` 表示沿用旧行为（删全部可删除成员，仅用于兼容）；界面始终传数组，空数组表示一个都不删。
- **每组的快捷操作**：全选（仅可删除成员）、全不选、反选、仅保留所选（把当前勾选项视为要保留的文件，
  其余成员改为待删除）。顶栏另有「全选可删除项 / 反选 / 全不选」；全局反选若导致某组整组被删，
  同样自动留出该组的保留项并提示调整了几组（0.9.5）。
- **安全约束**：
  - 前端在勾选会导致整组被删时拒绝该次勾选并提示「每组至少要保留一个文件」；
  - 反选后若整组都被勾选，自动留出策略保留项；
  - 后端 `plan.FromScan` 再次校验：某组成员全部出现在 `deletePaths` 返回 `ErrGroupFullySelected`；
    勾选路径不属于任何组返回 `ErrSelectionUnknown`（扫描结果过期时常见）；
  - 允许改留另一份：勾选策略保留项、不勾其他成员是合法的。
- **授权门控**：标准版仍可扫描、查看结果与勾选、生成预览；「确认执行」禁用并显示机器码，
  后端 `StartExecute` 也会拒绝并返回带机器码的原因，不是静默降级。
- **预览一致性**：预览页展示的「动作总数 / 涉及组 / 可回收」只统计勾选项；执行时重新按同一份勾选生成计划，
  不复用旧缓存，避免预览与执行范围不一致。

## 查重侧撤销（0.9.6）

- **记录范围**：真实执行（非试运行）中状态为成功的 `delete_trash` 写入
  `os.UserConfigDir()/FileDedup/dedup-undo.json`（Windows 即 `%AppData%\FileDedup\dedup-undo.json`），
  只保留最近一次“有回收站删除”的批次；只做永久删除的执行不会覆盖旧记录。
  记录含批次起止时间、删除方式、逐条路径 / 来源目录 / 大小 / 文件修改时间，跨重启可读。
- **永久删除不可撤销**：记录里逐条带 `unrestorable=true` 与原因「永久删除未进入回收站，无法撤销」，
  撤销结果单独计入 `unrestorable`，界面用文案说明，不尝试伪造恢复。
- **撤销语义**：`UndoLastDedup` 复用与整理侧同一把锁（`undoMu`），逐条：
  1. 目标位置已有同名文件 → 跳过并返回「原位置已有同名文件，未覆盖」，绝不覆盖；
  2. 回收站里按「文件名 + `System.Recycle.DeletedFrom` 目录」定位项，命中后调用项的 Restore 动词；
  3. 目标目录不存在时先建目录再还原；
  4. 逐条轮询确认文件确实回到原路径，未出现按失败上报。
- **回收站被清空**：脚本输出当前回收站项数，`binItems=0` 时结果带 `binEmpty=true`，界面明确提示
  「回收站已被清空，删除的文件无法自动恢复」，而不是笼统的“找不到”。
- **失败保留**：全部成功才删除记录文件；有失败时只保留失败项，便于修好阻塞（如先删掉同名文件）后重试。
- **平台差异**：`internal/platform/restore_windows.go` 实现真实恢复（Shell.Application + Namespace(10) + Restore 动词，
  结果写独立 UTF-8 结果文件并带 START/DONE 标记，区分“脚本没跑”和“逐条失败”；脚本文件带 UTF-8 BOM，
  否则 PowerShell 5.1 按 ANSI 解码中文会导致整脚本语法错误）。macOS / Linux 明确返回
  「当前平台不支持自动从回收站撤销，请在系统回收站中手动还原」，不假装成功。
- **界面**：完成页新增「撤销上次删除」卡片，显示可撤销条数、批次时间、每条来源目录与文件修改时间
  （明细最多 200 条，超出标注截断）、永久删除条数与原因、确认弹窗与逐条失败原因。

## 结果导出（0.9.7）

入口在结果页顶部：**导出 HTML** / **导出 CSV**。导出成功后界面显示文件绝对路径，并提供「在资源管理器中显示」。

- **内容**：扫描参数（根目录、保留策略、最小大小、是否含隐藏、相似图片/音频开关与阈值、排除项）、
  扫描开始/结束时间与耗时、汇总（重复组、组内成员、重复文件、可回收空间合计、扫描文件数/字节、跳过项），
  以及每个分组的结构（组号、类型、保留策略、本组可回收）和组内每个成员（文件名、完整路径、大小、修改时间、是否保留、是否可删除）。
- **HTML**：单文件、内联 CSS、UTF-8，可直接双击用浏览器打开或发给他人；保留项与待删除项用不同底色标记。
- **CSV**：UTF-8 **带 BOM**（Excel 双击打开中文不乱码），首列 `记录类型` 区分 `扫描信息` / `汇总` / `分组` / `文件`，
  每个文件一行的同时用 `编号` 关联所属组；大小一律为字节数，便于二次统计。
- **默认目录与文件名**：默认写到系统“文档”目录（`%USERPROFILE%\Documents`）；该目录不存在时退回用户主目录。
  文件名为 `dedup-report-<yyyyMMdd-HHmmss>.html` / `.csv`，同一秒重复导出时追加 `-2`、`-3` 避免覆盖。
- **不会被后续扫描当成待处理文件**：扫描枚举阶段把 `dedup-report-*.html` / `dedup-report-*.csv` 视为本工具产物直接跳过
  （与排除项判定在同一位置，大小写不敏感）。即使用户后来扫描“文档”目录，导出报告也不会进入查重结果或被删除。
  规则见 `internal/engine/scan/excludes.go` 的 `isToolReportName`。
- **失败处理**：未知会话、扫描未完成、格式不是 `html`/`csv`、目录不可写都会返回明确错误，界面显示“导出失败：…”，不写半截文件。

## 相似图片默认值与阈值（0.9.8）

- **默认关闭**：查重页「相似图片（dHash）」与整理页「图片去重」步骤默认都不勾选；只有用户显式打开才参与扫描。
  老配置里明确写过 `similarImage: true` 的仍保持开启，不会被新默认值覆盖。
- **阈值上限 16**（原 0..64，默认 8 不变）。依据：dHash 是 64 位指纹，互不相关的两张图汉明距离近似
  `Binomial(64, 0.5)`。随机两张图被判相似的概率：阈值 8 ≈ 2.8e-10、12 ≈ 2.3e-7、16 ≈ 3.9e-5、
  20 ≈ 1.8e-3、24 ≈ 3%、32 ≈ 55%。旧上限 32 时一半以上的无关图片会被判为“相似”，24 也有 3%；
  取 16（1/4 位）既允许比默认 8 更宽松，又把单对误判压到约四万分之一。
- **组内直径约束不变**：分组要求候选与组内**每个**成员都 ≤ 阈值，不是链式传递（A~B、B~C 不代表 A~C）。
  因此放宽到 16 也不会靠传递把整目录并成一组；新增 `TestGroupDiameterAtMaxThreshold` 固定该行为。
- **界面语义**：阈值输入框最大 16，下方常驻说明“数值越大判定越宽松，更容易把不同的图片当成相似（误判风险更高），
  数值越小越严格”，标签带同样的悬浮提示；相似图片未勾选时阈值输入禁用。
- **旧配置兼容**：
  - 合法的旧值（如开启 + 阈值 12）原样读入并继续生效；
  - 超出新上限的旧值（如 32）收敛为 16，并在设置提示里显示“超出新上限 16，已收敛为 16”，不静默截断；
  - 负数阈值回退默认 8 并提示；
  - 存储 schema v1 的 `maximum: 64` 保持不变，避免历史扫描结果文件无法回读；上限校验只作用于新请求与 UI。
- **定时任务**：参数映射继续读取 `scan.similarImage` / `scan.imageThreshold`；关闭时不再自动开启图片查重，
  阈值越界时按 8 回退（正常情况下设置读取阶段已收敛到 16 以内）。

## 整理目标在源目录之外与文案纠偏（0.9.9）

- **目标可填绝对路径**：整理页「整理目标目录」留空表示根目录；相对路径仍限制在根目录内；
  绝对路径若落在根目录内会转成相对路径（旧行为），落在根目录外则按外部目标执行。
- **规划期虚拟目标**：外部目标在规划层用虚拟目录名（`organize.ExternalTargetAlias`，含控制字符、不可能与真实目录重名）
  生成根内相对动作，规划后由 `organize.RewriteExternalTarget` 把 `To` 改写为真实绝对路径；
  `From` 始终是根内相对路径，`target` 子树不参与改名/去重/空目录清理。
- **同卷移动 / 跨卷复制**：执行层对每条 move 调 `SameVolume`；同卷走 `os.Rename`（源文件离开原位置），
  跨卷退化为复制并**保留源文件**。复制写入目标目录内的临时文件，校验大小后再原子重命名；
  磁盘满、权限失败、复制不完整都会报错并删除临时文件，不留半截文件。
- **界面提前说明**：结果/预览区在 `targetExternal=true` 时显示横幅，按 `moveMode` 提示
  「跨卷：将复制文件，原文件仍保留」「同卷：将重命名式移动」或「无法判断是否同卷」；
  动作样例的 move 徽标显示“移动 / 复制 / 移动/复制”。
- **冲突语义**：规划期不枚举外部目标已有文件；执行到该条时若目标已存在会如实报错，
  默认 `continueOnError=false` 会停止后续动作，不静默覆盖、不删除源文件。
- **规则导入**：`target` 允许相对路径或绝对路径；相对路径仍拒绝 `.`/`..` 逃逸。
- **文案纠偏（行为未改）**：
  - 查重页：`排除子串（逗号或空格分隔）` → `排除目录（目录名或路径前缀；逗号或空格分隔）`，
    并补“只匹配目录，不按文件名子串过滤”的说明；`包含隐藏/系统文件` →
    `包含隐藏/系统文件（不勾选 = 跳过隐藏与系统文件）`。
  - 整理页：目标标签改为 `整理目标目录（留空 = 根目录；可填绝对路径放在源目录之外）`；
    `排除子串` → `排除目录（按目录名匹配；逗号或空格分隔）`，并补“只对目录生效”的说明；
    隐藏项同样补“不勾选 = 跳过”。
  - 搜索页：`包含隐藏 / 系统文件` → `包含隐藏 / 系统文件（不勾选 = 跳过）`。
  - CLI：`--include-hidden` 帮助补“默认跳过”；`--target` 帮助改为“root 内相对路径，或 root 外绝对路径”；
    扫描帮助里的阈值范围由过时的 `0-64` 更正为 `0-16`。
  - 说明：代码里复选框一直是“勾选 = 包含、不勾 = 跳过”，本次只改文案，不反转行为。

## 设置持久化

- 位置：`os.UserConfigDir()/FileDedup/ui-settings.json`（Windows 即 `%AppData%\FileDedup\ui-settings.json`），界面会显示真实路径。
- 内容：`{ schemaVersion, scan, organize, update, lanzou }`，覆盖查重页、整理页与蓝奏上传页的全部选项（阈值、删除方式、保留策略、排除目录、九步开关等）。
- 容错约定：
  - 缺键按默认值补齐（先填默认值，再用文件里存在的键覆盖）；
  - **多出来的键不判损坏**：读取时忽略，保存时原样保留；
  - 枚举/范围越界拉回默认值；相似图片阈值超出新上限时收敛到 16 并在提示里说明（见“相似图片默认值与阈值”）；
  - 只有整份文件不是合法 JSON 才回退默认并在界面上给出提示，下次保存会覆盖。
- 写入方式：临时文件 + `Rename` 原子替换，避免半截文件。
- 前端：启动时 `GetSettings` 恢复；扫描选项变更后 600ms 防抖写盘，整理页 500ms 防抖，输入过程不反复落盘。
- `0.9.0` 起新增 `ai` / `window` / `schedule` / `logs` 四段，仍遵守同一套兼容策略：老文件没有这些键就补默认值，
  新键之外的旧键（含未来版本写入的未知键）原样保留，只有整份不是合法 JSON 才回退并提示。

## 设置面板与托盘

设置对话框（`web/src/Settings.tsx`）分五段，保存时先写密钥再写设置，避免“设置已开但密钥没存”：

| 段 | 字段 | 说明 |
| --- | --- | --- |
| 外观 | `window.theme` | 跟随系统 / 日间 / 夜间 |
| 窗口与托盘 | `window.closeAction`、`window.trayEnabled` | 关闭窗口时退出或最小化到托盘；托盘图标开关 |
| 定时任务 | `schedule.*` | 见下节 |
| AI 配置 | `ai.enabled/endpoint/model/timeoutSeconds` + 密钥 | 密钥单独 DPAPI 存储，界面只显示 `****后四位` |
| 运行日志 | `logs.retentionDays` | 保留天数（默认 14，范围 1–365） |

- **AI 密钥**：`internal/secret` 用 Windows DPAPI（`CryptProtectData`，当前用户范围）加密写到
  `%AppData%\FileDedup\ai\key.bin`；目录 `0700`、临时文件 `0600` 后 `Rename` 替换。设置文件里**不出现**密钥，
  只有读取时按密钥文件算出的 `keyHint`（仅后四位）。`SaveAIKey("")` 等价于清除。非 Windows 平台
  `SupportsProtection()` 返回 false，界面显示“当前平台不支持加密存储”，不落明文。
- **关闭行为**：Wails `OnBeforeClose` 里判断；`closeAction=tray` 且托盘可用时隐藏窗口并阻止关闭，否则正常退出。
  托盘不可用（非 Windows / 创建失败）时自动降级为退出，不设置“关闭后无窗口也无托盘”的死角。
- **托盘右键菜单**：打开主界面 / 扫描（打开窗口并开始扫描）/ 退出。双击托盘图标等同“打开”。
  实现是隐藏窗口 + 独立 OS 线程消息循环（`runtime.LockOSThread`），图标用系统 `IDI_APPLICATION`，不额外打包资源。
  “扫描”不在后端复制扫描逻辑，而是 `EventsEmit("tray:scan")`，由前端用当前界面参数触发同一套扫描。
- **应用方式**：`App.SaveSettings` 保存成功后调用 `applyRuntimeSettings`，立即启停托盘并重启定时任务；
  启动时也会读一次设置并应用，不必等用户打开设置面板。

## 主题（日夜切换）

- 颜色全部走 CSS 变量：`--bg/--card/--soft/--border/--text/--muted/--accent/--danger/--warn/--ok` 及各自的
  `*-weak` 弱底。深色是 `:root[data-theme="dark"]` 的整套覆盖，组件不写死颜色。
- `theme=system` 时用 `matchMedia("(prefers-color-scheme: dark)")` 并监听 `change`，系统切换立即跟随；
  `light` / `dark` 时移除监听。
- 持久化分两处：设置文件是唯一事实来源；`localStorage["filededup.theme"]` 只是镜像，供 [index.html](file:///f:/我的软件项目/文件去重/2026-11-13/go-port/filededup/web/index.html)
  的内联脚本在 React 挂载前先定 `data-theme`，避免刷新闪一下浅色。localStorage 不可写（隐私模式）时忽略。

## 定时任务（只读扫描）

- 触发方式二选一：按间隔（10 分钟–7 天）或每日固定 `HH:MM`；可启停。保存后立即重启调度循环，无需重启应用。
- **只做只读扫描**：动作字段目前强制 `scan`，不提供自动删除/整理。取舍：定时任务无人值守，删除/整理一旦参数被误改
  不可挽回；扫描是只读的，最坏结果只是占一点 CPU。要自动整理需要先有“变更前快照 + 一键回滚”的完整闭环。
- 根目录：任务可单独指定绝对路径；留空则用查重页保存的根目录。两者都为空时本轮记 `skipped`，不报错。
- 参数映射：直接用设置里的保留策略、最小大小、是否含隐藏、相似图片/音频阈值与排除目录，`Exact=true`。
- 上次执行结果落在 `%AppData%\FileDedup\schedule-state.json`（临时文件 + `Rename`），字段：
  `startedAt/finishedAt/status/message/root/files/groups/reclaimable/elapsedMs`，`status` 为 `ok/failed/cancelled/skipped`。
  设置面板读 `GetScheduleState` 展示；执行完成后也会 `EventsEmit("schedule:done")` 刷新界面。
- 关闭窗口到托盘时调度继续跑；彻底退出应用后不再有后台进程。

## 本地搜索

入口是顶部「本地搜索」页，走标准版功能位 `search`；未解锁时页面可见，点「开始搜索」返回带版本名与机器码的明确原因。

- **条件**：可添加多个根目录（绝对路径，重复项按大小写不敏感去重）；关键词按「文件名或相对路径」匹配。
  子串模式忽略大小写；通配符模式用 Go `path.Match` 语义（`*` 不跨 `/`），不带 `* ?` 时自动按子串处理，
  含 `/` 时匹配相对路径、否则匹配文件名。扩展名支持 `jpg,png` / `.jpg .png` 等写法；大小按字节区间，
  最大值为 0 表示不限；排除项与查重页相同（纯名称按目录组件匹配，带分隔符按路径前缀）。
- **索引不落盘**：每次搜索实时遍历。理由：落盘索引需要文件系统监视或定期重建，过期后会“搜不到旧文件、
  也搜不到新文件”，比慢一点更难解释；实时遍历配合取消与结果上限已能满足本地盘规模。
- **上限与截断**：默认 20 万条、后端硬上限 100 万条。达到上限立即停止遍历并标记“结果被截断”，
  界面显示警示；收紧条件重搜即可。
- **取消**：`context` 贯穿遍历，每个目录项检查一次；取消后返回已完成的部分结果，界面标注“已取消”。
- **不跟随符号链接**：避免目录环与跨盘遍历，跳过项记入结果（最多保留 200 条原因）。
- **渲染**：`GetSearchResults` 按 `offset/limit` 分页（每页 200 条），前端 `VirtualList` 只渲染可视窗口，
  `onRangeChange` 触发按页取数；数组长度等于命中总数但只有窗口附近的行有数据。
  排序在服务端做（`name/path/size/modTime`，再点同列切换升降序），切换排序只重排一次并清空已取页。
- **定位/打开**：`RevealPath` 用资源管理器选中文件（macOS Finder / Linux 文件管理器等价），`OpenPath` 用默认程序打开。
- 扫描与搜索互不影响：搜索有独立任务表，最多保留 8 个任务，超出时清理已完成的旧任务。

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
  导入用 `DisallowUnknownFields` 拒绝未知键，并校验版本、枚举、阈值范围、正则与 target 路径
  （根内相对路径或根外绝对路径）；失败不落盘、不改当前设置，
  成功导入后仍沿用当前根目录。
- 轮询：`GetOrganizeState` 400ms 一次，返回阶段（枚举 / 规划 / 执行）、每步动作数与计数、存疑项、前 200 条动作样例、执行统计与失败明细。

## 蓝奏云 / 蓝奏优享双网盘上传

页面顶部可选「蓝奏云（老版协议）」与「蓝奏优享（ilanzou）」。平台选择只记在浏览器 `localStorage`
（键 `filededup.lanzou.platform`），不写入 `ui-settings.json`：旧设置文件不需要新增键，也不会因为回退版本丢失。
两种网盘各自独立登录、各自保存凭据与上传缓存（缓存键带平台前缀），切换网盘不会退出另一种的登录态。
网盘查重（三期）与分享创建仍不在本功能内。

**升级行为（老用户会看到什么）**：首次进入仍默认「蓝奏云（老版协议）」，原 `lanzou.bin` 凭据与
`upload-cache.json` 缓存路径、键格式（除新增平台前缀）继续可用，无需重新登录。新增的优享凭据单独存
`youxiang.bin`，不会覆盖老版凭据。开始上传的授权门控从「执行删除（apply）」改为「网盘上传（cloud）」：
标准版此前能走到开始上传的路径，现在会被后端拒绝并给出带机器码的原因，登录与预检仍可用。

### 蓝奏云（老版协议）

与 C# 版对等：`pc.woozooo.com` / `accounts.woozooo.com`。以下联调结论均来自 0.8.2。

**联调状态**：登录、列目录、预检与**真实上传**都已用真实账号联调通过（0.8.2）。上传联调在账号里建
`test-<日期>-<时间>` 临时目录（本次 `test-20261115-185649`，目录 ID `14253921`），只在该目录内操作，
跑完删除文件与目录，并核对云端条目数回到操作前：根目录 **19 个文件夹 / 18 个文件 → 19 / 18**，目标目录已不存在。

| 分支 | 状态 | 实测结果 |
| --- | --- | --- |
| 登录（C# 凭据导入 + 登录态校验） | 实测通过 | 账号掩码 `****2661`，列目录正常返回 |
| 列根目录 / 子目录 | 实测通过 | 根目录 19 文件夹 / 18 文件；新建的子目录可列出 |
| 只读预检分类 | 实测通过 | 预检给出可上传 / 云端重复 / 超大跳过 / 缓存跳过，未动本地文件 |
| 正常上传（3 × 8 MiB，并发默认 3、间隔 1500ms） | 实测通过 | `success:3`，耗时 5.823s，同时进行最多 3 个，无 429、无重试 |
| 云端重复 + 本地移入「网盘重复文件」 | 实测通过 | `重复 3 / 超大 1`，3 个文件移动到 `…\网盘重复文件\`，云端仍为 3 个 |
| >100 MiB 跳过 | 实测通过 | 未发请求，汇总 `SkippedLarge:1` |
| 缓存跳过（同目录二次预检） | 实测通过 | `可上传 0，缓存 3`，上传结果 `cached:3` |
| 失败重试（首次读取失败后重新打开） | 实测通过（注入式） | 第 1 次尝试失败、第 2 次成功，5.923s 后 `success:1` |
| 上传中途取消（6 × 4 MiB） | 实测通过 | `success:1 canceled:5`；云端最终 5 个文件，与“已完成 + 取消前完成”一致 |
| 报告落盘 | 实测通过 | `upload-report-*.json/.csv` 真实批次落盘，CSV 带 UTF-8 BOM，状态文案正确 |
| 秒传 | **未覆盖** | 服务端全程没有返回秒传，按普通上传处理 |
| 账号密码登录 | **未覆盖** | 联调用的是 C# 凭据导入，未走账号密码 + `acw_sc__v2` 挑战 |
| 服务端 429 限流 | **未覆盖** | 默认并发 3 / 1500ms 下未触发，重试退避路径未在真实链路上出现 |
| 优享版、分享创建 | 不在本期 | — |

**联调复盘**（4 个真实问题 + 1 个时序问题，都已修复，改这块代码时别再踩）：

1. **上传文件部件名必须是 `upload_file`**。写成 `file` 时服务端返回 `{"zt":0,"info":"请选择需要上传的文件"}`，
   看起来像“没选文件”，实际是字段名不对。
2. **上传目标目录字段是 `folder_id`**。写成 `folder_id_bb_n` 服务端不报错，但文件会静默落到根目录——
   这是最容易“以为成功”的坑，联调时必须回读子目录确认。
3. **空子目录 `task=47` 返回 `zt=2`**，`info` 里带的是目录自身、`text` 为空数组。早期实现把 `zt=2` 一律当失败/登录失效；
   现在 `isEmptyFolderResponse` 只在这个形状下按空目录处理，其他 `zt=2` 仍按原文报错。
4. **汇总会重复计数**：预检阶段已经统计过“云端重复/超大/缓存”，上传阶段处理重复项时又计入一次，
   出现 `Total:7 Duplicate:6` 但明细只有 4 条。现在结束后按最终 `items` 重建 `Summary`，不再逐段累加。
   同一类问题还有“报告文件被下一批预检当成待上传文件”，已用 `isUploadReportName` 跳过 `upload-report-*.json/.csv`。
5. **时序：`Phase` 先于报告路径置为 `done`**。前端一看到 `done` 就立刻读状态，会拿到空的 `ReportJSON`/`ReportCSV`。
   现在先写报告、再写 `ReportJSON`/`ReportCSV`、最后置 `done`；报告写失败时 `Error` 有值但 `done` 照常给出。
   联调脚本也踩过同类时序问题：重试分支的注入必须在预检完成后、`LanzouStartUpload` 之前启动，否则预检会读到被改名的文件。

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
- **两道闸**：预检只读（不动本地文件）；开始上传要求后端 `confirmed=true`、设置里 `disclaimerAccepted=true`，并通过 `FeatureCloud` 授权门控
  （0.9.3 起从 `FeatureApply` 改为 `FeatureCloud`，与「专业版：网盘上传」的功能划分一致；标准版开始上传会被拒绝并给出带机器码的原因，登录与预检仍可用）。
- **错误透传**：`APIError` 原样带出操作名、HTTP 状态码、服务端 `zt/info` 或 `code/msg` 与响应片段，不包装成「网络错误」，不假装成功。
- **结果分类**（沿用 C# 七种 + 汇总行）：上传成功、秒传、缓存跳过、超过 100 MiB 跳过、云端重复（本地已移动）、已取消、失败。
- **报告**：结束后在上传目录写 `upload-report-<yyyyMMdd-HHmmss>.json` 与同名 `.csv`（UTF-8 BOM，带引号转义），
  界面提供「打开报告」与「打开网页端」。分享创建不在本功能内，需要分享请到网页端手动操作。

### 蓝奏优享（ilanzou）

协议字段来自对官方 Windows 客户端的静态分析（C# 反编译 `c/m.cs`、`B/F.cs` 与字符串表 XOR 解密），
**没有真实优享账号做过联调**；只保证编译、包内 `httptest` 单测与本地可验证部分。

- **凭据**：账号密码只用于登录请求，换取 `appToken`；落盘 `%AppData%\FileDedup\lanzou\youxiang.bin`，
  与老版 `lanzou.bin` 分开，DPAPI（CurrentUser）加密，界面只显示账号后四位。优享用 appToken 而非 Cookie，
  因此「粘贴 Cookie 登录」与「从 C# 版导入」在优享平台下不显示（后端也会明确拒绝，不会静默失败）。
- **协议（已按字段实现）**：
  - 基地址 `https://api.ilanzou.com`；每个 API 追加公共查询
    `uuid / devType=6 / devCode / devModel=chrome / devVersion=131 / appVersion / timestamp / extra=2`，登录后追加 `appToken`。
  - 登录 `POST /unproved/login`，body `{"loginName","loginPwd"}` → `data.appToken`；
    账号信息 `GET /proved/user/account/map` → `data.map.userId/account`（同时用于校验登录态）。
  - 目录 `GET /proved/record/folder/list?offset=<页码≥1>&limit=60&folderId=<id>&type=0`，文件同形但用 `file/list`；
    读 `data.totalPage` 翻页，`fileType==2` 按目录处理，文件 `fileSize` 单位是 KB，展示时 ×1024。
  - 上传令牌 `POST /proved/7n/getUpToken`，body `{"fileName","fileSize":<KB>,"folderId","md5","type":1}` → `data.upToken`；
    `upToken=="-1"` 表示秒传，`data.map.fileId` 即既有文件 ID。
  - ≤ 8 MiB 走七牛表单 `POST https://upload.qiniup.com/`（字段 `token`、`key`、`file`）；
    > 8 MiB 走七牛分片：初始化 `/buckets/wpanstore-lanzou/objects/<urlsafe-base64(objectKey)>/uploads`，
    分片 `PUT .../uploads/<uploadId>/<n>` 带 `Content-MD5`，合并 `POST .../uploads/<uploadId>`（JSON `parts`）。
  - 结果 `GET /unproved/7n/results?tokenList=<token>&tokenTime=<yyyy-MM-dd HH:mm:ss>` 轮询，`status==1` 视为成功。
  - objectKey：`storage/files/<年>/<月>/<日>/<账号或 0>/<unixMilli>_<guid32>.gz`。
- **行为**：登录后列目录、切换目录、只读预检（本机缓存 + 云端同名同大小判定）、上传、取消、结束写报告。
  优享不设 100 MiB 本地跳过；**不支持创建云端目录**，需要新目录请先在网页端创建。
  报告 kind 为 `filededup.lanzouyou.upload-report`，平台字段 `lanzou-youxiang`。
- **未验证项（不作结论）**：
  1. 分片上传的分片 URL 是否真的包含 `uploadId`：C# 反编译片段里没有，本实现按七牛公开协议补上，需真机确认。
  2. objectKey 的 base64 用 URL-safe 变体（C# 片段为标准 base64），以服务端接受为准。
  3. 优享对文件名/扩展名、单文件体积、并发与请求频率的限制未实测；目前沿用并发 1–5、间隔 1000–2000ms。
  4. 秒传的 `data.map.fileId` 与结果轮询 `data.list[].status` 只做了合成响应单测，未接真实账号。
  5. 账号容量 / VIP 字段只用于校验登录态与展示，不参与上传判定。

## 授权与试用

细节（许可证格式、签发流程、机器码算法、试用加固）见 [license-and-trial.md](license-and-trial.md)。界面侧约定：

- 顶栏授权徽标始终可见（已授权 / 试用中 N 天 / 试用已到期 / 未授权），点击打开授权面板。
- 授权面板显示：状态、许可证来源（Ed25519 / 旧 ECDSA 只读兼容）、**版本名**（标准版 / 专业版 / 试用版）、
  到期日（或永久）、本机机器码（可一键复制）、许可证文件路径、**功能解锁列表**、试用起止与剩余天数、
  以及未授权时的限制说明。
- **版本与功能集**（`internal/auth`）：
  - 标准版：重复文件扫描、删除执行、九步整理、本地搜索；
  - 专业版：标准版全部 + 网盘上传、网盘查重、AI 分析建议、定时任务；
  - 旧许可证缺 `edition` 按标准版处理，缺 `features` 按版本默认集补齐，两种情况都不会失效；
  - 功能列表由 `featureDefs` 单一来源生成，界面按同一顺序展示：已解锁标绿，未解锁标红并写明“需要专业版许可证”。
- 导入方式两种：文件选择框（`*.lic;*.fdlic;*.txt`，上限 64 KB）与直接粘贴文本。导入前先验签，验签失败不会落盘。
- **未授权不静默退化**：`apply`（执行删除）与 `organize`（整理真实执行）在后端被拒绝并返回中文原因
  （含功能名、所需版本与机器码），前端同时禁用按钮并显示红色横幅；扫描与试运行始终可用。
  后续接入的网盘上传、AI、定时任务同样走 `RequireFeature`，错误文案由同一份功能表生成。
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

- 入口：顶栏“检查更新”；有新版时按钮显示“有更新 vX”。更新源默认走内置仓库最新 release，
  `manifestUrl` 留空即自动（手动填写作为覆盖项，见 0.10.15）。
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
  结果页逐文件勾选与查重侧撤销（**撤销仅 Windows；真实往返测试见“未验证项”**）、
  查重结果导出（HTML / CSV，默认写“文档”目录，导出文件不入后续扫描）、
  九步整理（两套顺序 / 步骤开关 / 试运行 / 真实执行 / 冲突与保留策略 / 结果明细 / 仅规划 / 存疑项逐条确认或驳回 /
  最近一次整理撤销 / 规则导入导出）、双网盘上传（蓝奏云老版：登录 / Cookie 降级 / C# 凭据导入 / 目录浏览 / 只读预检 /
  并发上传与取消 / 云端同名同大小去重 / 七类结果与汇总 / 报告导出，**登录、预检与上传均已用真实账号实测**；
  蓝奏优享：appToken 登录 / 列目录 / 预检 / 七牛表单与分片上传 / 结果轮询 / 报告，**未用真实账号联调**）、
  自动更新（ed25519 清单与安装包验签、长度与 SHA-256 校验、备份与回滚、跳过版本、安装包缓存清理）、
  exe 与安装包版本资源、授权与试用（Ed25519 导入与状态、旧版只读兼容、机器码、试用加固、未授权门控、
  标准版 / 专业版功能集与未解锁说明）、设置面板（AI 配置与 DPAPI 密钥、关闭行为、托盘、定时任务、日志保留天数）、
  系统托盘（打开 / 扫描 / 退出）、日夜主题（跟随系统 / 日间 / 夜间）、本地搜索（多根目录、子串/通配符、
  扩展名与大小过滤、服务端排序、分页 + 虚拟渲染、定位与打开、可取消）。
- 未实现：
  - 全局：自定义保留项、多次撤销历史（只保留最近一次）、定时任务执行删除或整理（当前只读扫描）。
  - 蓝奏云：网盘查重（三期，原版默认只列不删、删除需二次确认）、优享创建云端目录、分享创建（C# 版没有、服务端接口不可用）。
  - AI 分析：设置项与密钥存储已就绪，分析调用在 `0.9.x` 后续块接入。

## 已知限制

1. 桌面窗口在自动化会话里不做人工交互验证，桌面显示由用户复核；已确认 production 构建的进程能稳定存活并创建标题为
   “文件去重 FileDedup”的窗口（此前“环境承载不了窗口、3 秒退出”的结论来自缺少 `production` tag 的 stub，已作废）。
   后端用 `go test -tags desktop -run "TestAppSmoke|TestSettingsTolerance|TestOrganizeJob" ./cmd/filededup` 覆盖真实目录的扫描→预览→执行闭环、设置容错与整理执行。
2. 结果列表一次性载入全部组（分页 500 逐步拉取），超大结果集的内存占用未单独测量。
3. 预览只展示前 200 条动作样例，完整计划在执行时才交给执行器。
4. 整理器动作样例回传前 200 条；存疑项（`Suspects`）单独结构化回传，逐条确认后才会进入删除动作，未确认项保留在磁盘上。
5. 整理后撤销与查重侧撤销都只保留最近一次执行；整理侧只能回滚 rename / move / ensure_dir / clear_readonly，
   删除与硬链接替换记为“不可恢复”，统计里单列，不回滚。查重侧只撤销“移入回收站”的删除，
   永久删除逐条标为不可撤销；其回收站恢复目前只有 Windows 实现（Shell.Application + Restore 动词），
   macOS / Linux 明确返回“不支持自动撤销”，需用户在系统回收站手动还原。
6. Linux 与 macOS 的回收站（XDG Trash / `~/.Trash` / `.Trashes/$uid`）、机器码采集与单实例锁已实现，
   **只做了交叉编译与 `go vet` 验证，没有在真机运行过**；Windows 端仍是唯一经过真机验证的平台。
7. 旧 ECDSA 许可证只做了算法与格式层面的单元测试（临时密钥 + 合成 payload/P1363 签名），**没有用真实签发的旧许可证样本验证**：
   历史私钥不在手边，无法生成真样本。
8. exe 与安装包均内嵌版本资源（FileVersion/ProductVersion 为四段，如 `0.8.2.0`；界面显示三段 `0.8.2`）。
   版本资源在构建时由 `app.go` 的 `appVersion` 生成，`.syso` 不提交。
9. 更新签名私钥保存在仓库外（`%USERPROFILE%\.tools\filededup-update-key\update.key`），客户端只内置公钥；
   私钥一旦泄露需要换钥并重新发布客户端，内置公钥不支持远程轮换。
10. 自动更新的“下载→验签→替换运行中的 exe→重启”已在真实安装目录（`%LOCALAPPDATA%\Programs\FileDedupGo`）实测通过，
    含启动失败回滚；**界面“下载并安装”按钮本身未人工点击**，闭环用命令行驱动同一套 `internal/update` 代码。
11. macOS / Linux 的自更新按单文件可执行程序替换实现，未处理 `.app` 包、代码签名与权限提升，**未真机验证**。
12. 蓝奏云上传一期已用真实账号做过**上传联调**（临时 `test-` 目录，跑完删文件删目录并核对根目录回到 19/18）：
    正常上传、云端重复+本地移动、>100 MiB 跳过、缓存跳过、失败重试、中途取消、报告落盘都实测通过。
    **仍未覆盖**：秒传（服务端没有返回秒传）、账号密码登录、限流 429、分享创建。
    协议实测修正：根目录 `folder_id` 必须传 `-1`（传 `0` 返回 `{"zt":2}`），文件夹 ID 字段是 `fol_id`；
    上传目录字段是 `folder_id`、文件部件必须叫 `upload_file`；子目录为空时 `task=47` 返回 `zt=2` 但带目录自身 info；
    文件列表 `page` 会被服务端忽略、`info` 是总页数，已按此停止翻页。
    本机缓存键不含 MD5（与 C# 版不同），内容变但大小/修改时间不变会误判为已传，可用「强制重传」绕过。
13. 设置面板、托盘图标与右键菜单、关闭到托盘、日夜主题：后端逻辑与前端构建都通过编译、`go vet` 与 `go test -tags desktop`，
    **托盘图标显示、菜单点击、双击还原、关闭拦截与深色配色未做人工点击验证**（自动化会话不驱动桌面交互），需人工复核。
14. 定时任务：调度循环、参数映射与状态落盘有代码路径覆盖，但**没有等待一个真实周期到点触发**；
    “间隔 / 每日”两种模式的实际触发时间点、托盘隐藏期间后台运行未真机验证。任务只做只读扫描，不做删除或整理。
15. AI 密钥 DPAPI 加密只验证了 `go vet` / 编译与 `SupportsProtection` 分支，**没有在真机上写入并重启读回**；
    非 Windows 平台明确不支持加密存储，界面会提示，不会退回明文。
16. 深色主题的对比度按 CSS 变量成对设计（浅底深字 / 深底浅字），**没有做对比度工具测量**，也未覆盖所有第三方内联样式。
17. 本地搜索：引擎单测覆盖子串、通配符、扩展名/大小过滤、排除目录、上限截断、取消、排序与多根目录；
    **未在数十万文件的真实大目录上测过耗时与内存**，也未人工验证滚动到底部的按页加载、定位/打开按钮在真实
    Explorer 中的表现。搜索索引不落盘是刻意取舍，见「本地搜索」章节。
18. 蓝奏优享：登录、账号、目录、表单/分片上传、秒传与结果轮询只有 `httptest` 合成响应单测，
    **没有真实优享账号联调**；分片 URL 是否含 `uploadId`、objectKey 用 URL-safe base64、服务端限制与 429 行为
    都未验证，详见「蓝奏优享（ilanzou）」的未验证项。界面平台切换、优享下隐藏 Cookie/导入入口也未做人工点击验证。
19. 逐文件勾选：`plan` 与 `App` 冒烟测试覆盖了显式勾选、整组勾选拒绝、未知路径拒绝与按勾选执行；
    但结果页的复选框、每组快捷按钮、顶栏「反选」、整组保护提示只做了 `npm run build` 类型检查，
    **没有在真实桌面窗口里逐项点击验证**。
20. 查重侧撤销：Windows 真实往返测试已通过（`TestRestoreFromTrashRoundTrip`：真实移入回收站 →
    删掉目标目录 → 按原路径恢复；`TestDedupUndoRoundTrip`：扫描 → 按计划删除 → 读记录 → 撤销 → 内容一致）。
    **未验证**：回收站被清空（`binEmpty`）、原位置已有同名文件不覆盖（仅有单测 `TestRestoreFromTrashNoOverwrite`，
    未在界面点击）、撤销确认弹窗与撤销卡片的桌面交互、macOS / Linux 分支（明确不支持）、
    多用户 / 其他 SID 回收站、OneDrive 等重定向目录下的 `DeletedFrom` 匹配。
21. 结果导出：`TestExportResultsHTMLAndCSV` 覆盖 HTML/CSV 两种格式的必需字段、UTF-8 BOM、非法格式与未知会话；
    `TestScanSkipsToolReports` 覆盖报告文件不被再次扫描。**未验证**：结果页「导出 HTML / CSV」按钮与
    「在资源管理器中显示」的桌面点击、默认“文档”目录在不同 Windows 语言版本 / OneDrive 重定向下的实际路径、
    macOS / Linux 上的主目录回退、超大结果（数万组）导出耗时与 Excel 打开体验。
22. 相似图片默认值与阈值：`TestSettingsTolerance` 覆盖默认关闭、旧配置 `true + 12` 原样读入、缺键保持关闭、
    越界 32 收敛到 16 并给出提示；`TestGroupDiameterAtMaxThreshold` 覆盖上限 16 时的组内直径约束；
    `validate_test` 覆盖上限+1 被拒绝。**未验证**：查重页/整理页阈值输入框与说明文字的桌面点击与排版、
    真实图片集在阈值 12/16 下的误判率（概率推导基于独立随机位假设，实际 dHash 位可能相关）、
    旧版本升级后首次启动的提示条实际展示。
23. 整理目标可在源目录之外：`TestResolveTarget` / `TestRewriteExternalTarget` 覆盖根内相对、根内绝对、
    根目录本身、根外绝对与相对逃逸；`TestApplyCrossVolumeCopies` / `...CopyFailureLeavesNoPartial` /
    `...CopySizeMismatch` / `TestApplyAbsoluteDestination` / `TestApplySameVolumeError` 用注入 `SameVolume` 与
    `CopyFile` 覆盖跨卷复制、失败清理与同卷移动；`TestStartOrganizeExternalTargetPlan` 覆盖 UI 侧外部目标预览，
    `TestRunOrganizeExternalTargetDryRun` 覆盖 CLI 外部目标。**未验证**：真实跨卷盘符（如 C:→D:）的 exe 手测
    （本机自动化只有注入钩子，没有真实跨卷环境）；外部目标新建目录与复制结果在真实磁盘上的手工核对；
    界面“目标在源目录之外”横幅、move 徽标“复制/移动/复制或移动”的桌面点击与排版；
    外部目标已有同名文件时的真实冲突报错文案展示。

## 版本号

Go 版走自己的序列，当前 `0.10.10`：

- `0.7.0` 补齐整理器三项（存疑项逐条确认、最近一次整理撤销、规则导入导出）；
- `0.8.0` 加自动更新与 exe/安装包版本资源，并按语义化版本升次版本号；
- `0.8.1` 是补丁版：取消按钮本地即时置为 `cancelling`（不等 300 ms 轮询）、更新包缓存清理
  （保留最近 2 个包，删除 `.part`/`.new`/`.old`），并补上真实安装目录的更新闭环与 8 万文件扫描实测数据；
- `0.8.2` 带蓝奏云真实上传联调的修复：上传字段（`folder_id` + `upload_file`）、空子目录 `zt=2` 识别、
  汇总重复计数、报告文件被当待上传、`done` 早于报告路径；含只读联调的根目录 `folder_id=-1` 与分页修正；
- `0.9.0` 授权分级：标准版 / 专业版两个版本名与默认功能集，未解锁功能在界面上可见并给出原因与机器码；
  旧许可证缺 `edition` 时按标准版处理且不失效；
- `0.9.1` 设置面板与托盘、日夜主题、定时任务（只读扫描）：新增 `ai/window/schedule/logs` 四段设置，
  AI 密钥 DPAPI 存储（仅显后四位）、关闭到托盘、Win32 托盘菜单、跟随系统的深浅色；
- `0.9.2` 本地搜索：多根目录、子串/通配符、扩展名与大小过滤、服务端排序、分页 + 虚拟渲染、定位打开、取消；
- `0.9.3` 双网盘：新增蓝奏优享（ilanzou）登录、列目录、预检、七牛表单/分片上传与报告，平台选择与独立凭据/缓存；
  开始上传的授权门控从 `FeatureApply` 改为 `FeatureCloud`；优享未接真实账号，存疑项列入「未验证项」；
- `0.9.4` 结果页逐文件勾选：成员级复选框、全选/全不选/反选/仅保留所选，预览与执行按勾选范围，
  整组删除前后端双重拒绝；未授权仍可勾选与预览，执行门控带机器码；
- `0.9.5` 补全结果页顶栏的「反选」：与每组反选同一规则，整组会被勾满时自动留出保留项并提示；
- `0.9.6` 查重侧撤销：删除动作写入 `dedup-undo.json`，完成页可撤销最近一次“移入回收站”的删除；
  Windows 走 Shell.Application Restore 动词（结果文件 + START/DONE 标记，脚本带 UTF-8 BOM），
  永久删除标为不可撤销，macOS / Linux 明确不支持；
- `0.9.7` 查重结果导出：结果页可导出 HTML / CSV，包含分组结构、成员与大小、保留策略与保留项、
  可回收空间合计、扫描参数与扫描时间；默认写“文档”目录，`dedup-report-<时间戳>.*` 在后续扫描中被自动忽略；
- `0.9.8` 相似图片默认关闭、阈值上限由 64 收紧到 16（旧值 12 等原样保留，越界收敛并提示），
  界面写明“调大更宽松、误判风险更高”；
- `0.9.9` 整理目标可填根外绝对路径：同卷重命名移动、跨卷复制并保留源文件（临时文件 + 尺寸校验 + 原子重命名，
  失败清理），界面横幅与动作徽标提前标注移动/复制；纠正“排除子串”“包含隐藏/系统文件”两处文案方向，
  行为未改；
- `0.10.0`–`0.10.10` 为 P1 补齐：关闭确认、拖放/F1/Esc、按时间整理、日志与环境自检、结果排序筛选与大图、
  全盘查重与快筛、Everything 搜索后端、中英双语、配置方案预设与规则测试器、整理后打开目标文件夹、
  危险源拒绝与结果页打开所在位置（逐项细节见下文「P1 补齐进度」）；
- 仍是 `0.x`：整理器撤销范围有限（删除与硬链接替换不可恢复），macOS / Linux 未做真机验证，不足以标 1.0；
- 刻意不用 C# 版的 `1.2.x`，避免两个实现共用同一版本号造成混淆（安装包、日志、问题反馈都以 `0.10.10` 为准）。

## P1 补齐进度（0.10.x）

### 0.10.0 执行中关闭确认

- 关闭窗口（且关闭行为不是“最小化到托盘”）或从托盘菜单退出时，只要查重扫描、删除执行、
  九步整理、本地搜索、网盘上传中任意一项仍在跑，就弹一次系统确认框：
  默认按钮“继续执行”，另一项是“仍然退出”。空闲时完全不弹。
- 关闭行为为“最小化到托盘”时不弹框：窗口只是隐藏，任务继续执行，不存在中断风险。
- 新增只读绑定 HasRunningTasks() (bool, string)，供前端或排障时查询当前任务，不改变任何退出路径。
- 未实测项：确认框在真实桌面上的按钮文案与默认焦点（代码路径已核对，构建通过；
  按约束未做真实桌面点击）。

### 0.10.1 拖放目录 / F1 帮助 / Esc 收起浮层

- Wails 开启 DragAndDrop.EnableFileDrop；「选择目录」步骤的路径框区域标了
  --wails-drop-target: drop，只有拖到该区域才接收，拖入时高亮描边。
- 新增绑定 InspectDroppedPath：目录直接采用；拖入单个文件改用其所在目录并提示；
  路径不存在 / 无法读取时给出原因，不抛错。
  拖放只在查重「选择目录」步骤生效，其他页签与步骤不响应，避免误改参数。
- 新增顶部「帮助 F1」按钮与帮助浮层：F1 在任何界面打开帮助，Esc 按固定优先级收起最上层浮层
  （帮助 → 更新 → 授权 → 设置 → 跳过列表 → 撤销确认 → 删除确认）。
- 未实测项：真实 Windows 拖放落点坐标与高亮类名（代码路径与 Wails 运行时核对过，未做桌面拖拽）；
  F1 在 WebView2 内的默认按键拦截（已 preventDefault，未真机按键验证）。

### 0.10.2 按时间整理（EXIF 优先）

- 新增页签「按时间整理」，与九步整理完全独立，不改动既有整理器的步骤与对拍基线。
- 分桶：日 2024-03-08、ISO 周 2024-W10、月 2024-03、年 2024；目标目录是容器目录，
  程序在其中建对应子目录。
- 时间来源：JPEG/TIFF 优先 EXIF DateTimeOriginal（回退 DateTime），其余文件用修改时间；
  界面上每行标注 EXIF / 修改时间。
- 处理方式：移动（跨卷自动退化为复制后删除源文件）或复制（临时文件 + 尺寸校验 + 原子重命名）。
- 冲突策略：改名保留（默认）、跳过、覆盖；预演阶段会标出冲突项。
- 保护：默认跳过隐藏/系统文件（可勾选包含）；目标目录位于源目录内时自动跳过目标子树；
  真实执行需勾选确认框，且受授权限制（未授权只允许预演）。
- 新增绑定：GetTimelineMeta、PreviewTimeline、StartTimeline、GetTimelineState、CancelTimeline。
- 引擎单测覆盖：分桶、EXIF 解析、mtime 回退、改名/跳过冲突、复制保留源、移动删除源。
- 未实测项：真实桌面上的目录选择与执行进度轮询、跨卷移动（跨卷分支由注入式单测覆盖复制逻辑，
  未在真实双卷环境跑过）、EXIF 时区差异（按本地时区解析，未用多时区样本验证）。

### 0.10.3 查看/清除日志 + 环境自检

- 顶部新增「诊断」入口，浮层分两页：
  - 日志：列出 <配置目录>/FileDedup/logs 与 logs/ops 下的 *.log/*.jsonl/*.txt，
    显示大小与修改时间；点击查看尾部（上限 512 KB）；可打开日志目录；
    清除需二次确认，只删文件不删目录，被占用的文件单独列在结果里。
  - 环境检测：应用版本/平台、配置目录可写（实际写探针）、日志目录与文件数、回收站能力、
    ui-settings.json 是否存在、Everything es.exe 是否在 PATH、配置盘剩余空间、更新缓存大小。
- 新增绑定：GetLogOverview、ReadLogFile、ClearLogs、RunEnvironmentCheck。
- 未实测项：真实桌面下的日志打开/清除点击路径、Windows 磁盘剩余空间的实际读数、
  被占用日志的删除失败提示（代码路径已核对，未真实点击）。

### 0.10.4 结果页排序/筛选 + 相似图片大图预览

- 结果页新增筛选条：关键字（路径/文件名）、类型（按分组类型）、排序（默认/文件大小/可回收空间/保留项文件名/修改时间）、升降序切换。
  筛选与排序只改变显示顺序和可见组；勾选状态与执行范围不受影响，界面有明确提示。
- 抽屉中的图片文件新增「加载大图预览（≤1600px）」：按需解码，等比缩放到 1600 以内，
  以 data URL 直接显示，不写临时文件；JPEG 按 EXIF 方向校正；展示原图尺寸与解码格式。
  超过 200 MB 或解码失败时给出原因，不静默。
- 新增绑定 ReadImagePreview；internal/engine/similar/image 增加 Load（共享既有解码器并做方向校正，
  未改动指纹计算路径）。
- 未实测项：真实桌面下筛选/排序与抽屉预览的点击路径、超大图与损坏图的提示、超大分组的排序耗时；
  已记入未验证清单。

### 0.10.5 全盘查重（硬排除 + 64KB 快筛）

- 「选择目录」步骤新增两个勾选项：
  - 全盘查重：勾选后忽略手选目录，扫描所有本地固定磁盘（GetDriveTypeW 只取 DRIVE_FIXED，
    不含 U 盘、光驱、网络映射盘与内存盘）；勾选时实时展示卷清单、各卷可用空间与强制排除项数量。
  - 64KB 快筛：先对同尺寸文件算前 64KB 的 SHA-256，只有指纹相同的才进入完整 SHA-256 复核。
    分组、保留项与删除判断仍以完整哈希为准，快筛仅减少需要完整读取的文件数量。
- 强制排除（不受用户排除设置影响）：Windows、Program Files、Program Files (x86)、ProgramData、
  $Recycle.Bin、System Volume Information、Recovery、PerfLogs、MSOCache、Windows.old、Config.Msi、
  $WinREAgent，以及各卷根下的 pagefile.sys、hiberfil.sys、swapfile.sys、DumpStack.log.tmp。
  实现为在 StartScan 时把硬排除项追加到引擎 excludes，不改枚举/排除的既有语义。
- 快筛与相似图片查重互斥（模型层既有校验）：前端在勾选相似图片时自动取消快筛并禁用该复选框，
  后端对被构造出的非法组合直接返回原因。
- 引擎改动：scan.hashAll 拆出 hashTargets/hashTargetsInto；新增 scan_quick.go（快筛指纹 + 候选桶 +
  全量复核），快筛读失败的文件登记为 Skipped。新增绑定 GetFullDiskPlan。
- 新增单测：快筛与全量在同一目录下分组/保留/统计一致；前 64KB 相同但尾部不同的文件不被误合并；
  同尺寸且前 64KB 均不同时全量哈希数为 0；快筛读失败登记 Skipped 且不参与分组；快筛结果快照完整。
- 未实测项：真实 Windows 上固定卷枚举与全盘扫描（未真跑全盘，避免长时间占用机器）；
  桌面上两个复选框与卷清单的点击/刷新路径（代码路径已核对、构建通过，未真实点击）；
  U 盘/光驱/网络盘的驱动类型过滤仅按 API 语义实现，未插拔设备验证。

### 0.10.6 顶部搜索接入 Everything（可选后端）

- 搜索页（顶部「搜索」）标题改为「文件搜索」，新增「检索后端」下拉：
  - 本地文件系统（默认，原实时遍历逻辑完全保留）；
  - Everything（es.exe）：仅当检测到 es.exe 时可选，未检测到则选项禁用并给出原因与修复方式，
    另有「重新检测」按钮（真实调用 GetSearchBackends，不是装饰按钮）。
- 后端定位：PATH 中的 es.exe，其次 Program Files / Program Files (x86) / LOCALAPPDATA 下的
  Everything\es.exe；非 Windows 返回“仅支持 Windows”。
- 查询方式：对每个搜索目录执行 es.exe -n <剩余上限> -path <root> <关键词>，
  输出按行解析为绝对路径（UTF-8 优先，失败按 GBK 解码，兼容中文 Windows 控制台代码页）；
  多个目录的结果去重后合并，候选达到上限时标记截断。
- 结果过滤与本地后端同口径：扩展名、最小/最大大小、隐藏/系统属性、排除目录（目录名按层级匹配、
  带路径项按前缀匹配）均在拿到候选后用本进程判断；目录条目被剔除（本页只列文件）。
- 进度口径区分：Everything 任务在状态里带 backend，界面显示“Everything 返回 N 条候选”，
  本地后端仍显示“遍历 N 个文件 / N 个目录”，不会把索引命中数说成遍历数。
- 取消：仍走 CommandContext，取消会终止 es.exe 子进程并保留已有结果。
- 新增绑定 GetSearchBackends；SearchRequest 增加 backend，SearchState 增加 backend。
- 新增单测（desktop 标签）：es.exe 输出解析（去 BOM/CR、丢弃非绝对路径行）、GBK 解码、
  扩展名/大小/隐藏/排除目录过滤。`go test -tags desktop ./cmd/filededup` 通过。
- 未实测项：本机未安装 Everything（PATH 中无 es.exe），真实 es.exe 查询与 GBK/UTF-8 输出
  仅在单测与代码路径层面验证，未连真索引跑过；搜索页下拉与“重新检测”按钮未在桌面点击。

### 0.10.7 中英双语（界面文案，zh / en）

- 新增 web/src/i18n.ts：Lang = "zh" | "en"，模块级 current + getLang/setLang/tr(t)，useT() 基于
  useSyncExternalStore，语言切换即时生效；英文词典以中文原文为键，按页面拆分为
  i18n/en-common.ts、en-app.ts、en-organize.ts、en-lanzou.ts、en-misc.ts，缺键回退中文并在 DEV 下 warn。
- 动态文案统一用 {name} 占位符，例：t("遍历 {n} 个文件", { n })。模块级标签表
  （KEEP_LABELS、KIND_LABELS、STEPS、ORG_KEEP_LABELS、Lanzou 的 KIND/PLATFORM/SOURCE/PHASE）
  仍保留中文常量作为词典键，翻译在渲染时做（t(value)），不在模块初始化期做。
- 已本地化的页面与组件：App 外壳（顶栏、扫描页、结果页、托盘/关闭提示）、Organizer（九步整理）、
  TimeOrganizer（按时间整理）、Search（搜索）、Settings（设置）、Diagnostics（诊断）、Lanzou（蓝奏云）。
- 语言入口两处：顶栏徽标按钮（中文界面显示 “English”，英文界面显示 “中文”，点击保存设置并立即切换）；
  设置页新增“语言”下拉（中文 / English），附说明“切换会立即生效并保存到设置文件”。
- 持久化：UISettings 新增 lang 字段（默认 "zh"），只接受 zh/en，非法值回退默认并记录警告；
  前端在 settings 变化时调用 setLang。浏览器 mock 的设置读写同步支持 lang。
- 词典规模：794 条英文键，覆盖 web/src 中全部 t()/tr() 字面量（审计脚本比对缺键为 0）。
- 新增 web/src/vite-env.d.ts（vite/client 类型引用），修掉 import.meta.env 的类型报错。
- 后端运行时字符串（错误消息、状态/阶段、存疑原因、授权与更新提示、Lanzou 远端消息、
  诊断环境明细）仍以中文返回，英文界面下这部分按原样显示；未做后端消息翻译。
- 未实测项：顶栏语言徽标与设置页语言下拉未在桌面真实点击（代码路径已核对、tsc 与构建通过）；
  语言切换后各页面的视觉排布（英文文本更长导致的换行/截断）未逐页截图核对；
  任务执行中途切换语言时组件重挂载后的轮询续接未实测。

### 0.10.8 配置方案预设、规则测试器

- 新增 cmd/filededup/organize_presets.go：整理规则可命名保存为一套预设，文件位于用户配置目录
  FileDedup/organize-presets.json（schemaVersion=1、kind=filededup.organize.presets、DisallowUnknownFields）。
  绑定 ListOrganizePresets / SaveOrganizePreset / DeleteOrganizePreset；保存前复用导入规则的
  normalize + validate 口径，根目录不写入预设（预设不绑定机器）；同名（大小写不敏感）覆盖时保留 ID 与创建时间；
  写入用临时文件 + 原子替换；文件损坏时读取与保存都报错，不静默清空旧预设。
- 新增 cmd/filededup/organize_tester.go：规则测试器绑定 TestOrganizeRules，对样例文件名做纯计算，
  不读盘、不写盘、不建任务。复用引擎导出纯函数：CleanPrefix / CleanSuffix / ClassifySpecial /
  Category / ExtensionUpper / JoinRel / SanitizeTargetName / SplitKeywords / ContainsAnyFold / ParseOrganizeMode。
  输出稳定键（specialAction: disabled/garbage/suspect/keep/excluded；organizeSkip:
  deleted/disabled/no-target/keyword-empty/keyword-miss/regex-empty/regex-invalid/regex-miss），
  界面负责翻译；样例上限 200 行。
- 整理页新增两块：配置方案预设（名称输入、保存/刷新/应用/删除、同名覆盖提示、预设文件路径展示）；
  规则测试器（多行样例输入、运行测试、汇总计数、表格展示原名/改名后/特殊判定/整理去向/最终路径）。
- 预设与测试器均接入中英双语词典（en-organize.ts 新增 60 余条）。
- 新增单测：organize_presets_test.go（保存/覆盖/列表/删除/名称与规则校验/损坏文件不覆盖）、
  organize_tester_test.go（前后缀剥离/保护词排除/特殊判定/删除后跳过整理/五种整理方式与跳过原因/输入上限/步骤关闭）。
- 浏览器预览：预设存 localStorage（刷新不丢，禁存储时只在本次会话有效）；规则测试器无 Go 引擎，
  点击后如实报“浏览器预览没有 Go 引擎，规则测试器只在桌面版可用”，不做假结果。
- 未实测项：整理页的预设保存/刷新/应用/删除与“运行测试”按钮未在桌面真实点击（代码路径已核对，
  tsc、构建与后端单测通过）；新增长表格在中英两种语言下的排版换行未逐条截图核对；
  预设文件在真实用户配置目录下的读写与权限异常（只读目录）未实测。

### 0.10.9 整理完成后打开目标文件夹

- OrganizeState 新增 openDir（绝对路径）：整理步骤开启且设置了目标时，根内目标解析为 根目录+相对目标，
  根外目标取绝对路径；步骤关闭或未设目标时为空。新增纯函数 organizeOpenDir 并配单测
  （TestOrganizeOpenDir：步骤关闭 / 未设目标 / 根内相对 / 根外绝对）。
- 整理页在真实执行完成（phase=done、非试运行、非仅规划）且 openDir 非空时显示「打开目标文件夹」按钮，
  按钮调用既有的 OpenPath 绑定；打开失败在页面内报错（打开目标文件夹失败：{error}），不静默。
- 本页功能状态文案补上该项，并接入中英词典。
- 未实测项：整理页「打开目标文件夹」按钮未在桌面真实点击（代码路径已核对、go vet/go test/npm build 通过）；
  真实执行完成后 Explorer 是否按预期定位到目标目录、目标目录被外部删除时的报错展示未实测；
  根外目标（跨卷复制）完成后的打开行为未实测。

### 0.10.10 危险源拒绝 + 结果页「打开所在位置」

- 新增 cmd/filededup/dangerous_root.go：StartScan（非全盘分支）与 StartOrganize 在解析出绝对路径后
  统一走 rejectDangerousRoot，拒绝两类危险源：
  - 整盘根目录（卷根）：`C:\`、`D:\`、`/` 等。Windows 上 `filepath.Clean("/")` 会归一成 `\`，
    按路径分隔符而无卷名的情形单独判定，避免漏掉 `/`。提示引导用户选择具体子目录，
    并说明整盘查重应使用「全盘查重」。
  - 系统目录：Windows、Program Files、Program Files (x86)、ProgramData、System Volume Information、
    $Recycle.Bin、Recovery、Boot、PerfLogs、Windows.old、Config.Msi、$WinREAgent、System32、
    SysWOW64、WinSxS（按路径层级逐段匹配，大小写不敏感）。
  全盘查重模式不走该校验：它本身就以所有固定卷为范围，依赖既有的 fullDiskExcludes 硬排除；
  校验只拦「把整盘或系统目录当普通扫描/整理根目录」的用法，不改既有枚举语义。
- 拒绝消息区分操作：扫描提示“拒绝扫描整盘根目录…”；整理提示“拒绝整理整盘根目录…”；
  系统目录提示“拒绝扫描系统目录：…” / “拒绝整理系统目录：…”。均为中文返回，与其余后端消息一致。
- 新增单测 dangerous_root_test.go：卷根（`C:\`、`/`）被拒且扫描消息包含“全盘查重”、
  12 个系统目录被拒、用户目录（临时目录、`C:\Users\me\Pictures`、`D:\资料\2024`）放行。
- 结果页每个成员行新增「打开所在位置」按钮（新增「操作」列，表格列宽改为
  112px / 300px / minmax(420px,1fr) / 110px / 168px / 96px，min-width 1364px）：
  调用既有 RevealPath 绑定在文件管理器中选中该文件；失败在页面内报错
  （打开所在位置失败：{error}），不静默。性能压测模式（perfActive）下该列不渲染按钮，
  避免压测路径上绑定大量事件。按钮单元格 stopPropagation，点它不会触发行的选中/展开。
- 中英词典补充「打开所在位置失败：{error}」；「打开所在位置」「操作」复用既有词条。
- 未实测项：结果页「打开所在位置」按钮未在桌面真实点击（代码路径已核对、tsc/npm build/go vet/go test 通过）；
  Windows Explorer 是否按预期选中该文件、文件已被移动/删除时的报错展示未实测；
  危险源拒绝对话（扫描页与整理页选择整盘或系统目录后的提示）未在桌面 UI 实际触发；
  整理侧拒绝只由单测与代码路径覆盖，未走界面按钮验证；性能压测模式下隐藏该列的视觉效果未截图核对。

### 0.10.12 本地搜索 Everything 后端真正可用

- 根因：Everything 1.5 安装包不再附带 es.exe，原检测只查 PATH 与
  `%ProgramFiles%\Everything` / `%ProgramFiles(x86)%` / `%LOCALAPPDATA%` 三个固定目录，
  本机 Everything 1.5 已安装并运行但目录内只有 Everything.exe，检测判为不可用，下拉被禁用。
- findEverythingCLI 探测顺序改为：PATH → 卸载注册表项（InstallLocation / DisplayIcon）、
  Everything 服务 ImagePath、常见安装目录（Everything / Everything 1.5a / Everything 1.4）→
  `%LOCALAPPDATA%\Programs`（深度 4）与 Program Files（深度 4）有界搜索 es.exe（2 万条目预算，
  结果缓存 60 秒，不跟随符号链接）。
- 静默失败处理：es.exe 对不存在目录、Everything 未运行时都返回 0 且无输出。
  现在查询前先 os.Stat 每个根目录，全部不可用时报「搜索目录不可用：…」；
  混合根目录时保留有效结果并登记跳过项；es.exe 返回 0 候选且未检测到 Everything.exe 进程时
  报「es.exe 没有返回任何结果：未检测到运行中的 Everything」。
- 诊断页的 Everything 自检改用同一检测函数，并提示 Everything 是否在运行；搜索页后端说明同步提示。
- 新增 testdata/fake-es（测试期编译为 es.exe）证明 `-n`/`-path`/关键词参数确实传给子进程，
  并覆盖输出解析、缺失目录原因与混合目录行为。
- 实测：本机真实 es.exe（`%LOCALAPPDATA%\Programs\AIPanBot\_internal\tools\es.exe`，1.1.0.27）
  被检测命中；实际命令 `es.exe -n 3 -path "C:\Program Files\Everything" *.exe` 退出码 0，
  stdout 首行 `C:\Program Files\Everything\Everything.exe`；应用内链路 visits=1、matches=1。
- 未实测：es.exe 进程被杀/Everything 退出时的界面提示未在 GUI 中点击验证（仅代码路径与单测）；
  非 PATH、非 Programs 的自定义安装位置未在真机验证。

### 0.10.13 优享登录「系统时间不正确」修复

- 根因：原版（C#）对 `timestamp` 做 **AES-128-ECB + PKCS7** 加密后取小写 hex 再拼进查询串
  （`B/F.cs` 的 `A(long)`，key 由字符串表解出为 `lanZouY-disk-app`）；Go 端一直发送明文毫秒，
  服务端把明文当非法时间戳，返回 `{"msg":"系统时间不正确","code":-1}`。
- 改动：`internal/cloud/lanzouyou/client.go` 新增 `encryptedTimestamp(ms)`，登录
  （`/unproved/login`）与所有公共查询（`withCommon`）统一改用它；补充解析 `data.items.appToken`
  作为 `data.appToken` 的后备；错误码与 msg 继续由 `checkEnvelope` 原样透传（新增单测锁定）。
- 逐字段对照（先解字符串表再对齐，getter 名大小写敏感，见下）：

  | 字段 | 原版 C# | 原 Go | 结论 |
  | --- | --- | --- | --- |
  | `uuid` / `devCode` | `Uri.EscapeDataString(uuid)` | `url.QueryEscape(uuid)` | 一致（UUID 只含 `-`，两者结果相同） |
  | `devType` | `6` | `6` | 一致 |
  | `devModel` / `devVersion` | `chrome` / `131` | `chrome` / `131` | 一致 |
  | `appVersion` | 空 | 空 | 一致 |
  | `timestamp` | `B.F.A(DateTimeOffset.Now.ToUnixTimeMilliseconds())` → AES-ECB/PKCS7 小写 hex | 明文 13 位毫秒 | **不一致，已修** |
  | `extra` | `&extra=2` | `&extra=2` | 一致 |
  | `appToken` | 有则 `&appToken=` + Escape | 同 | 一致 |
  | 时区 | `DateTimeOffset.Now`（本地秒数换算为毫秒） | `time.Now().UnixMilli()` | 一致（都是绝对毫秒，与时区无关） |
  | body | `{"loginName","loginPwd"}`，仅转义 `\` 与 `"` | `encoding/json` | 一致（JSON 转义更严格，服务端接受） |
  | `Accept` | `application/json, text/plain, */*` | 同 | 一致 |
  | `Accept-Language` | `zh-CN,zh;q=0.9` | 同 | 一致 |
  | `User-Agent` | Chrome/78 UA | 同 | 一致 |
  | `Origin` / `Referer` | `https://www.ilanzou.com` / 同 | 同 | 一致 |
  | `X-WK-Device-Flag` | `1` | `1` | 一致 |
  | `X-WK-UID` | uid 非空才发 | 同 | 一致 |
  | `Authorization` | `Bearer ` + appToken | 同 | 一致 |
  | Cookie | 有才发 | Go 侧没有 Cookie 字段 | 不适用（优享用 appToken） |

- 真实请求实测（假账号 `probe-not-exist@example.invalid`，HTTP 200）：
  - `timestamp` 用 AES hex → `{"msg":"用户账号不存在","code":-1}`（说明校验已通过，进入账号校验阶段）；
  - `timestamp` 用明文毫秒 → `{"msg":"系统时间不正确","code":-1}`；
  - `timestamp` 填 `0000000000000` → 同样「系统时间不正确」。
  服务端 `Date` 头与本机时间一致，排除本机时钟偏差。
- 单测：固定向量 `1690000000000 → 754a938402a7e614bad3c853f71f95d6`（独立实现 PowerShell Aes 算出），
  另覆盖登录 timestamp 必须是 32 位小写 hex、错误 msg 原文透传、`data.items.appToken` 解析。
- 未联调：没有真实优享账号，只验证到「账号不存在」这一层；正确账号/密码下的 appToken、容量、
  上传链路仍未实测，需账号后复测。

### 0.10.14 优享登录账号框与密码框同高同宽

- 根因：`.formgrid` 的输入框样式只匹配 `number / text / 无 type / select`，**漏了 `password`**。
  密码框退回浏览器默认样式（内边距更小、边框更粗、无圆角与白底），所以肉眼更矮、内文区更窄。
- 修复（`web/src/styles.css`）：
  - 在 `.formgrid` 输入框选择器组补上 `input[type="password"]`，两框共用同一份 padding / border /
    border-radius / background / width / min-width；
  - 追加 `.formgrid input[type="password"]::-ms-reveal { display: none; }`，避免 WebView2（Edge）
    的原生「显示密码」按钮挤占输入区。
- 改前 / 改后（14px 字号、行高 1.65，盒模型为全局 `border-box`）：

  | 项目 | 改前 账号框 / 密码框 | 改后 两框一致 |
  | --- | --- | --- |
  | 计算高度 | 41.1px / 29.1px（23.1 行 + 内边距 + 边框） | 41.1px |
  | `padding` | `8px 10px` / 浏览器默认 `1px 2px` | `8px 10px` |
  | `border` | `1px solid #e2e6ec` / 默认 `2px inset` | `1px solid #e2e6ec` |
  | `border-radius` | `8px` / 无 | `8px` |
  | 背景 | `var(--card)` 白底 / 默认 | `var(--card)` 白底 |
  | `width` | `100%` / 靠 flex 拉伸 | `100%` |
  | 内文区宽度 | 略宽 / 因内边距与边框更大而更窄 | 相同 |

- 实测：`web npm run build` 通过（tsc + vite 均无报错）；中英文案都会走同一套输入框样式，
  未发现溢出。**未实测**：未在桌面窗口里截图逐像素核对；密码框的 `::-ms-reveal` 在 WebView2
  下是否真的隐藏，需要在桌面端打开登录页确认。

### 0.10.15 更新源自动检测（内置默认源）

- 行为：
  - 设置里的「更新清单地址」留空 → 自动查询 GitHub 仓库 `asybhc/filededup` 的 **latest release**，
    从 release 资产中找签名清单 `update-manifest.json` 与安装包（优先 `*Setup*.exe`，其次任意 `.exe`）；
  - 填写地址 → 仍按原来的直连清单方式工作，作为覆盖项；
  - 签名策略不变：清单必须过 Ed25519，安装包必须过「长度 + SHA-256 + 包签名」，
    没有任何“忽略签名继续安装”的分支；
  - 自动模式额外做一致性校验：release 标签必须与清单 `version` 相同；安装包名称、大小必须与
    release 资产一致；GitHub 提供 `digest` 时还必须与清单 `sha256` 相同。通过后把下载地址替换为
    release 里的真实资产地址（哈希仍按清单校验）。
- 失败原因透传：GitHub API 非 200 → `HTTP <code>：<响应片段>`；清单非 200 与安装包非 200 同样附带
  响应前 300 字；缺少签名清单时列出 release 现有资产名。
- 真实请求（2026-11-18，本机）：
  - `GET https://api.github.com/repos/asybhc/filededup/releases/latest` → `HTTP 200`
  - `tag=0.10.10`；资产只有 `fdedup-ui.exe`（14187520 字节，`sha256:43256c4a…93cd`），
    **没有 `update-manifest.json`**，因此当前自动检查会明确报：
    「GitHub 最新发布 0.10.10 未附带签名清单 update-manifest.json（现有资产：fdedup-ui.exe）」。
- 发布要求：每次发布 release 需同时上传 `update-manifest.json`（Ed25519 签名，资产字段指向该
  release 的安装包），自动更新才可走通。当前 real release 尚未附带该资产。
- 单测：`internal/update/github_test.go` 覆盖 API 解析（tag/资产/摘要/优先 Setup）、404 状态与响应
  片段透传、缺少清单时列出资产、非法 tag 拒绝；`go test -tags desktop ./cmd/filededup/` 覆盖现有更新链路。
- 实测：`go test ./internal/update/` → `ok`；`go test -tags desktop ./cmd/filededup/` → `ok`。
- **未实测**：真实的签名清单 + release 安装包端到端下载/替换尚未联调（当前 latest release 无清单资产）；
  等发布 0.10.15 并附带清单后再验证。手动覆盖地址的旧分支保持原样，未回归。

### 0.10.16 系统体检：迁移中断可重试、扫描防重复启动

体检按「数据安全 → 凭据 → 更新链 → 并发 → 错误处理 → 界面」顺序只读排查，
本轮修复两项会造成实际损失的问题，其余列清单待确认。

- 修复 1：数据目录迁移半途中断后永远无法补齐（`cmd/filededup/migrate.go`）。
  - 原行为：只判断新目录是否存在，一旦新目录被创建（哪怕只复制了一部分就断电/崩溃），
    后续启动直接跳过，缺失的 `lanzou.bin`、`trial.dat` 等永不补齐，旧目录虽保留但新目录长期缺文件。
  - 现行为：以 `<新目录>/.migration-complete` 标记为准。无标记时继续复制并只补缺失文件
    （已存在文件一律不覆盖）；全部来源无失败才写标记；有失败下次启动重试。
  - 单测：`TestMigrateRetriesWhenMarkerMissing`（模拟半途中断：不覆盖已有文件、补齐缺失文件、
    写标记、再次运行不重复迁移）；`TestMigrateSkipsWhenMarkerExists`。实测
    `go test -tags desktop ./cmd/filededup/ -run 'TestMigrate' -v` → 5 个用例 PASS。
- 修复 2：扫描按钮与托盘事件可并发启动重复扫描（`web/src/App.tsx` `startScan`）。
  - 原行为：`startScan` 是异步函数，点击后到 `setBusy(true)` 之间没有同步守卫，
    双击按钮或「打开主窗口」托盘事件连发时会发出两次 StartScan，产生两个扫描任务竞争同一状态。
  - 现行为：新增 `startingScanRef`，进入即置位、`finally` 复位；守卫期内重复调用直接返回。
- 体检已核对、未改动（结论为安全）：
  - 回收站失败不会静默转永久删除（`internal/platform/trash_windows.go` 返回错误，执行器记 Failed）；
  - 撤销恢复不覆盖原位置同名文件（`internal/platform/restore_windows.go:60`、`:118`）；
  - 整理路径拒绝绝对路径、盘符、`..` 逃逸与超长路径（`internal/engine/organize/exec.go` `resolveRel`）；
  - 跨卷复制走临时文件 + 大小校验 + 原子重命名，源文件保留（`copyFileAtomic`）；
  - 回收站/永久删除前复核大小与修改时间，永久删除额外复核 SHA-256（`internal/engine/apply/apply.go` `precheck`）；
  - 勾选未知路径返回 `ErrSelectionUnknown`，不会被计划执行器静默跳过（`internal/engine/plan`）；
  - 凭据文件 DPAPI 加密、权限 0600（`internal/secret`、`lanzou.bin`），日志与诊断未发现明文密码/Cookie/token；
  - 更新清单与安装包 Ed25519/长度/SHA-256 校验分支完整，无空签名绕过路径；缓存清理只作用于更新缓存目录。
- 待确认清单（未改，需授权后处理）：
  1. 中：撤销恢复同名项可能取错版本。`restore_windows.go:69-77` 仅按「文件名 + 原目录」匹配回收站项，
     同名文件多次删除时按回收站枚举顺序取第一个，可能恢复旧版本。复现：同一目录下同名文件删除两次后撤销。
  2. 中：CLI `fdedup apply --plan` 不校验动作路径是否在计划根目录内（`cmd/fdedup/apply.go:48-83`）。
     计划 JSON 可写入任意绝对路径，需显式 `--yes` 才执行；桌面端不受影响（走引擎内存计划）。
  3. 低：蓝奏老版 HTTP 客户端未设总超时（`internal/cloud/lanzou/client.go:58`），仅靠调用方 ctx；
     服务端无响应且调用方 ctx 无 deadline 时可能长时间挂起。上传大文件需要长超时，未直接收紧。
- 校验：`web npm run build`、`go vet ./...`、`go vet -tags desktop ./...`、
  `go test ./...`、`go test -tags desktop ./...` 全部退出码 0。
- **未实测**：修复 1 的断电/杀进程真实中断场景只用单测模拟，未在真机迁移中人为中断；
  修复 2 的双击与托盘连发未在桌面窗口实际连点验证。

