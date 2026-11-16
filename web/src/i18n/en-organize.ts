// 九步整理与按时间整理的英文词条。
// 键为中文原文（含全角标点），动态部分用 {name} 占位。
const enOrganize: Record<string, string> = {
  // 九步整理：保留规则
  "保留路径最短": "Shortest path",
  "保留最新修改": "Newest",
  "保留最早修改": "Oldest",
  "保留最大文件": "Largest",
  "保留最短文件名": "Shortest file name",

  // 九步整理：整理方式
  "按文件类型分类": "By file type",
  "按后缀名整理（扩展名大写）": "By extension (uppercase)",
  "按文件名字符特征（正则）": "By file name pattern (regex)",
  "按文件名关键字": "By file name keyword",
  "按指定文件夹": "Into a fixed folder",

  // 九步整理：冲突策略
  "自动重命名 name (1).ext": "Rename automatically as name (1).ext",
  "跳过并计数": "Skip and count",
  "保留已存在目标，删除源文件": "Keep existing target and delete source",

  // 按时间整理：分桶粒度与冲突策略（后端 GetTimelineMeta 返回的 label）
  "按日（2024-03-08）": "By day (2024-03-08)",
  "按周（2024-W10）": "By week (2024-W10)",
  "按月（2024-03）": "By month (2024-03)",
  "按年（2024）": "By year (2024)",
  "移动（源目录不再保留）": "Move (source folder is not kept)",
  "复制（源目录保留）": "Copy (source folder is kept)",
  "改名保留（默认）": "Rename and keep both (default)",
  "跳过同名文件": "Skip files with the same name",
  "覆盖同名文件": "Overwrite files with the same name",

  // 九步整理：动作类型
  "改名": "Rename",
  "移动": "Move",
  "硬链接": "Hardlink",
  "去只读": "Clear read-only",
  "删空目录": "Remove empty folders",
  "建目录": "Create folders",
  "建议（仅报告）": "Suggest (report only)",
  "复制": "Copy",
  "移动/复制": "Move/Copy",
  "（回收站）": " (Recycle Bin)",

  // 九步整理：计数器
  "特殊删除": "Special delete",
  "冲突跳过": "Conflict skip",
  "整理跳过": "Organize skip",
  "内容重复删除": "Content dupes deleted",
  "相似图片删除": "Similar images deleted",
  "音频删除": "Audio deleted",
  "空目录清理": "Empty folders cleaned",
  "排除": "Excluded",
  "隐藏/系统跳过": "Hidden/system skipped",
  "存疑保留": "Suspects kept",
  "硬链接合并": "Hardlinks merged",
  "无计数变化": "No counter changes",

  // 执行顺序
  "预览顺序（先去重后整理）": "Preview order (dedup before organize)",
  "独立顺序（先改名后去重）": "Standalone order (rename before dedup)",

  // 顶部与目录选择
  "九步整理": "Nine-step organize",
  "仅规划结果": "Plan only",
  "试运行结果": "Dry-run result",
  "真实执行结果": "Real run result",
  "尚未选择要整理的目录": "No folder selected",
  "整理根目录": "Organize root folder",
  "选择目录": "Select folder",

  // 目标目录与整理参数
  "整理目标目录（留空 = 根目录；可填绝对路径放在源目录之外）":
    "Target folder (empty = root; an absolute path outside the source folder is allowed)",
  "例如 整理结果（根内）或 D:\\整理输出（根外）":
    "e.g. organized (inside root) or D:\\output (outside root)",
  "目标可在源目录之外；同卷按重命名移动，跨卷改为复制并保留源文件。":
    "The target may be outside the source folder; same volume = rename-move, cross volume = copy and keep the source.",
  "整理方式（第五步）": "Organize mode (step 5)",
  "关键字（逗号/分号/空格分隔）": "Keywords (comma/semicolon/space separated)",
  "正则表达式": "Regular expression",
  "例如 ^\\d{4}-": "e.g. ^\\d{4}-",
  "指定文件夹名": "Folder name",
  "冲突策略": "Conflict policy",
  "删除方式（特殊删除与去重删除）": "Deletion mode (special and dedup deletes)",
  "移入回收站（可恢复）": "Move to Recycle Bin (recoverable)",
  "永久删除（不可恢复）": "Delete permanently (unrecoverable)",
  "内容去重保留规则": "Content dedup keep rule",
  "相似图片保留规则": "Similar images keep rule",
  "0 = 指纹完全相同；数值越大判定越宽松，越容易把不同的图片当成相似；范围 0-16，默认 8":
    "0 = identical fingerprint; higher values match more loosely and treat different images as similar; range 0-16, default 8",
  "相似图片阈值（0 = 完全相同，越大越宽松；默认 8，上限 16）":
    "Similar-image threshold (0 = identical, higher = looser; default 8, max 16)",
  "音频时长容差（秒）": "Audio duration tolerance (seconds)",
  "垃圾识别强度（0 低 / 1 中 / 2 高）": "Junk detection strength (0 low / 1 medium / 2 high)",
  "最小文件大小（MB，0 = 不限）": "Minimum file size (MB, 0 = no limit)",
  "排除目录（按目录名匹配；逗号或空格分隔）":
    "Excluded folders (match by folder name; comma or space separated)",
  "例如 node_modules .git": "e.g. node_modules .git",
  "只对目录生效：目录名相同即跳过该目录及其内容，不按文件名或部分子串过滤。":
    "Applies to folders only: a matching folder name skips that folder and its contents; file names and partial substrings are not filtered.",
  "保护词（跳过改名与特殊删除判定）": "Protected words (skip rename and special-delete checks)",
  "包含隐藏/系统文件（不勾选 = 跳过隐藏与系统文件）":
    "Include hidden/system files (unchecked = skip them)",
  "去重时建立硬链接（同卷，替代删除）":
    "Create hardlinks on dedup (same volume, instead of deleting)",
  "出错后继续执行剩余动作": "Continue with remaining actions after errors",
  "导出规则": "Export rules",
  "导入规则": "Import rules",
  "规则文件带版本号；导入会拒绝未知字段与非法值，失败时不改动当前设置，也不会改变当前根目录。":
    "Rule files are versioned; import rejects unknown fields and invalid values, leaves current settings untouched on failure, and never changes the root folder.",

  // 步骤开关
  "执行顺序与步骤开关": "Order and step toggles",
  "顺序来自引擎，两套顺序只差“去重三步”的位置":
    "Order comes from the engine; the two plans differ only in where the three dedup steps run",
  "无预览计划 · 独立顺序": "No preview plan · standalone order",
  "有预览计划 · 先去重后整理": "With preview plan · dedup first",
  "当前执行顺序：": "Current order: ",
  "，已启用 {n} / 9 步。开关只影响是否执行该步，不改变顺序。":
    " — {n} of 9 steps enabled. Toggles only control whether a step runs; the order stays fixed.",
  "分析存疑项（不改文件）": "Analyze suspects (no changes)",
  "试运行（不改文件）": "Dry run (no changes)",
  "执行（会改文件）": "Run (changes files)",
  "正在执行，按钮暂时禁用。": "Running; buttons are disabled.",
  "未授权：整理执行已禁用。": "Unlicensed: organize runs are disabled.",
  "试运行只创建目标目录，不移动、不删除、不修改属性；真实执行会按上面的动作落盘。两者都必须先经过确认。":
    "A dry run only creates target folders; nothing is moved, deleted or re-attributed. A real run applies the actions above. Both require confirmation first.",

  // 撤销
  "撤销上次整理": "Undo last organize",
  "可撤销 {n} 条记录": "{n} records can be undone",
  "暂无可撤销记录": "No records to undo",
  "任务": "Task",
  "；根目录": "; root",
  "；开始时间 {time}。": "; started at {time}.",
  "真实执行过一次整理后，这里会出现最近一次的撤销入口。":
    "After a real organize run, the latest undo entry appears here.",
  "可回滚：改名、移动、新建目录、只读属性。不可回滚：已删除文件（回收站或永久删除）与硬链接替换；这些项会在结果里单独计数，不会假装恢复成功。":
    "Restorable: renames, moves, created folders, read-only attributes. Not restorable: deleted files (Recycle Bin or permanent) and hardlink replacements; these are counted separately instead of pretending to restore.",
  "正在撤销…": "Undoing…",
  "上次撤销：恢复 {restored} · 跳过 {skipped} · 不可恢复 {unrestorable} · 失败 {failed}（共 {total} 条）":
    "Last undo: restored {restored} · skipped {skipped} · unrestorable {unrestorable} · failed {failed} ({total} total)",

  // 结果概览
  "整理结果": "Organize result",
  "顺序": "Order",
  "试运行（未落盘）": "Dry run (nothing written)",
  "真实执行": "Real run",
  "耗时": "Elapsed",
  "阶段：": "Stage: ",
  "准备中": "Preparing",
  "（试运行）": " (dry run)",
  "（真实执行）": " (real run)",
  "整理目标在源目录之外：": "Target is outside the source folder: ",
  "跨卷：将复制文件，原文件仍保留在源目录（不会被移走）。":
    "Cross volume: files are copied; the originals stay in the source folder.",
  "同卷：将重命名式移动，原文件会离开源目录。":
    "Same volume: files are rename-moved; the originals leave the source folder.",
  "无法判断是否同卷：执行时按实际卷逐条判断，跨卷复制、同卷移动。":
    "Volume relation unknown: decided per file at run time (copy across volumes, move within one).",
  "这是仅规划结果：只做了扫描与规划，没有创建目录、没有移动、没有删除、没有修改任何文件。":
    "Plan-only result: scanning and planning only; no folders were created, nothing was moved, deleted or modified.",
  "这是试运行结果：只创建了目标目录，文件没有移动、没有删除、没有改属性。下面的动作数与计数是规划结果。":
    "Dry-run result: only target folders were created; no files were moved, deleted or re-attributed. The counts below are planned values.",
  "这是真实执行结果：动作已落盘。": "Real run result: the actions have been applied.",
  "删除项已移入回收站。": "Deleted items were moved to the Recycle Bin.",
  "删除项已永久删除，不可恢复。": "Deleted items were permanently removed.",
  "规划动作": "Planned actions",
  "执行总数": "Executed",
  "跳过": "Skipped",

  // 存疑项
  "存疑项（{n}）": "Suspects ({n})",
  "：逐条确认或驳回后再执行": ": confirm or reject each item before running",
  "：默认全部保留": ": all kept by default",
  "将删除": "Will delete",
  " · 建议 {value}": " · Suggest {value}",
  " · 得分 {value}": " · Score {value}",
  "确认删除": "Confirm delete",
  "保留（驳回）": "Keep (reject)",
  "全选确认": "Confirm all",
  "全部保留": "Keep all",
  "执行并删除已确认项（{n}）": "Run and delete confirmed ({n})",
  "只有标为“确认删除”的存疑项会在下一次真实执行中删除；未确认或被驳回的一律保留。":
    'Only suspects marked "confirm delete" are removed on the next real run; unconfirmed or rejected items are all kept.',
  "本次扫描没有发现存疑项。": "No suspects found in this scan.",

  // 明细
  "九步明细": "Nine-step details",
  "未执行": "Not run",
  "已规划": "Planned",
  "{n} 个动作": "{n} actions",
  "失败明细（{n}）": "Failures ({n})",
  "动作样例（前 200 条，共 {total} 条，显示 {shown} 条）":
    "Action samples (first 200 of {total}; {shown} shown)",
  "没有需要展示的动作。": "No actions to display.",

  // 确认弹窗
  "确认执行九步整理": "Confirm nine-step organize",
  "根目录：": "Root: ",
  "整理目标：": "Target: ",
  "（根目录）": "(root folder)",
  "执行顺序：": "Order: ",
  "；删除方式：": "; deletion mode: ",
  "移入回收站": "Move to Recycle Bin",
  "永久删除": "Delete permanently",
  "；去重改为建硬链接": "; dedup creates hardlinks",
  "存疑项：将删除": "Suspects: will delete",
  "个已确认项；其余存疑项一律保留。": "confirmed items; all other suspects are kept.",
  "将执行以下步骤（按当前顺序）：": "The following steps will run (current order):",
  "我明白永久删除不可恢复，并已备份重要数据":
    "I understand permanent deletion is irreversible and my important data is backed up",
  "确认执行": "Run",
  "确认撤销上次整理": "Confirm undo of last organize",
  "将把最近一次真实整理中可还原的动作反向执行：改名/移动还原、新建目录删除、只读属性恢复。":
    "The latest real organize run will be reversed where possible: renames/moves restored, created folders removed, read-only attributes restored.",
  "已删除文件（回收站或永久删除）与硬链接替换无法由本程序还原，会在结果里如实计入“不可恢复”。":
    'Deleted files (Recycle Bin or permanent) and hardlink replacements cannot be restored by this app; they are reported as "unrestorable".',
  "确认撤销": "Undo",

  // 功能状态
  "本页功能状态（如实标注）": "Feature status on this page (as implemented)",
  "九步开关与两套执行顺序、试运行、真实执行、执行前确认、去重/改名/特殊删除/去只读/整理/清理空目录、冲突策略、回收站与永久删除、排除与大小过滤、结果明细、存疑项逐条确认或驳回并进入执行、整理后撤销（最近一次）、规则导入导出（版本化 JSON，未知字段与非法值校验）、配置方案预设（同名覆盖、不含根目录）、规则测试器（样例纯计算：改名/特殊判定/整理去向）":
    "Nine-step toggles and both orders, dry run, real run, pre-run confirmation, dedup/rename/special delete/clear read-only/organize/empty-folder cleanup, conflict policies, Recycle Bin and permanent delete, excludes and size filter, result details, per-item suspect confirm/reject feeding into the run, undo of the latest organize, versioned JSON rule import/export with unknown-field and invalid-value validation, named configuration presets (overwrite by name, root folder excluded), and a rule tester (pure calculation on samples: rename/junk verdict/organize destination)",
  "限制": "Limitations",
  "撤销只保留最近一次真实整理；已删除文件与硬链接替换无法还原。":
    "Only the latest real organize run can be undone; deleted files and hardlink replacements cannot be restored.",
  "macOS / Linux 的回收站、机器码、单实例已按平台实现并完成交叉编译验证，但尚未在真机运行。":
    "Recycle Bin, machine ID and single instance are implemented per platform and cross-compiled on macOS / Linux, but not yet run on real hardware.",

  // 错误与提示
  "步骤顺序加载失败：{error}": "Failed to load step order: {error}",
  "读取整理进度失败：{error}": "Failed to read organize progress: {error}",
  "规则已导出：{path}（{bytes} 字节）": "Rules exported: {path} ({bytes} bytes)",
  "导出规则失败：{error}": "Failed to export rules: {error}",
  "规则已导入：{path}（当前根目录保持不变）": "Rules imported: {path} (root folder unchanged)",
  "导入规则失败：{error}": "Failed to import rules: {error}",
  "撤销失败：{error}": "Undo failed: {error}",

  // 步骤标题与阶段（后端原文，按字符串精确匹配）
  "第一步：去除文件名前缀序列号（含中间序列号）":
    "Step 1: strip leading serial numbers (including mid-name ones)",
  "第二步：去除文件名后缀序列号": "Step 2: strip trailing serial numbers",
  "第三步：删除特殊文件（保护层优先；确定垃圾自动删；存疑项需人工确认，未确认一律保留）":
    "Step 3: delete junk files (protections first; definite junk is deleted automatically; suspects need manual confirmation and are kept otherwise)",
  "第四步：去除所有文件只读属性": "Step 4: clear the read-only attribute on all files",
  "内容去重": "Content dedup",
  "相似图片去重": "Similar-image dedup",
  "音频相似去重（实验性）": "Similar-audio dedup (experimental)",
  "第五步：文件整理": "Step 5: file organizing",
  "清理空文件夹": "Remove empty folders",
  "枚举目录": "Enumerating folders",
  "生成预览计划（先去重后整理）": "Building preview plan (dedup first, then organize)",
  "执行动作": "Applying actions",

  // 配置方案预设
  "配置方案预设": "Configuration presets",
  "预设保存当前整理规则（不含根目录），可一键切换；校验口径与导出规则相同。":
    "A preset stores the current organize rules (without the root folder) for one-click switching; validation matches the exported rules.",
  "预设名称": "Preset name",
  "保存为预设": "Save as preset",
  "刷新预设": "Refresh presets",
  "同名预设会被覆盖（不区分大小写）。": "A preset with the same name is overwritten (case-insensitive).",
  "读取预设失败：{error}": "Failed to read presets: {error}",
  "请输入预设名称": "Enter a preset name",
  "预设已保存：{name}": "Preset saved: {name}",
  "保存预设失败：{error}": "Failed to save the preset: {error}",
  "已应用预设：{name}（当前根目录保持不变）": "Preset applied: {name} (the current root folder is unchanged)",
  "预设已删除：{name}": "Preset deleted: {name}",
  "删除预设失败：{error}": "Failed to delete the preset: {error}",
  "还没有保存的预设。": "No presets saved yet.",
  "启用 {n} 个步骤": "{n} steps enabled",
  "预设文件：{path}": "Preset file: {path}",
  "删除": "Delete",

  // 规则测试器
  "规则测试器": "Rule tester",
  "对样例文件名做纯计算：不读磁盘、不改文件，只用当前页面的开关与规则。":
    "Runs a pure calculation on sample file names: no disk reads, no file changes, only the switches and rules on this page.",
  "样例文件名（每行一个，可带路径）": "Sample file names (one per line, paths allowed)",
  "例如：001-2024-03-08-照片.jpg": "For example: 001-2024-03-08-photo.jpg",
  "运行测试": "Run test",
  "测试结果只反映当前配置，不代表已执行任何改动。":
    "The test only reflects the current configuration; no changes were made.",
  "请输入至少一个样例文件名": "Enter at least one sample file name",
  "测试失败：{error}": "Test failed: {error}",
  "样例 {total} 个：改名 {renamed}，垃圾 {garbage}，存疑 {suspects}，保留 {kept}，保护词排除 {excluded}，可整理 {organized}，跳过 {skipped}。":
    "{total} samples: {renamed} renamed, {garbage} junk, {suspects} suspect, {kept} kept, {excluded} protected, {organized} organizable, {skipped} skipped.",
  "原名": "Original name",
  "改名后": "After renaming",
  "特殊文件": "Junk verdict",
  "整理去向": "Organize destination",
  "最终路径": "Final path",
  "未启用": "Not enabled",
  "垃圾文件（将被删除）": "Junk (will be deleted)",
  "存疑（默认保留，需人工确认）": "Suspect (kept by default, needs confirmation)",
  "保留": "Keep",
  "命中保护词（排除）": "Matched a protected word (excluded)",
  "已在特殊删除步骤删除": "Deleted in the junk-file step",
  "整理步骤未启用": "Organize step is off",
  "未选择整理目标": "No organize destination selected",
  "未填写关键字": "No keyword entered",
  "未命中任一关键字": "Matched no keyword",
  "未填写正则": "No regular expression entered",
  "正则无效": "Invalid regular expression",
  "未命中正则": "Did not match the regular expression",

  // 按时间整理
  "按时间整理": "Organize by time",
  "按文件的拍摄时间（JPEG/TIFF 的 EXIF 优先，取不到时用修改时间）分到日、周、月、年目录，支持移动或复制；同名冲突按所选策略处理。预演只读元数据，不会改动任何文件。":
    "Sort files into day, week, month and year folders by capture time (JPEG/TIFF EXIF first, falling back to modified time), using move or copy; name conflicts follow the selected policy. Preview only reads metadata and changes nothing.",
  "源目录": "Source folder",
  "尚未选择源目录": "No source folder selected",
  "按时间整理源目录": "Time organize source folder",
  "目标目录（容器目录，程序会在其中建日/周/月/年子目录）":
    "Target folder (container; day/week/month/year subfolders are created inside)",
  "尚未选择目标目录": "No target folder selected",
  "按时间整理目标目录": "Time organize target folder",
  "分桶粒度": "Bucket size",
  "处理方式": "Action",
  "同名冲突": "Name conflict",
  "包含隐藏/系统文件（默认跳过）": "Include hidden/system files (skipped by default)",
  "预演计划": "Preview plan",
  "开始整理": "Start",
  "共 {total} 个文件，可整理 {ready} 个，冲突 {conflicts} 个，跳过 {skipped} 个。":
    "{total} files total, {ready} ready, {conflicts} conflicts, {skipped} skipped.",
  "整理中：{done} / {total}": "Organizing: {done} / {total}",
  "（{current}）": " ({current})",
  "已完成：移动 {moved}，复制 {copied}，覆盖 {overwrote}，失败 {failed}":
    "Done: moved {moved}, copied {copied}, overwrote {overwrote}, failed {failed}",
  "执行失败": "Execution failed",
  "：{error}": ": {error}",
  "失败明细（前 10 条）：": "Failures (first 10):",
  "我确认按上面的计划": "I confirm that, per the plan above,",
  "{count} 个文件（{size}），冲突策略为「{policy}」。":
    '{count} files ({size}) will be processed; conflict policy "{policy}".',
  "目标": "Destination",
  "就绪": "Ready",
  "冲突": "Conflict",
  "预览条目过多，仅显示前 {n} 条；执行时会处理全部文件。":
    "Too many preview rows; showing the first {n}. All files are processed on run.",
};

export default enOrganize;
