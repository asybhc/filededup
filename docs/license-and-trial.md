# 授权、机器码与试用

本文对应 `internal/auth/` 与 `cmd/fdedup-keygen/`。三条线并行：

| 线 | 作用 | 能否签发 |
| --- | --- | --- |
| Ed25519 许可证 | 新许可证（`FDLIC1:...`） | 能，用 `fdedup-keygen` |
| 旧 ECDSA 许可证 | 与 C# 版通用的旧许可证 | **只读兼容**，不再签发 |
| 试用记录 | 首次运行起 3 天全功能 | 客户端自动维护 |

## 机器码

两种许可证绑同一个机器码，算法逐位复刻 C# 版，因此同机同码：

```
parts = ["g=" + 小写去空白(MachineGuid), "v=" + 系统盘卷序列号(8 位大写十六进制)]
        # MachineGuid 读不到时改用：
        # ["c=" + CPU 名称|标识, "b=" + 主板厂商|型号, "v=" + 卷序列号, "n=" + 计算机名]
机器码 = CrockfordBase32(SHA256("FileDedupTool|v1|" + join(parts, "\n"))[:10])   # 16 位，展示时每 4 位加 `-`
```

- `MachineGuid`：`HKLM\SOFTWARE\Microsoft\Cryptography`（64 位视图）。
- 卷序列号：`%SystemRoot%` 所在盘符的根，`GetVolumeInformation` 的卷序列号，`%08X`。
- 归一化：`Trim` → 小写 → 删除所有空白（与 C# 的 `A()` 一致）。
- Crockford 字母表 `0123456789ABCDEFGHJKMNPQRSTVWXYZ`；展示格式如 `CT6P-PBAJ-C9HY-S546`。

用户可在授权面板一键复制；命令行为 `fdedup-keygen machine-code`。

## Ed25519 许可证

### 文本格式

```
FDLIC1:<base64url(payload JSON)>:<base64url(signature)>
```

- `base64url` 为 RFC 4648 §5 无填充（Go 的 `base64.RawURLEncoding`）。
- 签名输入是 `"FDLIC1:" + payload 段`（ASCII 字节）；签名 64 字节。
- 校验时忽略空格、换行、制表符与 UTF-8 BOM，允许邮件/聊天工具折行。

### 载荷

```json
{
  "v": 1,
  "app": "filededup-go",
  "machine": "CT6PPBAJC9HYS546",
  "exp": "2027-11-15",
  "issued": "2026-11-15",
  "edition": "pro",
  "features": ["scan", "apply", "organize", "search"],
  "id": "0A1B2C3D4E5F"
}
```

| 字段 | 规则 |
| --- | --- |
| `v` | 必须为 1 |
| `app` | 必须为 `filededup-go` |
| `machine` | 16 位机器码（可带连字符），或 `*` 表示不绑定机器 |
| `exp` | `YYYY-MM-DD`；`forever` / `never` / 空 表示永久 |
| `edition` | `pro` 或 `standard`，缺省 standard（旧许可证没有该字段时按标准版处理，不会失效） |
| `features` | 功能位子集，见下表；缺省或全为空时按 `edition` 给默认功能集 |

版本与默认功能集（`internal/auth/license.go` 的 `featureDefs` / `DefaultFeatures`）：

| 版本 | 默认功能位 | 说明 |
| --- | --- | --- |
| 标准版 `standard` | `scan` 重复文件扫描、`apply` 删除执行、`organize` 九步整理、`search` 本地搜索 | 旧许可证与旧 ECDSA 许可证缺 features 时按这套 |
| 专业版 `pro` | 标准版全部 + `cloud` 网盘上传、`cloud-dedup` 网盘查重、`ai` AI 分析建议、`schedule` 定时任务 | 显式 features 仍然优先 |

到期按**本地日期**比较，到期日当天仍可用（`ExpiredOn` 只在日期更大时返回 true）。
功能位是白名单：没有 `apply` 就不能执行删除，没有 `organize` 就不能真实整理；`scan` 始终可用。
未解锁的功能不会从界面消失：授权面板列出全部功能与解锁所需版本，调用被拒绝时错误里带功能名、
所需版本与本机机器码。

### 签名与密钥

- 客户端只内置 **Ed25519 公钥**（`internal/auth/license.go` 的 `embeddedEd25519PublicKeyB64`）。
- 私钥只存在于签发机器：本仓库外的 `2026-11-13/go-port/outputs/license-keys/ed25519-private.pem`
  （已在 `.gitignore` 的上级目录之外，不会被提交）。私钥丢失只能换钥匙并重新发布客户端。
- 换钥匙流程：`gen-keypair` → 用新公钥替换 `embeddedEd25519PublicKeyB64` → 重新构建客户端；
  已签发的旧公钥许可证随之失效，需重签。

### 签发流程

```powershell
# 1) 生成密钥对（只在签发机执行一次）
go run ./cmd/fdedup-keygen gen-keypair -out ..\outputs\license-keys

# 2) 生成许可证（机器码来自用户反馈，或 -any 用于演示/不绑定）
go run ./cmd/fdedup-keygen issue -key ..\outputs\license-keys\ed25519-private.pem `
    -machine CT6P-PBAJ-C9HY-S546 -days 365 -edition pro -out ..\outputs\license-keys\demo.lic

# 3) 自检（用内置公钥验签 + 本机机器码）
go run ./cmd/fdedup-keygen inspect -license ..\outputs\license-keys\demo.lic
```

`-forever`、`-exp YYYY-MM-DD`、`-features scan,apply`、`-id` 等参数见 `fdedup-keygen` 的 usage。
不传 `-features` 时按 `-edition` 的默认功能集签发（`standard` / `pro`）；传了则只认列出的功能位。
私钥也可以用环境变量 `FDEDUP_LICENSE_KEY` 指定，避免出现在命令行历史里。
签发机应离线或至少与发布产物隔离；不要用仓库工作副本里的私钥签名正式许可证。

### 导入与存放

- 文件位置：`%AppData%\FileDedup\license.fdlic`（目录 0700、文件 0600，临时文件 + `Rename` 原子替换）。
- 导入方式：授权面板的“选择文件”（`*.lic;*.fdlic;*.txt`，≤64 KB）或粘贴文本。
- **先验签再落盘**：签名、应用标识、机器码任一不通过都直接报错，不会写入半份许可证。
- 启动时读取该文件；无效/过期只写入 `warning` 供界面展示，不会删除用户的文件。

## 旧 ECDSA 许可证（只读兼容）

- 文本：Crockford Base32（容忍连字符、`I/L→1`、`O→0`）。解码后为 `payload || 64 字节 P1363 签名`。
- 载荷 17 或 18 字节：
  - `[0]=2` 类型；`[1..4]` 应用编号 `20261101`（小端）；`[5..14]` 机器码 10 字节；
  - `[15..16]` 自 2024-01-01 起的天数（小端），`0xFFFF` 为永久；第 18 字节 `1` 表示 pro。
- 验证顺序：ECDSA P-256 / SHA-256 验签（签名覆盖 payload）→ 类型 → 应用编号 → 机器码 → 由界面统一判到期。
- 公钥为 SPKI base64（`legacyPublicKeySPKI`）。**不再签发**：`fdedup-keygen` 没有旧格式输出；
  历史私钥不随仓库分发。
- 局限：只用临时 P-256 密钥与合成 payload 做了单元测试，**未用真实旧许可证样本验证**（私钥不在手边）。

## 试用记录

与 C# 版共用同一份存储与同一套密钥材料，切换版本不会重置试用，也不会让 C# 版认不出记录。

- 存储（三处，取最严）：
  - `%AppData%\FileDedupTool\trial.dat`（写入后加隐藏属性）；
  - 同目录 `trial.dat.bak`；
  - `HKCU\Software\AI大叔\FileDedupTool.Trial` 的值 `Trial`。
- 记录：`FDTR1|start|last|flagStarted|flagRollback|HMAC`，日期为 `O` 格式（往返格式）。
- HMAC：`HMAC-SHA256(key, 除最后一段外的全部文本)`，十六进制大写无连字符。
- key：
  - 新记录 `PBKDF2-HMAC-SHA256("FileDedupTool.Trial|" + 机器码 10 字节的大写连字符十六进制 + "|7a3c9e5b", 固定盐, 10000 次, 32 字节)`；
  - 读到旧记录（`SHA256(password)` 作 key 能验过）时按旧 key 验，下次写盘升级为新 key。
- 合并规则（严格优先）：任一来源 HMAC 有效即参与合并；取**最早** `start`、**最晚** `last`，两个 flag 取或。
- 回拨：`now < max(start,last) - 2min` 时置 rollback 并立即写回；记录被标记 rollback 视为**已污染**。
- 结果：
  - 从 `start` 起 3 天（`now < start+3d`）为试用中，剩余天数向上取整并夹在 1..3；
  - 超过 3 天为试用到期；
  - 有来源存在但全部验签失败、或记录被标记回拨 → 视为已污染，按试用到期处理（不放行）。
- 启动时 `TouchTrial()` 创建/刷新记录；`LoadState()` 只读不写。

## 剩余风险（如实说明）

1. 授权判定在客户端完成，能挡住普通用户改系统时间、删文件重置试用，挡不住逆向/打补丁。
   真要更强只能上服务端在线校验，本版没有做。
2. 试用记录写在用户可写位置，权限足够时仍可整体回滚到早期快照；双存储 + 回拨检测只是提高成本。
3. `%AppData%` 目录名沿用了 `FileDedupTool`（试用）与 `FileDedup`（设置/许可证），是刻意为之；
   卸载安装包不会删除它们，避免“卸载重装 = 重置试用”。
