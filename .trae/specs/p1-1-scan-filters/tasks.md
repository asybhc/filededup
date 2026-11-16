# P1-1 扫描过滤条件任务清单

状态：待确认后执行
提交：单一提交 `feat(scan): 支持大小上下限与扩展名白名单过滤并统计跳过数（0.9.10）`

## 任务

- [ ] T1 引擎契约与归一化
  - [ ] `internal/engine/model/scan.go`：`MaxSize`、`Extensions`、`SkippedBySize`、`SkippedByExtension`
  - [ ] 新增 `NormalizeExtensions` / `ExtensionAllowed` / `ValidateExtensions`
  - [ ] `validate.go`：负值、上下限颠倒、非法扩展名校验
  - [ ] schema v1 增加可选字段
  - [ ] 模型层单测

- [ ] T2 枚举与扫描主流程
  - [ ] `enumerate.go`：统一大小/扩展名判定，普通文件与链接分支共用，返回过滤计数
  - [ ] `scan.go`：归一化请求、写入统计、取消路径保持计数
  - [ ] `filters_test.go`：边界、扩展名变体、组合、留空、计数、请求记录

- [ ] T3 桌面桥接与设置
  - [ ] `app.go`：版本 0.9.10、`ScanRequest`/`ScanState` 字段、`StartScan` 校验与映射
  - [ ] `settings.go`：字段、默认值、归一化与提示
  - [ ] `schedule.go`：定时任务透传
  - [ ] 相关单测

- [ ] T4 CLI
  - [ ] `main.go`：`--max-size`、`--ext`、帮助文本、参数校验与退出码 2

- [ ] T5 导出
  - [ ] `export.go`：summary 字段、HTML 扫描参数与汇总、CSV 列与行长度

- [ ] T6 前端
  - [ ] `types.ts`：ScanRequest/ScanSettings/ScanState
  - [ ] `App.tsx`：文本输入、解析防呆、提示、持久化、结果 chips
  - [ ] `mock.ts`：默认设置与 mock 过滤语义

- [ ] T7 文档与验证
  - [ ] `docs/ui-shell.md`：0.9.10、过滤说明、验证结果、未验证项
  - [ ] `go vet -tags desktop ./...`
  - [ ] `go test -tags desktop ./...`
  - [ ] `npm run build`
  - [ ] `scripts/build-desktop.ps1` 产出 `bin\fdedup-ui.exe`
  - [ ] 记录 exe 大小与 SHA-256
  - [ ] 单一提交并推送 main
  - [ ] 停下报告，不进入 P1 下一项
