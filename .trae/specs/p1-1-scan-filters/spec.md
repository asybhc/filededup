# P1-1 扫描过滤条件（大小区间 + 扩展名白名单）规范

状态：待确认
基线：`d3ecc93`（P0 全部完成，工作区干净）
目标版本：`0.9.10`
提交策略：本次只做这一项，单独一个提交，完成后停下报告，不进入 P1 下一项。

## 1. 目标

补齐查重扫描的过滤能力，并让过滤结果可见、可导出、可复现：

1. 最小/最大文件大小（单位在界面写明，留空表示不限）。
2. 扩展名白名单（如 `jpg,png,zip`，留空表示不限；大小写不敏感，带不带点都能识别）。
3. 查重页与 CLI 双入口；整理器若复用同一套枚举则语义一致（现状：整理器使用独立快照与独立 `MinSize`，不共享本枚举，见 §7）。
4. 扫描结果给出“被大小阈值过滤”“被扩展名过滤”的跳过数量。
5. 过滤条件写入 `model.Request`，随结果 JSON 与 HTML/CSV 导出体现。

## 2. 语义定义（决策完成，不再二次解释）

### 2.1 大小过滤

- 字段：`model.Request.MaxSize int64`（JSON `maxSize`，字节），与既有 `MinSize` 成对。
- `MinSize = 0` 且 `MaxSize = 0`：不限大小。
- 保留条件：`size >= MinSize` 且（`MaxSize == 0` 或 `size <= MaxSize`）。
- 边界包含：恰好等于下限或上限的文件**保留**。
- `MaxSize < 0`：校验错误。
- `MaxSize > 0 && MaxSize < MinSize`：校验错误，提示“最大文件大小不能小于最小文件大小”。
- 读不到大小的文件仍走既有 `size_failed` 逻辑，不计入本次新增的两个过滤计数。

### 2.2 扩展名白名单

- 字段：`model.Request.Extensions []string`（JSON `extensions`）。
- 空数组 / 全为空白：不限扩展名。
- 归一化（引擎统一执行，保证结果内记录与匹配一致）：
  - 按 `[,，;；\s]+` 拆分（与排除项风格一致）；
  - 去首尾空白；
  - 去掉开头的 `*.`、`*`、`.`（`*.JPG`、`.jpg`、`jpg` 等价）；
  - 转小写；
  - 去重并保持首次出现顺序；
  - 归一化后为空的条目忽略。
- 匹配：取文件名最后一个 `.` 之后的后缀，忽略大小写比较；`jpg` 匹配 `IMG.JPG`，不匹配 `a.jpg.txt`。
- 只过滤文件，不影响目录遍历。
- 扩展名条目包含路径分隔符或盘符（如 `dir/jpg`）：校验错误，避免用户误以为能按目录过滤。

### 2.3 过滤顺序与计数归属

单个候选文件的判定顺序：

1. 链接/隐藏/系统属性（既有行为，不计数）。
2. 排除目录、工具报告文件（既有行为，不计数）。
3. 大小过滤：命中则 `skippedBySize++`，不再判断扩展名。
4. 扩展名过滤：命中则 `skippedByExtension++`。

因此“同时不满足大小与扩展名”时只计入 `skippedBySize`，两个计数之和不会重复计同一文件。这两个计数是**候选过滤计数**，不逐条写入 `Result.Skipped`（与既有“正常排除不登记”的约定一致）。

## 3. 代码改动清单

### 3.1 引擎契约 `internal/engine/model/scan.go`

- `Request` 增加：
  - `MaxSize int64 \`json:"maxSize,omitempty"\``
  - `Extensions []string \`json:"extensions,omitempty"\``
- `Stats` 增加：
  - `SkippedBySize int64 \`json:"skippedBySize"\``
  - `SkippedByExtension int64 \`json:"skippedByExtension"\``
- 新增导出函数：
  - `NormalizeExtensions(raw []string) []string`
  - `ExtensionAllowed(normalized []string, path string) bool`
  - `ValidateExtensions(raw []string) error`（供 UI/CLI 提前报错；引擎内部仍会归一化）
- `DefaultRequest` 不强制设置 `Extensions`，保持 nil/空等价；如需 JSON 稳定，在 `run` 中归一化后置为 `[]string{}`。

### 3.2 校验 `internal/engine/model/validate.go`

`Request.Validate` 增加：

- `MaxSize < 0` → `maxSize 不能为负：%d`
- `MaxSize > 0 && MaxSize < MinSize` → `maxSize（%d）不能小于 minSize（%d）`
- `ValidateExtensions(r.Extensions)` 失败 → 返回具体条目错误。

### 3.3 枚举 `internal/engine/scan/enumerate.go`

- 入口预计算 `exts := model.NormalizeExtensions(req.Extensions)`。
- 抽出统一判定函数（文件内私有）：
  - `filterFileBySize(size int64, req model.Request) bool`
  - 命中大小/扩展名时分别累加局部计数。
- 普通文件分支与 `FollowLinks` 的链接文件分支都使用同一判定，避免两条路径语义分叉。
- 返回签名改为 `([]fileEntry, []model.Skip, filterCounts, error)`；`filterCounts{bySize, byExtension int64}`。

### 3.4 扫描主流程 `internal/engine/scan/scan.go`

- `run` 在 `Validate` 之后执行：
  - `req.Extensions = model.NormalizeExtensions(req.Extensions)`
  - 保证 `Result.Request` 记录的是归一化后的白名单。
- 接收 `enumerate` 的新返回值，写入 `Stats.SkippedBySize` / `Stats.SkippedByExtension`。
- 取消路径返回的部分结果同样带上过滤计数。

### 3.5 Schema `internal/engine/model/schema/v1/filededup.schema.json`

- `scanRequest.properties` 增加 `maxSize`（integer, minimum 0）、`extensions`（string array）。
- `stats.properties` 增加 `skippedBySize`、`skippedByExtension`（integer, minimum 0）。
- 三个字段均为可选（不加入 `required`），旧结果文件仍可被校验；当前引擎输出始终包含统计字段。
- 契约版本保持 `SchemaVersion = 1`：本次只是新增可选字段，无改名/删除。

### 3.6 桌面桥接 `cmd/filededup/app.go`

- `appVersion = "0.9.10"`。
- `ScanRequest` 增加 `MaxSize int64` / `Extensions []string`。
- `ScanState` 增加 `SkippedBySize` / `SkippedByExtension`。
- `StartScan`：
  - `MaxSize < 0` → “最大文件大小不能为负”；
  - `MaxSize > 0 && MaxSize < MinSize` → “最大文件大小不能小于最小文件大小”；
  - `model.ValidateExtensions` 失败时原样返回错误；
  - 映射 `mreq.MaxSize`、`mreq.Extensions`（归一化在引擎内完成）。
- 扫描完成后把 `res.Stats.SkippedBySize/SkippedByExtension` 写入状态。

### 3.7 设置 `cmd/filededup/settings.go`

- `ScanSettings` 增加 `MaxSize int64 \`json:"maxSize"\``、`Extensions []string \`json:"extensions"\``。
- `DefaultSettings`：`MaxSize: 0`、`Extensions: []string{}`。
- `normalizeSettings`：
  - `MaxSize < 0` → 置 0 并提示；
  - `MaxSize > 0 && MaxSize < MinSize` → 置 `MaxSize = 0`（退回不限）并提示“已忽略最大文件大小”；
  - `Extensions` 归一化；nil → `[]string{}`；
  - 非法条目在归一化时丢弃并提示（设置文件容错，不阻塞启动）。

### 3.8 定时任务 `cmd/filededup/schedule.go`

- `scanRequestFromSettings` 映射 `MaxSize` 与 `Extensions`，与 `StartScan` 使用同一份归一化/校验语义。
- 定时任务复用设置中的值，不另设默认值；设置归一化后不会再出现上下限颠倒。

### 3.9 导出 `cmd/filededup/export.go`

- `exportSummary` 增加 `MaxSize`、`Extensions`、`SkippedBySize`、`SkippedByExtension`。
- `buildExportSummary` 从 `req` 与 `result.Stats` 填充。
- HTML“扫描参数”新增：
  - `最大文件大小：<n> 字节（<human>）`，0 显示“不限”；
  - `扩展名白名单：jpg png zip`，空显示“（无）”；
  - 汇总区新增 `按大小过滤跳过`、`按扩展名过滤跳过`。
- CSV：
  - `exportCSVHeader` 末尾追加三列：`最大文件大小`、`扩展名白名单`、`过滤跳过（大小/扩展名）`；
  - 扫描信息行、汇总行同步补齐列数，保持每行列数一致；
  - 过滤跳过列格式 `大小 n / 扩展名 m`。

### 3.10 CLI `cmd/fdedup/main.go`

- `scan` 新增 flag：
  - `--max-size <n>`：最大文件大小（字节），0=不限；
  - `--ext <list>`：扩展名白名单，逗号/分号/空白分隔，留空=不限。
- 帮助文本（`usage` 的 scan 选项段）同步。
- 显式传参时覆盖 `--request` 中的同名值（遵循既有 `set[...]` 规则）。
- 解析后校验：
  - `--max-size` 为负 → stderr 明确报错，退出码 2；
  - `--max-size` 非 0 且小于 `--min-size` → 报错退出码 2；
  - 非法扩展名条目 → 报错退出码 2。
- 结果 JSON 的 `request` 中自动带归一化后的 `extensions` 与 `maxSize`。

### 3.11 前端类型 `web/src/types.ts`

- `ScanRequest` 增加 `maxSize: number`、`extensions: string[]`。
- `ScanSettings` 增加 `maxSize: number`、`extensions: string[]`。
- `ScanState` 增加 `skippedBySize: number`、`skippedByExtension: number`。

### 3.12 查重页 `web/src/App.tsx`

- 过滤区新增：
  - `最小文件大小（MB，留空 = 不限）`
  - `最大文件大小（MB，留空 = 不限）`
  - `扩展名白名单（如 jpg,png,zip；留空 = 不限；大小写与点号均可）`
- 大小输入改为保留原始文本的受控输入（不再 `Math.max(0, ...)` 静默归零），新增模块级解析函数：
  - `parseSizeMB(text): { bytes: number } | { error: string }`
  - 规则：空 → 0；必须是有限、非负数字（允许小数）；负数/非数字/带单位字符 → 明确错误“最小/最大文件大小必须是 ≥0 的数字，单位 MB”。
  - 上下限都非空且 `min > max` → 错误“最小文件大小不能大于最大文件大小”。
  - 转换：`Math.round(mb * 1024 * 1024)`。
- 过滤区下方展示 `filterNote`（有错时红色提示）；`startScan` 遇到解析错误时 `setError` 并中止，不发起扫描。
- 设置恢复/自动保存：
  - `minSize`/`maxSize` 以字节持久化，读取时换算成 MB 文本；
  - `extensions` 持久化为归一化数组，界面用空格连接展示。
- 结果统计 chips 增加：
  - `大小过滤 <b>{scan?.skippedBySize ?? 0}</b>`
  - `扩展名过滤 <b>{scan?.skippedByExtension ?? 0}</b>`
- 现有“跳过”chip 仍表示逐条异常跳过，与过滤计数区分，并在提示文案中写明。

### 3.13 浏览器 mock `web/src/mock.ts`

- 默认设置补 `maxSize: 0`、`extensions: []`。
- mock 扫描按与引擎一致的规则过滤候选并返回两个计数（大小优先，边界包含），保证预览环境不自相矛盾。

### 3.14 文档 `docs/ui-shell.md`

- 版本历史新增 `0.9.10`。
- 新增“扫描过滤”章节：字段语义、单位、边界、跳过计数、导出体现、CLI 用法。
- 记录验证结果与未验证项（见 §6）。
- 明确说明整理器使用独立过滤配置，不与扫描共享枚举，本次未改动其 `MinSize` 语义。

## 4. 测试计划（必须覆盖）

新增 `internal/engine/scan/filters_test.go`：

1. **边界值**：`MinSize=10` 时 9 跳过、10 保留；`MaxSize=20` 时 20 保留、21 跳过。
2. **扩展名变体**：`jpg` 命中 `A.JPG`、`.Jpg` 输入等价、`*.PNG` 等价；`tar.gz` 不命中 `jpg`；空名单全放行。
3. **组合过滤**：大小与扩展名同时设置，仅保留同时满足者；同时不满足时只计入大小计数。
4. **留空即不限**：`MinSize=0、MaxSize=0、Extensions=[]` 时与无过滤结果一致。
5. **跳过计数**：构造 6 个文件（低于下限 2、高于上限 1、扩展名不符 2、合格 1），断言 `SkippedBySize=3`、`SkippedByExtension=2`、`FilesEnumerated=1`。
6. **请求记录**：`Result.Request.MaxSize` 与归一化后的 `Extensions` 正确。
7. **取消路径**：取消时计数仍反映已枚举部分（可复用现有取消测试模式）。

`internal/engine/model` 测试：

8. `Validate` 拒绝负 `MaxSize`、上下限颠倒、含路径分隔符的扩展名。
9. `NormalizeExtensions` 的点号/星号/大小写/去重/空白拆分。

`cmd/filededup` 测试（如已有 settings/schedule 测试则扩展）：

10. `normalizeSettings` 对负 `MaxSize`、颠倒区间、非法扩展名的收敛与提示。
11. `scanRequestFromSettings` 透传 `MaxSize`/`Extensions`。

`cmd/fdedup`：

12. CLI 解析负值/颠倒区间返回退出码 2 并输出明确错误（若现有 CLI 测试结构可承载）。

## 5. 界面与 CLI 用法（交付说明用）

界面（查重页 → 1. 选择目录与扫描设置）：

- 最小文件大小：单位 MB，留空或 0 = 不限；恰好等于下限的文件保留。
- 最大文件大小：单位 MB，留空或 0 = 不限；恰好等于上限的文件保留。
- 扩展名白名单：`jpg,png,zip`、`.jpg .png`、`*.JPG` 均可；留空 = 全部扩展名。
- 扫描结果 chips 显示“大小过滤 N”“扩展名过滤 M”。

CLI：

```text
fdedup scan --root D:\data --min-size 1048576 --max-size 104857600 --ext jpg,png,zip
```

## 6. 验证步骤与产物

1. `git status --short` 确认只包含本项改动。
2. `go vet -tags desktop ./...`
3. `go test -tags desktop ./...`
4. `cd web && npm run build`
5. `powershell -File scripts/build-desktop.ps1`
6. `Get-FileHash bin\fdedup-ui.exe -Algorithm SHA256`
7. 单一提交：`feat(scan): 支持大小上下限与扩展名白名单过滤并统计跳过数（0.9.10）`
8. 报告：改动摘要、界面/CLI 用法、跳过计数位置、验证结果、exe 路径/大小/SHA-256、未验证项。

未验证项（预期）：

- 超大目录下过滤计数的性能影响只做单测，不做真实百万文件压测。
- 浏览器 mock 与真实后端的一致性只覆盖过滤语义，不覆盖 Wails 事件时序。

## 7. 整理器一致性结论

- 整理器使用 `internal/engine/organize` 的独立 `Snapshot`/`Config`，不复用 `scan.enumerate`；其 `MinSize` 语义为 `size < MinSize` 跳过，与扫描的下限边界一致。
- 本次不给整理器新增最大大小/扩展名白名单（不在需求范围），也不改动其现有语义，避免两边分叉。
- 规则导入（`organize_rules.go`）只处理 `OrganizeSettings`，不包含扫描过滤字段；定时任务（`schedule.go`）通过 `ScanSettings` 复用本次新增字段，已纳入改动。

## 8. 需要确认

请确认以上语义与改动范围后，我再开始落盘实现、测试、构建与单一提交。
