# 依赖清单（M0 冻结）

记录 Go 侧依赖与取舍依据。原则：

1. `internal/engine/**` 只允许标准库（由 `internal/engine/arch_test.go` 用 AST 强制），保证引擎可独立编译、可跨平台、可确定性复现；
2. 任何第三方依赖必须纯 Go（`CGO_ENABLED=0` 可构建），不引入 cgo 与系统库；
3. 引入新依赖必须在本文件登记"为什么不用标准库"，并在 CI 三平台通过。

## 1. 当前（M0–M3）

| 模块 | 版本 | 用途 | 引入原因 / 替代方案 | 作用域 |
| --- | --- | --- | --- | --- |
| Go | 1.27.0 | 工具链 | `go.mod` 固定；语义化 `max/min` 内建、`os.Root` 等 | 全部 |
| `golang.org/x/text` | v0.44.0 | `unicode/norm`（NFC 归一化） | 标准库无 Unicode 归一化；跨平台路径必须统一 NFC，否则 macOS(APFS/NFD) 与 Windows 会产生不同的清单哈希 | 工具 + 后续引擎路径层 |
| `golang.org/x/image` | v0.48.0 | `webp`、`tiff` 解码（相似图片 dHash） | 标准库 `image/*` 不含 WebP/TIFF；这两个子包纯 Go、`CGO_ENABLED=0` 可构建。原版 GDI+ 支持 TIFF、不支持 WebP，Go 侧因此是 WebP 的超集能力（已登记） | engine（`similar/image`，由 `arch_test` 白名单放行） |
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.3 | JSON Schema v1 校验 | 手写校验无法覆盖 `additionalProperties`/`oneOf`/枚举；仅用于契约测试，不进入产品二进制 | 测试 |

`go.sum` 中的 `github.com/dlclark/regexp2` 为 jsonschema 的传递依赖，未直接使用。

## 2. 计划引入（按里程碑）

| 里程碑 | 模块 | 用途 | 说明 |
| --- | --- | --- | --- |
| M1 | 标准库 `crypto/sha256`、`hash` | 内容哈希 | 不引入 xxhash：对拍要求 SHA-256 逐字节一致 |
| M1 | 标准库 `encoding/json` | 输出 | 不引入第三方 JSON，避免字段顺序/转义差异影响对拍 |
| M2 | `golang.org/x/sys/windows` | IFileOperation 回收站、属性读写 | 需 cgo 之外的原生调用，`x/sys` 是官方扩展 |
| M3 | `golang.org/x/image/webp`、`golang.org/x/image/tiff` | WebP/TIFF 解码 | **M3 已引入**（v0.48.0，见上表）；HEIC/RAW 不做（原版也不支持） |
| M5 | `github.com/wailsapp/wails/v2` | 桌面壳 | **M0 spike 结论为准**；不通过则回退 Fyne |
| M5 | React + TypeScript + Vite + TanStack Virtual | 前端 | 仅构建期依赖，产物为静态资源 |
| M6 | `fyne.io/systray` 或 `getlantern/systray` | 托盘 | M6 冻结；Linux 走 StatusNotifier，无托盘时降级 |
| M6 | `modernc.org/sqlite` | 设置/缓存/操作清单 | 纯 Go（无 cgo）；若体积或性能不达标，M1 压测后改用自有文件格式 |
| M8 | 标准库 `crypto/ecdsa`、`crypto/ed25519` | 许可证 | 不引入第三方 |
| M9 | 标准库 `crypto/ed25519` | 清单签名 | 更新包验签 |

## 3. 明确不引入

| 依赖 | 原因 |
| --- | --- |
| 任何 cgo 库（sqlite3、chromaprint、ImageMagick） | 破坏"引擎纯 Go"与三平台交叉构建；音频指纹增强默认关，若要做也放到独立可选组件 |
| Wails v3（alpha） | 未稳定，M0 只评估 v2 |
| 第三方日志库 | `log/slog` 足够，且日志不参与对拍 |
| 第三方测试框架 | 标准库 `testing` + golden 文件足够 |

## 4. 构建约束

- 引擎与 CLI：`CGO_ENABLED=0 go build ./...` 必须通过（CI 校验）。
- 桌面壳：Windows 需 WebView2 Runtime（Win11 自带；Win10 由安装包检测并提示）；macOS 需 Xcode Command Line Tools；Linux 需 `libwebkit2gtk-4.1-dev`、`libgtk-3-dev`（M5 在 CI 固化版本）。
- 版本锁定：CI 用 `go-version-file: go.mod`；Wails/Fyne 版本在 M5 写入 `docs/ui-spike.md` 并锁定。
