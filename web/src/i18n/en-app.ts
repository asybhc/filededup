// App 外壳与查重流程的英文词条。
const enApp: Record<string, string> = {
  // 授权状态摘要（模块级函数 licenseLabel / licenseSourceLabel 中使用）
  "Ed25519 许可证": "Ed25519 license",
  "旧版 ECDSA 许可证（只读兼容）": "Legacy ECDSA license (read-only)",
  试用期: "Trial",
  授权状态读取中: "Loading license status",
  "已授权 · {edition} · 永久": "Licensed · {edition} · perpetual",
  "已授权 · {expiry} 到期": "Licensed · expires {expiry}",
  "试用中 · 剩余 {days} 天": "Trial · {days} days left",
  "试用已结束 · 未授权": "Trial ended · unlicensed",

  // 启动、拖放、保存提示
  "设置读取失败，已使用默认值：{error}": "Failed to load settings; using defaults: {error}",
  "拖放处理失败：{error}": "Drop handling failed: {error}",
  "设置保存失败：{error}": "Failed to save settings: {error}",
  "定时扫描：{message}": "Scheduled scan: {message}",
  已结束: "Finished",

  // 自动更新提示
  "正在检查更新…": "Checking for updates…",
  "检查失败：{error}": "Check failed: {error}",
  "更新源必须是 http:// 或 https:// 地址。": "The update source must be an http:// or https:// URL.",
  "正在保存并检查…": "Saving and checking…",
  "检查完成。": "Check complete.",
  "已跳过版本 {version}。": "Skipped version {version}.",
  "已跳过版本 {version}，后续检查不再提示。": "Skipped version {version}; future checks will not prompt.",
  "跳过版本失败：{error}": "Failed to skip version: {error}",
  "已取消跳过版本。": "Version skip cancelled.",
  "取消跳过失败：{error}": "Failed to cancel skip: {error}",
  "正在下载、验签并替换程序，请勿关闭窗口…": "Downloading, verifying, and replacing the program; do not close the window…",
  "已更新到 {version}，程序即将重启。备份：{backup}": "Updated to {version}; the app will restart. Backup: {backup}",
  "安装已取消：{error}": "Installation cancelled: {error}",
  "未配置（不检查）": "Not configured (no check)",
  "内置默认（GitHub {repo} 最新发布）": "Built-in default (latest GitHub release of {repo})",
  "自动，每 {hours} 小时": "Automatic, every {hours} h",
  "取消跳过 {version}": "Unskip {version}",
  "发现新版本 v{version}": "New version available: v{version}",
  "（{size}）": " ({size})",
  "确认覆盖当前程序并重启": "Confirm: overwrite current program and restart",
  "下载并安装": "Download and install",
  "将下载安装包并覆盖当前程序；替换前会备份为 .old，长度、SHA-256、清单签名或包签名任一不符都会拒绝安装并回滚。":
    "The installer will be downloaded and will overwrite the current program. Before replacing, it is backed up as .old; any mismatch in length, SHA-256, manifest signature, or package signature rejects the install and rolls back.",
  "清单与安装包都必须通过内置的更新专用 Ed25519 公钥验签，并校验长度与 SHA-256；失败一律拒绝安装，没有“忽略签名继续”的选项。":
    "Both the manifest and the installer must verify against the built-in update-only Ed25519 public key and pass the length and SHA-256 checks. Any failure rejects the install; there is no “ignore the signature and continue” option.",

  // 扫描、导出、撤销、许可证操作提示
  "授权状态读取失败：{error}": "Failed to load license status: {error}",
  扫描失败: "Scan failed",
  当前环境不支持全盘查重: "Full-disk dedup is not supported in this environment",
  "每组至少要保留一个文件：已阻止把整组都勾选删除。":
    "Each group must keep at least one file: selecting the whole group for deletion was blocked.",
  "反转后会删除整组，已自动留出「{name}」保留。":
    "Inverting would delete the whole group, so “{name}” was kept automatically.",
  "请先勾选要保留的文件，再点「仅保留所选」。": "Check the files to keep first, then click “Keep selected only”.",
  "已保留勾选的 {kept} 个文件，其余 {rest} 个改为待删除。":
    "Kept {kept} checked files; the other {rest} are now marked for deletion.",
  "反选后有 {count} 组会变成整组删除，已自动留出每组一个保留项。":
    "Inverting would delete {count} whole groups; one file per group was kept automatically.",
  "已导出 {format}：{path}": "Exported {format}: {path}",
  "导出失败：{error}": "Export failed: {error}",
  "打开所在位置失败：{error}": "Failed to show the file location: {error}",
  "回收站已被清空，删除的文件无法自动恢复。":
    "The Recycle Bin has been emptied; deleted files cannot be restored automatically.",
  "有 {count} 个文件未能恢复，原因见下方逐条说明。":
    "{count} files could not be restored; see the per-item reasons below.",
  "撤销失败：{error}": "Undo failed: {error}",
  "许可证导入成功。": "License imported successfully.",
  "未识别到有效许可证。": "No valid license found.",
  "导入失败：{error}": "Import failed: {error}",
  "机器码已复制。": "Machine ID copied.",
  "复制失败，请手动选中机器码复制。": "Copy failed; select the machine ID and copy it manually.",

  // 顶部标题与工具条
  "文件整理.一杯好茶": "File Organizer. A Cup of Tea",
  "本地扫描 · 回收站可恢复 · 云端上传为可选":
    "Local scan · recoverable via Recycle Bin · cloud upload optional",
  "浏览器预览（合成数据）": "Browser preview (synthetic data)",
  "查看授权状态、机器码与许可证导入": "View license status, machine ID, and license import",
  "设置：外观、托盘、定时任务、AI、日志": "Settings: appearance, tray, scheduled tasks, AI, logs",
  "帮助（快捷键 F1）": "Help (F1)",
  "帮助 F1": "Help F1",
  "查看日志与环境自检": "View logs and environment diagnostics",
  "检查更新（清单与安装包均验签）": "Check for updates (manifest and installer are both signature-verified)",
  "有更新 v{version}": "Update available: v{version}",
  "检查更新": "Check for updates",
  "：": ": ",
  去授权: "Get a license",
  模块: "Modules",
  查重与清理: "Dedup & Cleanup",
  九步整理: "9-Step Organizer",
  按时间整理: "Timeline Organizer",
  蓝奏云上传: "Lanzou Upload",
  本地搜索: "Local Search",
  "当前流程：{steps}": "Current flow: {steps}",
  流程: "Flow",

  // 未授权提示
  "未授权：整理的真实执行已禁用，请先导入许可证；试运行仍可用。":
    "Unlicensed: real execution in the organizer is disabled; import a license first. Dry runs still work.",
  "未授权：按时间整理的真实执行已禁用，预演仍可用。":
    "Unlicensed: real execution in the timeline organizer is disabled; previews still work.",
  "未授权：开始上传会被拒绝，登录与预检仍可用。":
    "Unlicensed: starting an upload will be rejected; login and pre-checks still work.",
  "未解锁：{reason}（本机机器码 {code}）。": "Locked: {reason} (machine ID {code}).",
  本地搜索需要标准版许可证: "Local search requires a Standard license",
  读取中: "Reading…",
  "正在读取授权状态，请稍候。": "Loading license status, please wait.",

  // 扫描参数
  "1. 选择目录与扫描设置": "1. Choose folder and scan options",
  "全盘：{volumes}": "Full disk: {volumes}",
  "尚未选择目录（也可把文件夹拖到这里）": "No folder selected yet (you can also drop a folder here)",
  扫描目录: "Scan folder",
  选择目录: "Choose folder",
  保留策略: "Keep policy",
  删除方式: "Deletion method",
  "移入回收站（可恢复）": "Move to Recycle Bin (recoverable)",
  "永久删除（不可恢复）": "Delete permanently (unrecoverable)",
  "最小文件大小（MB，0 = 不限）": "Minimum file size (MB, 0 = no limit)",
  "排除目录（目录名或路径前缀；逗号或空格分隔）":
    "Excluded folders (folder name or path prefix; comma- or space-separated)",
  "例如 node_modules .git": "e.g. node_modules .git",
  "全盘查重（扫描所有本地固定磁盘，自动跳过系统目录与卷根系统文件）":
    "Full-disk dedup (scan all local fixed disks, skipping system folders and volume-root system files)",
  "64KB 快筛（先用前 64KB 指纹滤掉不可能重复的文件，候选项仍做完整 SHA-256 复核）":
    "64KB fast filter (use a first-64KB fingerprint to rule out files that cannot be duplicates; candidates still get full SHA-256 verification)",
  "（相似图片开启时不可用）": " (unavailable while similar-image detection is on)",
  "相似图片（dHash）": "Similar images (dHash)",
  "0 = 指纹完全相同；数值越大判定越宽松，越容易把不同的图片当成相似；范围 0-16，默认 8":
    "0 = identical fingerprints; larger values are more lenient and more likely to treat different images as similar; range 0-16, default 8",
  "相似阈值（0 = 完全相同，越大越宽松；默认 8，上限 16）":
    "Similarity threshold (0 = identical, larger = more lenient; default 8, max 16)",
  "相似音频（时长容差）": "Similar audio (duration tolerance)",
  "音频容差（秒）": "Audio tolerance (seconds)",
  "包含隐藏/系统文件（不勾选 = 跳过隐藏与系统文件）":
    "Include hidden/system files (unchecked = skip hidden and system files)",
  "排除项只匹配目录：填目录名会跳过所有同名目录及其内容，填带路径的项按路径前缀匹配；不会按文件名子串过滤。":
    "Exclusions match folders only: a folder name skips every folder with that name and its contents, and an entry with a path matches by path prefix. File names are never matched by substring.",
  "全盘范围：{volumes}。强制排除 {count} 项：Windows、Program Files、Program Files (x86)、ProgramData、$Recycle.Bin、System Volume Information、Recovery 等系统目录，以及各卷根下的 pagefile.sys、hiberfil.sys、swapfile.sys。这些项不受上方排除设置影响。":
    "Full-disk scope: {volumes}. {count} items are always excluded: Windows, Program Files, Program Files (x86), ProgramData, $Recycle.Bin, System Volume Information, Recovery, and other system folders, plus pagefile.sys, hiberfil.sys, and swapfile.sys at each volume root. These are not affected by the exclusions above.",
  "{path}（可用 {free}）": "{path} ({free} free)",
  "快筛只减少需要完整哈希的文件数量；最终分组与保留/删除判断仍以完整 SHA-256 结果为准。":
    "The fast filter only reduces how many files need full hashing; final grouping and keep/delete decisions still use the full SHA-256 result.",
  "相似图片默认关闭，勾选后才参与查重。阈值 0 表示指纹完全相同；数值越大判定越宽松，更容易把不同的图片当成相似（误判风险更高），数值越小越严格；可调范围 0-16，默认 8。":
    "Similar-image detection is off by default and only runs when enabled. A threshold of 0 means identical fingerprints; larger values are more lenient and more likely to flag different images as similar (higher false-positive risk), while smaller values are stricter. Range 0-16, default 8.",
  "设置自动保存到 {path}，重启后自动恢复。":
    "Settings are saved automatically to {path} and restored on restart.",
  "（{note}）": " ({note})",
  永久: "Perpetual",
  开始扫描: "Start scan",

  // 扫描中与结果
  "2. 扫描中": "2. Scanning",
  全盘查重: "Full-disk dedup",
  "全盘查重：所有本地固定磁盘": "Full-disk dedup: all local fixed disks",
  "3. 查重结果": "3. Results",
  "导出包含分组、成员、大小、保留策略与扫描参数的报告":
    "Export a report with groups, members, sizes, keep policy, and scan options",
  "导出 HTML": "Export HTML",
  "导出 CSV": "Export CSV",
  "请先勾选要删除的文件": "Check the files to delete first",
  "生成执行预览": "Generate execution preview",
  重复组: "Duplicate groups",
  重复文件: "Duplicate files",
  可回收: "Reclaimable",
  跳过: "Skipped",
  查看跳过项: "View skipped items",
  "性能自检：当前是 {rows} 行合成数据（≥ 10 万），不是真实扫描结果。滚动帧率约 {fps} fps（1 秒采样）。":
    "Performance self-test: {rows} rows of synthetic data (≥ 100k), not a real scan result. Scroll rate is about {fps} fps (1-second sample).",
  退出自检: "Exit self-test",
  "正在载入结果：{loaded} / {total} 组": "Loading results: {loaded} / {total} groups",
  "共 {total} 组，已全部载入；点击任意行查看完整路径。":
    "{total} groups loaded; click any row to see the full path.",
  "性能自检：载入 10 万行合成数据": "Performance self-test: load 100k synthetic rows",
  "勾选 = 删除，未勾选 = 保留；已勾选 {count} 项，执行范围以勾选为准，不再按整组删除。":
    "Checked = delete, unchecked = keep; {count} items checked. Execution follows your selections, not whole-group deletion.",
  "全选可删除项": "Select all deletable",
  "筛选：路径或文件名包含…": "Filter: path or file name contains…",
  筛选结果: "Filter results",
  按类型筛选: "Filter by type",
  排序方式: "Sort order",
  默认顺序: "Default order",
  按文件大小: "By file size",
  按可回收空间: "By reclaimable space",
  按保留项文件名: "By kept file name",
  按修改时间: "By modified time",
  "切换升序/降序": "Toggle ascending/descending",
  "降序 ↓": "Descending ↓",
  "升序 ↑": "Ascending ↑",
  "显示 {shown} / {total} 组（筛选只影响显示，勾选与执行范围不变）":
    "Showing {shown} / {total} groups (filtering affects display only; selections and execution scope are unchanged)",
  "选择 / 状态": "Select / Status",
  "文件 / 组": "File / Group",
  完整路径: "Full path",

  // 执行预览
  "4. 执行预览": "4. Execution preview",
  返回结果: "Back to results",
  "未授权，请先在“授权”中导入许可证": "Unlicensed — import a license in “License” first",
  确认执行: "Confirm execution",
  "未授权：确认执行已禁用，只能扫描与预览。请先在“授权”中导入许可证。":
    "Unlicensed: execution is disabled; only scanning and preview work. Import a license in “License” first.",
  "机器码：{code}": "Machine ID: {code}",
  "本次只处理结果页勾选的 {count} 个文件；同一组未勾选的副本会保留。":
    "Only the {count} files checked on the results page are processed; unchecked copies in the same group are kept.",
  "永久删除不可恢复，也不会进入回收站。请确认已备份重要数据。":
    "Permanent deletion is unrecoverable and does not go to the Recycle Bin. Make sure important data is backed up.",
  动作总数: "Total actions",
  涉及组: "Groups involved",
  永久删除: "Delete permanently",
  回收站: "Recycle Bin",
  "正在执行：{count} 个动作，请勿关闭窗口。": "Running: {count} actions; do not close the window.",
  "动作样例（前 200 条，共 {count} 条）": "Sample actions (first 200 of {count})",

  // 执行完成与撤销
  "5. 执行完成": "5. Done",
  回滚: "Rolled back",
  释放空间: "Space freed",
  撤销上次删除: "Undo last deletion",
  "可撤销 {count} 个文件": "{count} files undoable",
  暂无可撤销记录: "Nothing to undo",
  "批次时间 {time} · 删除方式 {mode}（只保留最近一次批次）。":
    "Batch time {time} · deletion method {mode} (only the latest batch is kept).",
  移入回收站: "Move to Recycle Bin",
  "明细只展示前 200 条。": "Only the first 200 entries are listed.",
  "来源目录 {dir}": "Source folder {dir}",
  "文件修改时间 {time}": "File modified {time}",
  "另有 {count} 个文件为永久删除：{reason}。":
    "{count} more files were permanently deleted: {reason}.",
  "永久删除未进入回收站，无法撤销":
    "Permanently deleted files never entered the Recycle Bin and cannot be restored",
  "正在撤销…": "Undoing…",
  "撤销上次删除（恢复到原路径）": "Undo last deletion (restore original paths)",
  "撤销会逐条检查：目标位置已有同名文件时不覆盖；原目录不存在时先重建目录。永久删除与已清空的回收站都无法恢复，界面会如实标注失败原因。":
    "Undo checks each file: existing files at the destination are never overwritten, and missing folders are recreated first. Permanent deletions and an emptied Recycle Bin cannot be restored; the UI reports each failure as it is.",
  "真实执行过一次“移入回收站”的删除后，这里会出现最近一次的撤销入口；记录跨重启仍可读取。":
    "After one real “move to Recycle Bin” deletion, the latest undo entry appears here; the record survives restarts.",
  "上次撤销：恢复 {restored} · 失败 {failed} · 不可撤销 {unrestorable}（共 {total} 条）":
    "Last undo: {restored} restored · {failed} failed · {unrestorable} unrestorable (of {total})",
  "执行日志已写入用户配置目录下的 {path}。删除后的文件不在结果列表中刷新，建议重新扫描确认。":
    "The execution log was written to {path} in the user config folder. Deleted files are not refreshed out of the result list; run a new scan to confirm.",

  // 详情抽屉与跳过列表
  路径详情: "Path details",
  "正在解码…": "Decoding…",
  重新加载预览: "Reload preview",
  "加载大图预览（≤1600px）": "Load large preview (≤1600px)",
  "预览：{path}": "Preview: {path}",
  "原图 {width} × {height} · 解码格式 {format}（预览已按 EXIF 方向校正）":
    "Original {width} × {height} · decoded as {format} (preview is EXIF-orientation corrected)",
  "扫描期间跳过（前 200 项）": "Skipped during scan (first 200)",
  "没有跳过项。": "No skipped items.",

  // 确认对话框
  "即将对 {count} 个文件执行{mode}，预计释放 {size}。":
    "About to process {count} files ({mode}), freeing an estimated {size}.",
  "移入系统回收站；执行完成后可在本页“撤销上次删除”中恢复到原路径。":
    "Files move to the system Recycle Bin; afterwards you can restore them to their original paths via “Undo last deletion” on this page.",
  "永久删除不可恢复，也不会进入回收站。": "Permanent deletion is unrecoverable and does not go to the Recycle Bin.",
  "我明白永久删除不可恢复，并已备份重要数据":
    "I understand permanent deletion is unrecoverable and my important data is backed up",
  "未授权：确认执行已禁用。请先关闭此窗口，在“授权”中导入许可证后重试。":
    "Unlicensed: execution is disabled. Close this window, import a license in “License”, then retry.",
  确认撤销上次删除: "Confirm undo",
  "将把 {count} 个文件从回收站恢复到原路径。":
    "{count} files will be restored from the Recycle Bin to their original paths.",
  "目标位置已有同名文件时会跳过并给出原因，不会覆盖你后来新建的文件；原目录不存在时会先重建目录。此操作无法撤销。":
    "Files are skipped with a reason when a same-name file already exists at the destination, so files you created later are never overwritten; missing folders are recreated first. This action cannot be undone.",
  确认撤销: "Confirm undo",

  // 帮助
  帮助与快捷键: "Help & shortcuts",
  "打开本帮助（任何界面都可用）": "Open this help (works anywhere)",
  "收起最上层浮层：帮助 → 更新 → 授权 → 设置 → 跳过列表 → 撤销确认 → 删除确认":
    "Close the topmost overlay: Help → Update → License → Settings → Skipped list → Undo confirmation → Delete confirmation",
  拖放: "Drag & drop",
  "在「选择目录」步骤把文件夹拖到路径框区域，等同于点击「选择目录」；拖入单个文件会改用其所在目录。":
    "On the “Choose folder” step, dropping a folder on the path field is the same as clicking “Choose folder”; dropping a single file uses its containing folder.",
  "查重流程：选择目录与设置 → 扫描 → 预览分组 → 勾选要删除的文件 → 确认执行（移入回收站可撤销，永久删除不可恢复）。所有删除范围以勾选为准，未勾选的文件一律保留。":
    "Dedup flow: choose folder and options → scan → preview groups → check files to delete → confirm execution (Recycle Bin is undoable, permanent deletion is not). Every deletion follows your selections; unchecked files are always kept.",
  "九步整理、网盘上传、本地搜索各有独立页签；具体参数与限制见对应页面的说明文字。":
    "The 9-step organizer, Lanzou upload, and local search each have their own tab; see the notes on each page for options and limits.",

  // 授权对话框
  "功能解锁状态": "Feature unlock status",
  已解锁: "Unlocked",
  "{edition}解锁 · {reason}": "{edition} unlocks this · {reason}",
  未解锁: "Locked",
  "{edition} · {reason}": "{edition} · {reason}",
  "{start} 至 {end}": "{start} to {end}",
  "试用期从首次启动算起共 3 天，剩余 {days} 天；到期后删除执行与整理执行会被禁用。":
    "The trial lasts 3 days from first launch, with {days} days left; when it ends, deletion and organizer execution are disabled.",
  或粘贴许可证文本: "Or paste license text",
  "FDLIC1:… 或旧版 Base32 许可证": "FDLIC1:… or a legacy Base32 license",
  导入粘贴内容: "Import pasted text",
  "未授权时仍可扫描与预览，但删除执行和整理执行会被禁用，并在界面上明确提示，不会静默降级。":
    "Without a license you can still scan and preview, but deletion and organizer execution are disabled and clearly indicated in the UI — no silent degradation.",

  // 更新对话框
  软件更新: "Software update",
  手动: "Manual",
  未配置更新源: "No update source configured",
  手动检查模式: "Manual check mode",
  检查间隔未到: "Check interval not reached",
  已跳过此版本: "This version is skipped",

  // 扫描进度
  "正在启动扫描…": "Starting scan…",
  "正在取消…": "Cancelling…",
  "已取消（结果不完整）": "Cancelled (incomplete results)",
  正在计算哈希: "Hashing",
  正在枚举文件: "Enumerating files",
  阶段: "Phase",
  已发现文件: "Files found",
  已读取: "Read",
  用时: "Elapsed",
  "哈希进度 {a} / {b}（{x} / {y}）": "Hashing {a} / {b} ({x} / {y})",
  "枚举阶段的总量未知，进度条为不确定状态；取消会保留已扫描的部分结果。":
    "The total is unknown during enumeration, so the bar is indeterminate; cancelling keeps partial results.",
  取消扫描: "Cancel scan",

  // 结果行
  "删 {checked}/{total}": "Del {checked}/{total}",
  "重复组 · {count} 个文件": "Duplicate group · {count} files",
  "策略保留：{name}": "Kept by policy: {name}",
  "把当前勾选项视为要保留的文件，其余成员改为待删除":
    "Treat currently checked files as kept and mark the other members for deletion",
  仅保留所选: "Keep selected only",
  "可回收 {size}": "Reclaimable {size}",
  "取消删除 {name}": "Keep {name}",
  "标记删除 {name}": "Mark {name} for deletion",
  将删除: "Delete",

  // 保留策略与类型标签
  最短路径优先: "Shortest path first",
  最新修改优先: "Newest modified first",
  最早修改优先: "Oldest modified first",
  最短文件名优先: "Shortest file name first",
  内容相同: "Identical content",
  相似图片: "Similar images",
  相似音频: "Similar audio",
  预览执行: "Preview & execute",

  // 功能状态
  "功能状态（如实标注）": "Feature status (stated as it actually is)",
  "目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、查重结果导出（HTML/CSV）、九步整理（试运行/真实执行/存疑项确认/最近一次撤销/规则导入导出）、自动更新（Ed25519 清单与安装包验签、备份与回滚）、Ed25519 许可证导入与状态显示、旧版 ECDSA 许可证只读兼容、试用期（PBKDF2 + 双存储 + 回拨检测）":
    "Directory scan; content/image/audio dedup; paged and virtualized results; Recycle Bin and permanent deletion; pre-execution confirmation; execution stats; execution log; persisted settings; dedup export (HTML/CSV); 9-step organizer (dry run/real execution/uncertain-item confirmation/latest undo/rule import-export); auto-update (Ed25519 manifest and installer verification, backup and rollback); Ed25519 license import and status display; legacy ECDSA license read-only compatibility; trial period (PBKDF2 + dual storage + clock-rollback detection)",
  "蓝奏云上传页（登录/目录浏览/预检/上传门面已接通，真实列表与上传需要蓝奏账号验证）":
    "Lanzou upload page (login/folder browsing/pre-check/upload facade connected; real listing and upload still need a Lanzou account test)",
  "系统托盘、自定义保留项、查重侧执行撤销、撤销历史":
    "System tray, custom keep rules, dedup-side execution undo, undo history",
};

export default enApp;
