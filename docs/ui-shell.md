# 桌面 UI 外壳（Wails v2 + React/TS）

首版标记：`0.6.0-ui`。目标是先用一版能点的界面把现有引擎接起来，功能范围见“已接通 / 未实现”。

## 构建与运行

```powershell
cd web && npm install && npm run build      # 产出 web/dist
cd .. && go build -tags desktop -o bin/fdedup-ui.exe ./cmd/filededup
./bin/fdedup-ui.exe
```

- 不带 `desktop` tag 时，`cmd/filededup` 只有一个占位 main，三平台 CI 的 `go build ./...` 不会引入 WebView2 / WebKitGTK。
- 前端产物通过模块根的 `assets_desktop.go`（`//go:embed all:web/dist`）嵌入，因为 `go:embed` 不能引用上级目录。
- 浏览器预览：`cd web && npm run dev`，然后打开 `http://localhost:5173/`。
  没有 Wails 桥时会走 `src/mock.ts` 的合成后端，页面顶部显示“浏览器预览（合成数据）”。
  `?demo=results|preview|done|perf` 可直达对应步骤（只在非 Wails 环境生效）。

## 目录

| 路径 | 作用 |
| --- | --- |
| `cmd/filededup/app.go` | 绑定到前端的门面：扫描、分页、预览、执行、状态轮询 |
| `cmd/filededup/main_desktop.go` | Wails 入口（`//go:build desktop`） |
| `cmd/filededup/app_smoke_test.go` | 后端接线冒烟测试（`-tags desktop`） |
| `web/src/App.tsx` | 五步状态机与全部页面 |
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

界面首屏底部固定展示这两行，改功能时同步改这里，不要留下“点了没反应”的入口。

- 已接通：目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志。
- 未实现：系统托盘、自动更新、授权与试用、蓝奏云上传、自定义保留项、执行撤销、撤销历史。

## 已知限制

1. 桌面窗口在无桌面会话的自动化环境里无法弹出（官方 Wails 模板同样表现），桌面显示由人工复核；
   后端用 `go test -tags desktop -run TestAppSmoke ./cmd/filededup` 覆盖真实目录的扫描→预览→执行闭环。
2. 结果列表一次性载入全部组（分页 500 逐步拉取），超大结果集的内存占用未单独测量。
3. 预览只展示前 200 条动作样例，完整计划在执行时才交给执行器。
