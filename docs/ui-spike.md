# M0 UI spike 结论

## 1. 结论

**桌面壳采用 Wails v2（v2.17.1）+ React 18/TypeScript/Vite**，不启用 Fyne 回退。
理由：Windows 构建与运行在 1m05s 内完成，产物 12.0 MB（未压缩、含 WebView2 引导）；中文与输入法正常；WebView2 已在目标系统内置（Win11）/由安装器检测（Win10）；与"引擎纯 Go、UI 用 Web"的架构一致，Claude 侧只暴露 Wails binding，不污染 `internal/engine`。

## 2. 实测记录（Windows 11 x64，本机）

| 项目 | 结果 |
| --- | --- |
| `wails version` | v2.17.1 |
| `wails doctor` | WebView2 Runtime 已安装；Node v22.14.0 / npm 10.9.2；Go 1.27.0 |
| `wails init -n uispike -t react-ts` | 成功（模板拉取需联网） |
| `wails build -clean` | 成功，耗时 1m04.7s |
| 产物 | `uispike.exe` = 12,015,104 B（12.0 MB） |
| 运行 | 正常启动，无控制台窗口 |
| 中文渲染 | 正常（文件名、路径、按钮、输入框均无豆腐块） |
| 300 行长表格 | 无卡顿；`position: sticky` 表头正常 |
| 窗口缩放/最大化 | 布局跟随，无空白/闪烁 |
| 截图 | `testdata/out/uispike-windows.png`、`testdata/out/uispike-windows-max.png` |

spike 工程位于 `testdata/out/uispike`（已被 `.gitignore` 忽略，不进入产品代码）。

## 3. 未验证项（需在 CI/真机补）

| 项目 | 原因 | 补法 |
| --- | --- | --- |
| macOS 构建与截图 | 本机无 macOS；Wails 不支持从 Windows 交叉编译 macOS 包 | 仓库初始化后由 `macos-latest` runner 跑 `wails build`，截图用 `screencapture` |
| Linux 构建与截图 | 同上；需 `libwebkit2gtk-4.1-dev`/`libgtk-3-dev` | `ubuntu-latest` 安装依赖后构建；截图用 `xwd`/`import` |
| 输入法候选框定位 | 需人工在真实窗口点击验证 | M5 建立真实 UI 骨架后人工验收 |

## 4. 对路线的影响

1. 桌面壳依赖锁定 `github.com/wailsapp/wails/v2 v2.17.1`；升级需重跑本 spike 四项检查。
2. 前端构建产物内嵌进 exe，`fdedup` CLI 与 `fdedupd` 服务壳不依赖前端（headless 部署不受影响）。
3. Wails 的 binding 只允许出现在 `internal/api` 上层（`cmd/filededup`），`internal/engine/arch_test.go` 已禁止引擎引用 Wails，保持不变。
4. macOS/Linux 的构建失败风险集中在系统库版本（webkit2gtk 4.0 vs 4.1），M5 在 CI 固定 Ubuntu 22.04 镜像并记录包名。
