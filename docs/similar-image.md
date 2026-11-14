# 相似图片判定（M3）

本文记录相似图片判定的实现口径、与原版的一致性状态，以及 `skipUniform` 退化指纹规则。

## 1. 支持格式

| 扩展名 | 解码器 | 与原版（GDI+）一致性 |
| --- | --- | --- |
| jpg / jpeg | 标准库 `image/jpeg`（含 EXIF Orientation 1–8 归一） | 逐位一致 |
| png | 标准库 `image/png` | 逐位一致 |
| gif | 标准库 `image/gif`（取首帧） | 逐位一致 |
| bmp | 本包自实现（24/32 位） | 逐位一致 |
| tif / tiff | `golang.org/x/image/tiff`（纯 Go） | 与 GDI+ 生成的样本对拍一致 |
| webp | `golang.org/x/image/webp`（纯 Go） | **原版不支持**（GDI+ 对 webp 一律解码失败），Go 侧为超集能力，无法对拍 |

不允许 HEIC/RAW：原版也不支持，且没有纯 Go 解码器。

## 2. 指纹算法

1. 按 EXIF Orientation(274) 归一：5–8 时先按行列互换的尺寸绘制，再旋转/镜像回正。
2. 缩放到 9×8、白底、24 位画布。缩小的轴用面积平均（复刻 GDI+ 预滤波），放大的轴用双线性；
   被缩放的轴最外 1 像素叠加约 `0.129` 的白（实测 GDI+ 边缘抗锯齿）。
3. 8 行 × 8 列，灰度 `(R*299+G*587+B*114)/1000` 取整，左侧大于右侧时置位，`bit = row*8+col`。
4. 分组：按路径（忽略大小写）升序，贪心锚点 + 组内直径约束——新成员与锚点、与每个已加入
   成员的汉明距离都必须 `<= 阈值`（默认 8）。

已知退化差异：`1×1` 等极小图放大到 9×8 时 GDI+ 会整体掺白（C# 得 `0x0f0f…`，Go 得 `0x0101…`），
已在对拍中登记。其余样本逐位一致。

## 3. 退化（均匀）指纹规则 `skipUniform`

### 3.1 问题

纯色图归一化后每行相邻灰度相等，理论上 dHash 全 0。但白边会让最左（及最右/上下）一列变亮，
实际指纹恒定落在 `0x0101010101010101` 一类，纯白图还会因浮点截断得到 `0x2828282828282828`。
结果是互不相关的红色、蓝色、黑色纯色图被判成"相似"，形成假阳性组。

### 3.2 判定

`image.Compute` 在算指纹的同时返回 `Uniform`：

- 取归一化画布 **去掉 1 像素白边后的内部区域**（9×8 画布即 x∈[1,7]、y∈[1,6]）；
- 若内部灰度极差 `<= 1`，判定为退化指纹（纯色、纯色条、没有行内明暗变化的图）；
- 判定不看指纹数值，因此不受白边与浮点截断影响。

### 3.3 开关

| 位置 | 名称 | 默认 |
| --- | --- | --- |
| CLI | `fdedup scan --image-skip-uniform` | 关闭 |
| JSON 请求 | `similarImage.skipUniform` | 关闭（`false`） |

默认关闭＝与原版完全一致（纯色图仍会被合并）。默认值最终由产品口径决定，改动时只需改默认值，
无需改算法。

### 3.4 日志与输出

开启后，被排除的图片不进入相似分组，并在结果 `skipped` 中逐条登记：

```json
{ "path": "...", "code": "uniform_fingerprint",
  "message": "图片无行内明暗变化（纯色图/纯色条等退化指纹），按 skipUniform 规则不参与相似分组" }
```

`fdedup scan` 的文本摘要会额外打印一行：`退化指纹未参与相似分组：N 个（--image-skip-uniform）`。

注意：这些文件仍参与**精确查重**（`--exact`），规则只影响相似图片分组。

## 4. 对拍与验证

- `parity similar --csharp <dump.json> --root <dir>`：逐文件指纹/时长对拍；`-allow <relpaths>`
  登记已知差异（`04-images/corrupt.png`、`04-images/tiny-1x1.png`）。
- `parity scan`：扫描结果对拍，相似组集合、成员、可回收字节逐项比较。
- 单测：`internal/engine/similar/image`（纯色/渐变退化判定、TIFF↔PNG 一致性、WebP 解码与截断容错、
  分组拆组、EXIF、BMP）、`internal/engine/similar/audio`。

### 4.1 多语料对拍（2026-11-14）

四批语料，C# 侧统一用 `FileDedupTool.exe --dump-similar-json <out> --root <dir>` 导出基线：

| 批次 | 语料目录 | 一致 / 总数 | 差异 |
| --- | --- | --- | --- |
| A | `%TEMP%\m3corpus`（98 文件：18 图 11 音频） | 图 16/18，音频 11/11 | 2 条已登记：`corrupt.png`（C# 有指纹、Go 解码失败）、`tiny-1x1.png`（`0f0f…` vs `0101…`） |
| B | `%TEMP%\m3fmt`（2 tif + 3 webp） | 2/5 | 3 条已登记：webp 为 Go 超集 |
| C | `%TEMP%\m3uniform`（4 张纯色 png） | 4/4 | 无 |
| D | `%TEMP%\m3fmt-scan`（B + photo.png） | 3/6 | 3 条已登记：webp 为 Go 超集 |
| E | `testdata/out/uispike`（2 张 1024px 真实 PNG，含大量透明像素） | 0/2 | 2 条已登记，汉明距离均为 10，原因见 4.3 |

命令（以 A 为例）：

```powershell
go run ./tools/parity similar --csharp "$env:TEMP\m3-csharp-similar.json" `
  --root "$env:TEMP\m3corpus" -allow "04-images/corrupt.png,04-images/tiny-1x1.png"
```

扫描层对拍（`-ignore imageKey,skipped,cache`）：m3corpus 仅 tiny-1x1 引起 6 条；m3fmt-scan 仅 WebP 组引起 7 条。

WebP 的 3 条差异不可消除：GDI+ 不支持 WebP，C# 一律解码失败，因此只能登记为 Go 侧超集能力。

### 4.2 已修复：纯白图指纹偏差

C 批次曾暴露一处新差异：纯白图 C# 得 `0000000000000000`，Go 得 `2828282828282828`。
原因是面积平均得到 `254.9999…`，被灰度公式截断成 254，凭空造出梯度位。
现在 `normalize` 末尾把 9×8 画布通道就近取整（GDI+ 全程是 8 位整数），偏差消除且 A 批次结果不变。

### 4.3 未解决：大比例缩小时与 GDI+ 的位级差异

E 批次暴露 2 条新差异（各 10 位）：

| 文件 | C# | Go |
| --- | --- | --- |
| `build/appicon.png`（1024×1024） | `011397970f2b2b03` | `039296968a2a2b01` |
| `frontend/src/assets/images/logo-universal.png`（1024×811） | `63431f1f1f1f1f3c` | `674318561f1f1d30` |

已用实验排除透明通道：把两张图先按白底压平成不透明 PNG 后重新对拍，C# 与 Go 的指纹都一字不变，
说明两边都把透明像素合成为白底，差异不在 alpha。

剩余解释是**大比例缩小（约 114:1）时 GDI+ 的预滤波核**：本实现用面积平均（box），
GDI+ `HighQualityBilinear` 的缩小路径更接近按比例展宽的三角核。比例越大差异越明显，
这解释了为什么 m3corpus 里的小图逐位一致、而 1024px 素材差 10 位。

影响：默认阈值 8，10 位不会被判为相似，因此这两张（本来也不是近重复图）的分组结果不变。
但如果真实场景里存在"同一张大图的不同版本"，可能出现 C# 判为一组、Go 判为两组。

可选取舍（待定）：
1. 按现状登记为已知差异（对拍口径：分组一致率 + 距离分布，与本路线 M3 的既定口径一致）；
2. 改造缩小路径为展宽三角核，尝试逐位对齐 GDI+，代价是可能扰动当前已逐位一致的 16 张小图样本。

## 5. 纯色图合并：默认值待定

按当前默认（`skipUniform=false`），纯色图仍会与其他无梯度图片合并为一组，与原版行为一致。
是否改为默认开启由产品口径决定；确定后只需把 CLI 开关默认值与 `similarImage.skipUniform`
的缺省值改为 `true`，规则本身与对拍基线不受影响。

