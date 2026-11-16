// 蓝奏云上传页的英文词条。
const enLanzou: Record<string, string> = {
  // 页头与平台
  "蓝奏优享上传": "Lanzou Premium upload",
  "蓝奏云上传（老版协议）": "Lanzou Cloud upload (legacy protocol)",
  "已登录 · {account}": "Signed in · {account}",
  "掩码未返回": "masked ID unavailable",
  "未登录": "Not signed in",
  "网盘类型": "Cloud drive",
  "蓝奏云（老版协议，单文件 ≤ 100 MiB）": "Lanzou Cloud (legacy protocol, ≤ 100 MiB per file)",
  "蓝奏优享（ilanzou，分片上传）": "Lanzou Premium (ilanzou, chunked upload)",
  "两种网盘各自独立登录、各自保存凭据与上传缓存；切换网盘不会退出另一种的登录态。":
    "Each drive signs in independently and keeps its own credentials and upload cache; switching drives does not sign you out of the other.",
  "优享不支持 Cookie 登录、从 C# 版导入与创建云端目录。":
    "Lanzou Premium does not support Cookie sign-in, importing from the C# app, or creating cloud folders.",
  "账号": "Account",
  "登录来源": "Sign-in source",
  "凭据加密": "Credential encryption",
  "DPAPI 加密存储": "DPAPI encrypted",
  "当前平台不可用": "Unavailable on this platform",
  "保存时间": "Saved at",
  "凭据文件：{path}": "Credential file: {path}",
  "（读取中）": "(loading)",
  "登录态仅在本机用 DPAPI（CurrentUser）加密保存，不保存密码，也不写明文日志。":
    "Sign-in state is encrypted with DPAPI (CurrentUser) on this machine only; no passwords are stored and no plain-text logs are written.",
  "是否为 DPAPI 加密以本机后端返回为准；界面不假设凭据已加密。":
    "Whether DPAPI encryption is in effect comes from the local backend; the UI does not assume credentials are encrypted.",
  "未联调声明": "Not integration-tested",
  "：": ": ",
  "优享协议字段来自对官方客户端的静态分析，登录、列目录、上传与判重都需要真实优享账号验证；分片上传的 URL 形态等存疑项已列在":
    "The Premium protocol fields come from static analysis of the official client. Sign-in, folder listing, uploads, and duplicate detection all need verification with a real Premium account; open questions such as the chunked-upload URL shape are listed in",
  "的「未验证项」，不作结论。分享创建不在本功能内，请到网页端手动操作。":
    "under “Unverified items”; no conclusions are drawn. Folder sharing is not part of this feature; use the web UI.",
  "登录成功后的真实目录列表、上传与云端判重都需要蓝奏账号验证。本页只保证编译、单测与本地可验证部分。分享创建不在本功能内，请到网页端手动操作。":
    "Real folder listing after sign-in, uploads, and cloud duplicate detection all require verification with a Lanzou account. This page only covers compilation, unit tests, and what can be verified locally. Folder sharing is not part of this feature; use the web UI.",

  // 登录
  "登录": "Sign in",
  "账号密码只用于本次登录请求，不落盘":
    "Credentials are used only for this sign-in request and are never written to disk",
  "蓝奏优享账号": "Lanzou Premium account",
  "蓝奏云账号": "Lanzou Cloud account",
  "手机号或邮箱": "Phone number or email",
  "密码": "Password",
  "只用于本次登录，不保存": "Used only for this sign-in; not saved",
  "正在登录…": "Signing in…",
  "正在导入…": "Importing…",
  "从 C# 版导入": "Import from C# app",
  "正在退出…": "Signing out…",
  "退出登录": "Sign out",
  "收起 Cookie 登录": "Hide Cookie sign-in",
  "粘贴 Cookie 登录": "Paste Cookie to sign in",
  "粘贴 Cookie": "Pasted Cookie",
  "请求头 Cookie（整段粘贴；只用于登录，不写日志）":
    "Request Cookie header (paste the whole value; used only for sign-in, never logged)",
  "例如 phpDiskInfo=...; ylogin=...": "e.g. phpDiskInfo=...; ylogin=...",
  "正在校验…": "Verifying…",
  "使用粘贴的 Cookie": "Use pasted Cookie",
  "错误原文直接来自后端（HTTP 状态与 zt/info 一并展示），界面不改写成“网络错误”。":
    "Errors are shown verbatim from the backend (HTTP status and zt/info included); the UI never rewrites them as “network error”.",
  "已退出登录，本机保存的凭据已删除。":
    "Signed out; saved credentials on this machine have been deleted.",
  "读取上传进度失败：{error}": "Failed to read upload progress: {error}",

  // 本地与云端目录
  "本地与云端目录": "Local and cloud folders",
  "上传目标 = 当前浏览的云端目录": "Upload target = the cloud folder you are browsing",
  "尚未选择要上传的本地目录": "No local upload folder selected",
  "本地目录": "Local folder",
  "选择中…": "Selecting…",
  "选择本地目录": "Choose local folder",
  "只上传该目录下的文件（不含子目录）；":
    "Only files directly in this folder are uploaded (subfolders excluded); ",
  "超过 100 MiB 的文件在预检阶段跳过。": "files over 100 MiB are skipped during pre-check. ",
  "优享按分片上传，不设 100 MiB 上限。":
    "Lanzou Premium uploads in chunks with no 100 MiB limit. ",
  "预检会读取云端当前目录做同名同大小判定。":
    "Pre-check reads the current cloud folder to detect same-name, same-size duplicates.",
  "登录后才能浏览云端目录。": "Sign in to browse cloud folders.",
  "上一级": "Up one level",
  "上传目标": "Upload target",
  "根目录": "Root",
  "当前目录没有子文件夹。": "No subfolders in this folder.",
  "类型": "Type",
  "当前目录没有文件。": "No files in this folder.",
  "文件": "File",
  "大小未知": "Size unknown",

  // 上传设置
  "上传设置": "Upload settings",
  "并发 1–5，请求间隔 1000–2000ms": "Concurrency 1–5, request interval 1000–2000 ms",
  "并发数（1–5）": "Concurrency (1–5)",
  "请求间隔（毫秒，1000–2000）": "Request interval (ms, 1000–2000)",
  "强制重传（忽略本机缓存与云端判重）":
    "Force re-upload (ignore local cache and cloud duplicate detection)",
  "强制重传会跳过本机缓存与同名同大小判定，可能产生云端重复文件；此时不会移动本地文件。":
    "Force re-upload bypasses the local cache and same-name/same-size detection, which can create duplicates in the cloud; local files are not moved in this case.",
  "正在预检…": "Pre-checking…",
  "预检（不移动、不上传）": "Pre-check (nothing moved or uploaded)",
  "请先登录。": "Sign in first.",
  "请先选择本地目录。": "Choose a local folder first.",
  "上传进行中，预检暂时禁用。": "Upload in progress; pre-check is disabled.",
  "请先预检": "Run a pre-check first",
  "请先勾选并同意免责声明": "Check and accept the disclaimer first",

  // 上传前确认与免责声明
  "上传前确认": "Confirm before upload",
  "免责声明已同意": "Disclaimer accepted",
  "需同意免责声明": "Disclaimer required",
  "勾选即表示你已阅读并同意：": "Checking this means you have read and agree to:",
  "我已阅读并同意以上说明": "I have read and agree to the above",
  "勾选后会调用后端持久化（写入本机设置）；每次开始上传仍会再弹一次确认。":
    "Checking it saves the choice via the backend (written to local settings); every upload still asks for confirmation.",
  "未授权：开始上传会被拒绝，登录与预检仍可用。":
    "Unlicensed: uploads will be rejected; sign-in and pre-check still work.",
  "正在启动…": "Starting…",
  "开始上传": "Start upload",
  "打开网页端": "Open web UI",
  "本次待上传 {n} 个文件。": "{n} file(s) ready to upload.",
  "本批次已结束；如需再次上传，请重新预检。":
    "This batch has finished; run a pre-check again to upload more.",
  "遵守{platform}使用条款，不上传违法、侵权或违反服务条款的内容。":
    "Follow the {platform} terms of use; do not upload unlawful, infringing, or terms-violating content.",
  "仅在本机用 DPAPI（CurrentUser）加密保存登录态，不保存密码、不写明文日志。":
    "Sign-in state is encrypted with DPAPI (CurrentUser) on this machine only; no passwords are stored and no plain-text logs are written.",
  "网盘重复只做同名同大小判定，不读取网盘文件内容。":
    "Cloud duplicates are detected by name and size only; cloud file contents are never read.",
  "云端重复的本地文件会移动到上传目录旁的「网盘重复文件」文件夹。":
    "Local files that duplicate cloud ones are moved into the “网盘重复文件” folder next to the upload folder.",
  "分享创建不在本功能内，请到网盘网页端手动操作。":
    "Folder sharing is not part of this feature; create it in the cloud web UI.",
  "分享创建不在本功能内，请到网页端手动操作。":
    "Folder sharing is not part of this feature; use the web UI.",
  "即将把 {n} 个文件上传到「{folder}」。云端重复的本地文件会被移动到「网盘重复文件」，上传期间可取消。继续？":
    "Upload {n} files to “{folder}”? Local files that duplicate cloud ones will be moved into “网盘重复文件”. You can cancel during the upload. Continue?",

  // 预检结果
  "预检结果": "Pre-check results",
  "批次 {id}": "Batch {id}",
  "文件总数": "Files total",
  "云端重复": "Cloud duplicates",
  "超 100 MiB": "Over 100 MiB",
  "缓存跳过": "Cache skips",
  "上传目标：": "Upload target: ",
  "（ID {id}）；本地目录：": " (ID {id}); local folder: ",
  "预检不会移动或上传任何文件。": "Pre-check moves and uploads nothing.",

  // 上传进度
  "上传进度": "Upload progress",
  "字节": "Bytes",
  "并发": "Concurrency",
  "间隔": "Interval",
  "目标": "Target",
  "、": ", ",
  "正在上传：{files}": "Uploading: {files}",
  "当前没有进行中的文件。": "No files in progress.",
  "汇总：共 {total} 项": "Summary: {total} items",
  "秒传": "Instant",
  "超限跳过": "Over-limit skips",
  "报告：": "Report: ",
  "尚未生成": "Not generated yet",
  "正在取消…": "Cancelling…",
  "取消上传": "Cancel upload",
  "打开报告": "Open report",
  "开始时间 {time}": "Started {time}",
  "结束时间 {time}": "Finished {time}",
  "逐条结果（{n} 项）": "Per-file results ({n})",

  // 逐条结果与状态标签
  "没有文件。": "No files.",
  "待上传": "Pending",
  "上传成功": "Uploaded",
  "超过单文件上限跳过": "Skipped (over per-file limit)",
  "云端重复（本地已移动）": "Cloud duplicate (local file moved)",
  "云端同名：{name}": "Same name in cloud: {name}",
  "（ID {id}）": " (ID {id})",
  "远端 ID：{id}": "Remote ID: {id}",
  "已移动到：{path}": "Moved to: {path}",

  // 登录来源、平台名称与阶段
  "账号密码登录": "Account & password",
  "本机已保存的登录态": "Saved sign-in on this machine",
  "—": "—",
  "蓝奏云（老版协议）": "Lanzou Cloud (legacy protocol)",
  "蓝奏优享": "Lanzou Premium",
  "预检完成，等待上传": "Pre-check done, waiting to upload",
  "上传中": "Uploading",
  "正在取消": "Cancelling",
  "已结束": "Finished",

  // 页面底部功能状态
  "本页功能状态（如实标注）": "Feature status on this page (reported as-is)",
  "账号密码登录、退出登录、DPAPI 本地存凭据、云端目录浏览与面包屑、上传前预检（本机缓存 / 云端同名同大小判定）、并发与请求间隔设置、分片上传、进度轮询与取消、结果报告与逐条错误原文":
    "Account/password sign-in, sign-out, local credentials stored with DPAPI, cloud folder browsing with breadcrumbs, pre-check (local cache / cloud same-name same-size detection), concurrency and request interval settings, chunked upload, progress polling and cancellation, reports with per-file raw errors",
  "账号密码登录、粘贴 Cookie 登录、从 C# 版导入、退出登录、DPAPI 本地存凭据、云端目录浏览与面包屑、上传前预检（超 100 MiB / 本机缓存 / 云端同名同大小判定）、并发与请求间隔设置、上传进度轮询与取消、结果报告与逐条错误原文":
    "Account/password sign-in, pasted Cookie sign-in, import from the C# app, sign-out, local credentials stored with DPAPI, cloud folder browsing with breadcrumbs, pre-check (over 100 MiB / local cache / cloud same-name same-size detection), concurrency and request interval settings, upload progress polling and cancellation, reports with per-file raw errors",
  "真实优享账号的登录、列目录、上传与判重尚未联调；分片上传 URL 等存疑项见 docs/ui-shell.md 的「未验证项」。":
    "Sign-in, folder listing, uploads, and duplicate detection with a real Premium account are not yet integration-tested; open questions such as the chunked-upload URL are listed under “Unverified items” in docs/ui-shell.md.",
  "登录成功后的真实目录列表、真实上传与云端判重命中都需要蓝奏账号验证。":
    "Real folder listing after sign-in, real uploads, and cloud duplicate hits all require verification with a Lanzou account.",
};

export default enLanzou;
