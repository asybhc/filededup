// 蓝奏云上传页面：接 cmd/filededup/lanzou.go。
//
// 诚实边界：登录成功后的真实目录列表、上传与云端判重命中都需要真实蓝奏账号联调；
// 本页只保证编译、单测（httptest）与本地可验证的部分。分享创建不做，请到网页端手动操作。
import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type {
  LanzouFolder,
  LanzouItem,
  LanzouItemKind,
  LanzouListing,
  LanzouOptions,
  LanzouPlatform,
  LanzouPrepareResult,
  LanzouSettings,
  LanzouStatus,
  LanzouUploadState,
} from "./types";

const KIND_LABELS: Record<LanzouItemKind, string> = {
  pending: "待上传",
  success: "上传成功",
  instant: "秒传",
  cached: "缓存跳过",
  "skipped-large": "超过单文件上限跳过",
  duplicate: "云端重复（本地已移动）",
  canceled: "已取消",
  failed: "失败",
};

const PLATFORM_LABELS: Record<LanzouPlatform, string> = {
  lanzou: "蓝奏云（老版协议）",
  youxiang: "蓝奏优享",
};

// 平台选择只影响界面，不写入 ui-settings.json，避免旧版读取设置时出现未知键。
const PLATFORM_STORAGE_KEY = "filededup.lanzou.platform";

function loadPlatform(): LanzouPlatform {
  try {
    return localStorage.getItem(PLATFORM_STORAGE_KEY) === "youxiang" ? "youxiang" : "lanzou";
  } catch {
    return "lanzou";
  }
}

// 复用现有 kind 徽标配色：成功绿、跳过灰、超限黄、重复蓝、失败红。
const KIND_CLASS: Record<LanzouItemKind, string> = {
  pending: "kind-exact",
  success: "kind-keep",
  instant: "kind-keep",
  cached: "kind-del",
  "skipped-large": "kind-warn",
  duplicate: "kind-audio",
  canceled: "kind-del",
  failed: "kind-danger",
};

const SOURCE_LABELS: Record<string, string> = {
  login: "账号密码登录",
  cookie: "粘贴 Cookie",
  import: "从 C# 版导入",
  local: "本机已保存的登录态",
  none: "—",
};

const PHASE_LABELS: Record<string, string> = {
  prepared: "预检完成，等待上传",
  running: "上传中",
  cancelling: "正在取消",
  done: "已结束",
  failed: "失败",
};

function disclaimerPoints(platform: LanzouPlatform): string[] {
  return [
    `遵守${PLATFORM_LABELS[platform]}使用条款，不上传违法、侵权或违反服务条款的内容。`,
    `仅在本机用 DPAPI（CurrentUser）加密保存登录态，不保存密码、不写明文日志。`,
    "网盘重复只做同名同大小判定，不读取网盘文件内容。",
    "云端重复的本地文件会移动到上传目录旁的「网盘重复文件」文件夹。",
    "分享创建不在本功能内，请到网盘网页端手动操作。",
  ];
}

function defaultLanzouSettings(): LanzouSettings {
  return {
    workers: 3,
    intervalMs: 1500,
    forceReupload: false,
    disclaimerAccepted: false,
    lastLocalDir: "",
    lastFolderId: "0",
    lastFolderName: "",
  };
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`;
}

function sizeText(size: number): string {
  return size < 0 ? "大小未知" : formatBytes(size);
}

function progressRatio(st: LanzouUploadState): number {
  if (st.bytesTotal > 0) return Math.max(0, Math.min(1, st.bytesSent / st.bytesTotal));
  if (st.total > 0) return Math.max(0, Math.min(1, st.done / st.total));
  return 0;
}

function summarize(items: LanzouItem[]): LanzouUploadState["summary"] {
  const s = {
    total: items.length, success: 0, instant: 0, cached: 0,
    skippedLarge: 0, duplicate: 0, canceled: 0, failed: 0,
  };
  for (const it of items) {
    switch (it.kind) {
      case "success": s.success++; break;
      case "instant": s.instant++; break;
      case "cached": s.cached++; break;
      case "skipped-large": s.skippedLarge++; break;
      case "duplicate": s.duplicate++; break;
      case "canceled": s.canceled++; break;
      case "failed": s.failed++; break;
      default: break;
    }
  }
  return s;
}

export function Lanzou({
  initial,
  onPersist,
  canRun = true,
  licenseHint,
}: {
  initial?: LanzouSettings;
  onPersist: (partial: Partial<LanzouSettings>) => void;
  canRun?: boolean;
  licenseHint?: string;
}) {
  const [cfg, setCfg] = useState<LanzouSettings>(() => initial ?? defaultLanzouSettings());
  const [platform, setPlatform] = useState<LanzouPlatform>(() => loadPlatform());
  const [status, setStatus] = useState<LanzouStatus>();
  const [statusError, setStatusError] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [cookieOpen, setCookieOpen] = useState(false);
  const [cookie, setCookie] = useState("");
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [path, setPath] = useState<LanzouFolder[]>([{ id: "0", name: "根目录" }]);
  const [listing, setListing] = useState<LanzouListing>();
  const [browseError, setBrowseError] = useState("");
  const [prepare, setPrepare] = useState<LanzouPrepareResult>();
  const [batchId, setBatchId] = useState("");
  const [upload, setUpload] = useState<LanzouUploadState>();
  const [polling, setPolling] = useState(false);

  // onPersist 每次渲染都是新函数，用 ref 固定，避免防抖 effect 反复重置。
  const persistRef = useRef(onPersist);
  persistRef.current = onPersist;
  // 只在启动时恢复一次上次浏览的目录；刚登录时统一回到根目录，避免两次浏览互相覆盖。
  const shouldRestoreRef = useRef(true);

  // 设置改动后 400ms 落盘，输入过程中不反复写文件。
  useEffect(() => {
    const timer = window.setTimeout(() => persistRef.current(cfg), 400);
    return () => window.clearTimeout(timer);
  }, [cfg]);

  // 记住平台选择；只在浏览器本地，不进入 ui-settings.json。
  useEffect(() => {
    try {
      localStorage.setItem(PLATFORM_STORAGE_KEY, platform);
    } catch {
      // localStorage 不可用时静默忽略，仅影响下次默认选中项。
    }
  }, [platform]);

  useEffect(() => {
    let alive = true;
    setStatusError("");
    api
      .lanzouStatus(platform)
      .then((st) => {
        if (alive) setStatus(st);
      })
      .catch((err) => {
        if (alive) setStatusError(errorText(err));
      });
    return () => {
      alive = false;
    };
  }, [platform]);

  // 已登录时恢复上次浏览的云端目录；仅启动/切换平台时触发一次。
  useEffect(() => {
    if (!status?.loggedIn || !shouldRestoreRef.current) return;
    let alive = true;
    const id = cfg.lastFolderId || "0";
    api
      .lanzouBrowse(platform, id)
      .then((l) => {
        shouldRestoreRef.current = false;
        if (!alive) return;
        setListing(l);
        setBrowseError("");
        setPath(
          id === "0"
            ? [{ id: "0", name: "根目录" }]
            : [{ id: "0", name: "根目录" }, { id, name: cfg.lastFolderName || id }],
        );
      })
      .catch((err) => {
        shouldRestoreRef.current = false;
        if (alive) setBrowseError(errorText(err));
      });
    return () => {
      alive = false;
    };
    // 只恢复一次，后续浏览位置由用户点击控制。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [status?.loggedIn, platform]);

  // 上传期间每 400ms 轮询进度；结束（done/failed）后自动停。
  useEffect(() => {
    if (!polling || !batchId) return;
    let stopped = false;
    const tick = async () => {
      try {
        const st = await api.lanzouUploadState(batchId);
        if (stopped) return;
        setUpload(st);
        if (st.phase === "done" || st.phase === "failed") {
          stopped = true;
          setPolling(false);
        }
      } catch (err) {
        if (stopped) return;
        stopped = true;
        setPolling(false);
        setError("读取上传进度失败：" + errorText(err));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 400);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [polling, batchId]);

  function patch(next: Partial<LanzouSettings>) {
    setCfg((prev) => ({ ...prev, ...next }));
  }

  function resetBatch() {
    setPrepare(undefined);
    setBatchId("");
    setUpload(undefined);
    setPolling(false);
  }

  // 切换网盘：清空会话态，避免把老版的登录态/目录/批次带到优享。
  function switchPlatform(next: LanzouPlatform) {
    if (next === platform) return;
    setPlatform(next);
    setStatus(undefined);
    setStatusError("");
    setNotice("");
    setError("");
    setUsername("");
    setPassword("");
    setCookie("");
    setCookieOpen(false);
    setListing(undefined);
    setBrowseError("");
    setPath([{ id: "0", name: "根目录" }]);
    shouldRestoreRef.current = true;
    resetBatch();
  }

  async function afterLogin(st: LanzouStatus) {
    shouldRestoreRef.current = false;
    setStatus(st);
    resetBatch();
    setListing(undefined);
    setPath([{ id: "0", name: "根目录" }]);
    setCookie("");
    setCookieOpen(false);
    if (st.message) setNotice(st.message);
    try {
      const l = await api.lanzouBrowse(platform, "0");
      setListing(l);
      setBrowseError("");
      patch({ lastFolderId: "0", lastFolderName: "" });
    } catch (err) {
      setBrowseError(errorText(err));
    }
  }

  async function doLogin() {
    setError("");
    setNotice("");
    setBusy("login");
    try {
      const st = await api.lanzouLogin(platform, username.trim(), password);
      setPassword("");
      await afterLogin(st);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function doCookie() {
    setError("");
    setNotice("");
    setBusy("cookie");
    try {
      const st = await api.lanzouUseCookie(platform, cookie);
      await afterLogin(st);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function doImportCSharp() {
    setError("");
    setNotice("");
    setBusy("import");
    try {
      const st = await api.lanzouImportCSharp(platform);
      await afterLogin(st);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function doLogout() {
    setError("");
    setNotice("");
    setBusy("logout");
    try {
      const st = await api.lanzouLogout(platform);
      setStatus(st);
      resetBatch();
      setListing(undefined);
      setBrowseError("");
      setPath([{ id: "0", name: "根目录" }]);
      setNotice(st.message || "已退出登录，本机保存的凭据已删除。");
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function pickLocalDir() {
    setError("");
    setBusy("pick");
    try {
      const dir = await api.lanzouPickDirectory();
      if (!dir) return;
      patch({ lastLocalDir: dir });
      resetBatch();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function goToCrumb(index: number) {
    if (index < 0 || index >= path.length - 1) return;
    const next = path.slice(0, index + 1);
    const target = next[next.length - 1];
    setBusy("browse");
    setBrowseError("");
    try {
      const l = await api.lanzouBrowse(platform, target.id);
      setListing(l);
      setPath(next);
      resetBatch();
      patch({ lastFolderId: target.id, lastFolderName: target.id === "0" ? "" : target.name });
    } catch (err) {
      setBrowseError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function browseInto(folder: LanzouFolder) {
    setBusy("browse");
    setBrowseError("");
    try {
      const l = await api.lanzouBrowse(platform, folder.id);
      setListing(l);
      setPath((prev) => [...prev, folder]);
      resetBatch();
      patch({ lastFolderId: folder.id, lastFolderName: folder.name });
    } catch (err) {
      setBrowseError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function runPrepare() {
    setError("");
    setNotice("");
    const target = path[path.length - 1];
    setBusy("prepare");
    try {
      const opts: LanzouOptions = {
        platform,
        localDir: cfg.lastLocalDir,
        folderId: target.id,
        folderName: target.id === "0" ? "" : target.name,
        workers: cfg.workers,
        intervalMs: cfg.intervalMs,
        forceReupload: cfg.forceReupload,
      };
      persistRef.current(cfg);
      const res = await api.lanzouPrepare(opts);
      setPrepare(res);
      setBatchId(res.batchId);
      setUpload(undefined);
      setPolling(false);
      if (res.message) setNotice(res.message);
    } catch (err) {
      setPrepare(undefined);
      setBatchId("");
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function toggleDisclaimer(checked: boolean) {
    if (!checked) {
      // 后端已记录的同意不撤销；这里只把界面与本机设置改回“需重新确认”。
      patch({ disclaimerAccepted: false });
      return;
    }
    setError("");
    try {
      await api.lanzouAcceptDisclaimer();
      patch({ disclaimerAccepted: true });
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function startUpload() {
    setError("");
    setNotice("");
    if (!prepare || !batchId) {
      setError("请先预检");
      return;
    }
    if (!cfg.disclaimerAccepted) {
      setError("请先勾选并同意免责声明");
      return;
    }
    if (!canRun) {
      setError(licenseHint || "未授权：开始上传会被拒绝，登录与预检仍可用。");
      return;
    }
    const ok = window.confirm(
      `即将把 ${prepare.uploadable} 个文件上传到「${prepare.folderName || "根目录"}」。` +
        "云端重复的本地文件会被移动到「网盘重复文件」，上传期间可取消。继续？",
    );
    if (!ok) return;
    setBusy("upload");
    try {
      await api.lanzouStartUpload(batchId, true);
      setUpload({
        batchId,
        platform,
        phase: "running",
        folderId: prepare.folderId,
        folderName: prepare.folderName,
        localDir: prepare.localDir,
        workers: cfg.workers,
        intervalMs: cfg.intervalMs,
        total: prepare.total,
        done: prepare.total - prepare.uploadable,
        bytesSent: 0,
        bytesTotal: 0,
        active: [],
        summary: summarize(prepare.items),
        items: prepare.items,
      });
      setPolling(true);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function cancelUpload() {
    if (!batchId) return;
    setError("");
    setBusy("cancel");
    try {
      await api.lanzouCancelUpload(batchId);
      setUpload((prev) => (prev ? { ...prev, phase: "cancelling" } : prev));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy("");
    }
  }

  async function openReport() {
    if (!batchId) return;
    setError("");
    try {
      await api.lanzouOpenReport(batchId);
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function openWeb() {
    setError("");
    try {
      await api.lanzouOpenWeb(platform);
    } catch (err) {
      setError(errorText(err));
    }
  }

  const target = path[path.length - 1];
  const running = upload?.phase === "running" || upload?.phase === "cancelling";
  const batchFinished = Boolean(upload && !running);
  // Go 侧空切片会序列化成 null，这里统一兜底，避免 .length/.join 崩溃。
  const items: LanzouItem[] = upload?.items ?? prepare?.items ?? [];
  const active: string[] = upload?.active ?? [];
  const summary = upload?.summary ?? (prepare ? summarize(prepare.items) : undefined);

  return (
    <div className="orgpage lanzoupage">
      <section className="card">
        <div className="sectionhead">
          <h2>{platform === "youxiang" ? "蓝奏优享上传" : "蓝奏云上传（老版协议）"}</h2>
          <span className={`badge ${status?.loggedIn ? "ok" : "warn"}`}>
            {status?.loggedIn ? `已登录 · ${status.account || "掩码未返回"}` : "未登录"}
          </span>
        </div>

        <div className="formgrid">
          <label>
            <span>网盘类型</span>
            <select
              value={platform}
              disabled={running || busy !== ""}
              onChange={(e) => switchPlatform(e.target.value as LanzouPlatform)}
            >
              <option value="lanzou">蓝奏云（老版协议，单文件 ≤ 100 MiB）</option>
              <option value="youxiang">蓝奏优享（ilanzou，分片上传）</option>
            </select>
          </label>
        </div>
        <p className="hint">
          两种网盘各自独立登录、各自保存凭据与上传缓存；切换网盘不会退出另一种的登录态。
          {platform === "youxiang"
            ? " 优享不支持 Cookie 登录、从 C# 版导入与创建云端目录。"
            : ""}
        </p>

        <div className="chips">
          <span className="chip">
            账号 <b>{status?.loggedIn ? status.account || "掩码未返回" : "—"}</b>
          </span>
          <span className="chip">
            登录来源 <b>{SOURCE_LABELS[status?.source ?? "none"] ?? status?.source}</b>
          </span>
          <span className="chip">
            凭据加密 <b>{status?.supportsDpapi ? "DPAPI 加密存储" : "当前平台不可用"}</b>
          </span>
          {status?.savedAt && (
            <span className="chip">
              保存时间 <b>{status.savedAt}</b>
            </span>
          )}
        </div>

        <p className="hint mono ellipsis" title={status?.credentialPath}>
          凭据文件：{status?.credentialPath || "（读取中）"}
        </p>
        <p className="hint">
          {status?.supportsDpapi
            ? "登录态仅在本机用 DPAPI（CurrentUser）加密保存，不保存密码，也不写明文日志。"
            : "是否为 DPAPI 加密以本机后端返回为准；界面不假设凭据已加密。"}
        </p>

        <div className="banner perf" role="status">
          {platform === "youxiang" ? (
            <span>
              <b>未联调声明</b>：优享协议字段来自对官方客户端的静态分析，登录、列目录、上传与判重都需要真实优享账号验证；
              分片上传的 URL 形态等存疑项已列在 <code>docs/ui-shell.md</code> 的「未验证项」，不作结论。
              分享创建不在本功能内，请到网页端手动操作。
            </span>
          ) : (
            <span>
              <b>未联调声明</b>：登录成功后的真实目录列表、上传与云端判重都需要蓝奏账号验证。
              本页只保证编译、单测与本地可验证部分。分享创建不在本功能内，请到网页端手动操作。
            </span>
          )}
        </div>

        {statusError && <div className="banner error">{statusError}</div>}
      </section>

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            关闭
          </button>
        </div>
      )}

      <section className="card">
        <div className="sectionhead">
          <h2>登录</h2>
          <span className="hint">账号密码只用于本次登录请求，不落盘</span>
        </div>

        <div className="formgrid">
          <label>
            <span>{platform === "youxiang" ? "蓝奏优享账号" : "蓝奏云账号"}</span>
            <input
              value={username}
              autoComplete="username"
              placeholder="手机号或邮箱"
              onChange={(e) => setUsername(e.target.value)}
            />
          </label>
          <label>
            <span>密码</span>
            <input
              type="password"
              value={password}
              autoComplete="current-password"
              placeholder="只用于本次登录，不保存"
              onChange={(e) => setPassword(e.target.value)}
            />
          </label>
        </div>

        <div className="actions">
          <button
            className="primary"
            disabled={!username.trim() || !password || busy === "login"}
            onClick={() => void doLogin()}
          >
            {busy === "login" ? "正在登录…" : "登录"}
          </button>
          {platform === "lanzou" && (
            <button className="secondary" disabled={busy === "import"} onClick={() => void doImportCSharp()}>
              {busy === "import" ? "正在导入…" : "从 C# 版导入"}
            </button>
          )}
          <button className="secondary" disabled={busy === "logout" || !status?.loggedIn} onClick={() => void doLogout()}>
            {busy === "logout" ? "正在退出…" : "退出登录"}
          </button>
          {platform === "lanzou" && (
            <button className="link" onClick={() => setCookieOpen((v) => !v)}>
              {cookieOpen ? "收起 Cookie 登录" : "粘贴 Cookie 登录"}
            </button>
          )}
        </div>

        {cookieOpen && platform === "lanzou" && (
          <>
            <label className="field">
              <span>请求头 Cookie（整段粘贴；只用于登录，不写日志）</span>
              <textarea
                rows={3}
                value={cookie}
                placeholder="例如 phpDiskInfo=...; ylogin=..."
                onChange={(e) => setCookie(e.target.value)}
              />
            </label>
            <div className="actions">
              <button className="secondary" disabled={!cookie.trim() || busy === "cookie"} onClick={() => void doCookie()}>
                {busy === "cookie" ? "正在校验…" : "使用粘贴的 Cookie"}
              </button>
            </div>
          </>
        )}

        {notice && <p className="hint">{notice}</p>}
        <p className="hint">错误原文直接来自后端（HTTP 状态与 zt/info 一并展示），界面不改写成“网络错误”。</p>
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>本地与云端目录</h2>
          <span className="hint">上传目标 = 当前浏览的云端目录</span>
        </div>

        <div className="dirrow">
          <input
            className="path"
            readOnly
            value={cfg.lastLocalDir}
            placeholder="尚未选择要上传的本地目录"
            aria-label="本地目录"
          />
          <button className="secondary" disabled={busy === "pick"} onClick={() => void pickLocalDir()}>
            {busy === "pick" ? "选择中…" : "选择本地目录"}
          </button>
        </div>
        <p className="hint">
          只上传该目录下的文件（不含子目录）；
          {platform === "lanzou"
            ? "超过 100 MiB 的文件在预检阶段跳过。"
            : "优享按分片上传，不设 100 MiB 上限。"}
          预检会读取云端当前目录做同名同大小判定。
        </p>

        {!status?.loggedIn ? (
          <p className="hint">登录后才能浏览云端目录。</p>
        ) : (
          <>
            <div className="lanzoubar">
              <button
                className="secondary"
                disabled={path.length <= 1 || busy === "browse"}
                onClick={() => void goToCrumb(path.length - 2)}
              >
                上一级
              </button>
              <div className="crumbs" title={path.map((f) => f.name).join(" / ")}>
                {path.map((f, i) => (
                  <span className="crumb" key={`${f.id}-${i}`}>
                    {i > 0 && <span className="crumbsep">/</span>}
                    <button className="link" disabled={i === path.length - 1} onClick={() => void goToCrumb(i)}>
                      {f.name}
                    </button>
                  </span>
                ))}
              </div>
              <span className="chip">
                上传目标 <b>{target.name}</b>
              </span>
            </div>

            {browseError && <div className="banner error">{browseError}</div>}

            <div className="lanfolders">
              {(listing?.folders ?? []).map((f) => (
                <button className="lanfolder" key={f.id} disabled={busy === "browse"} onClick={() => void browseInto(f)}>
                  <span className="kind kind-exact">目录</span>
                  <span className="lanname" title={f.name}>
                    {f.name}
                  </span>
                  <span className="mono">{f.id}</span>
                </button>
              ))}
              {(listing?.folders ?? []).length === 0 && <span className="hint">当前目录没有子文件夹。</span>}
            </div>

            <div className="lantablewrap">
              <div className="tablehead lanzoufilehead">
                <span>类型</span>
                <span>名称</span>
                <span className="num">大小</span>
                <span>修改时间</span>
              </div>
              <div className="lanfilelist">
                {(listing?.files ?? []).length === 0 && <p className="hint lanempty">当前目录没有文件。</p>}
                {(listing?.files ?? []).map((f) => (
                  <div className="row lanfilerow" key={f.id}>
                    <span className="kind kind-del">文件</span>
                    <span className="mname" title={f.name}>
                      {f.name}
                    </span>
                    <span className="num">{f.size < 0 ? "大小未知" : f.sizeText || formatBytes(f.size)}</span>
                    <span className="mtime">{f.time || "—"}</span>
                  </div>
                ))}
              </div>
            </div>
          </>
        )}
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>上传设置</h2>
          <span className="hint">并发 1–5，请求间隔 1000–2000ms</span>
        </div>

        <div className="formgrid">
          <label>
            <span>并发数（1–5）</span>
            <input
              type="number"
              min={1}
              max={5}
              value={cfg.workers}
              onChange={(e) => patch({ workers: Math.max(1, Math.min(5, Number(e.target.value) || 3)) })}
            />
          </label>
          <label>
            <span>请求间隔（毫秒，1000–2000）</span>
            <input
              type="number"
              min={1000}
              max={2000}
              step={100}
              value={cfg.intervalMs}
              onChange={(e) => patch({ intervalMs: Math.max(1000, Math.min(2000, Number(e.target.value) || 1500)) })}
            />
          </label>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.forceReupload}
              onChange={(e) => patch({ forceReupload: e.target.checked })}
            />
            <span>强制重传（忽略本机缓存与云端判重）</span>
          </label>
        </div>
        <p className="hint">
          强制重传会跳过本机缓存与同名同大小判定，可能产生云端重复文件；此时不会移动本地文件。
        </p>

        <div className="actions">
          <button
            className="primary"
            disabled={!status?.loggedIn || !cfg.lastLocalDir || busy === "prepare" || running}
            onClick={() => void runPrepare()}
          >
            {busy === "prepare" ? "正在预检…" : "预检（不移动、不上传）"}
          </button>
          {!status?.loggedIn && <span className="hint">请先登录。</span>}
          {status?.loggedIn && !cfg.lastLocalDir && <span className="hint">请先选择本地目录。</span>}
          {running && <span className="hint">上传进行中，预检暂时禁用。</span>}
        </div>
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>上传前确认</h2>
          <span className={`badge ${cfg.disclaimerAccepted ? "ok" : "warn"}`}>
            {cfg.disclaimerAccepted ? "免责声明已同意" : "需同意免责声明"}
          </span>
        </div>

        <div className="disclaimer">
          <p className="disclaimer-title">勾选即表示你已阅读并同意：</p>
          <ul>
            {disclaimerPoints(platform).map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.disclaimerAccepted}
              onChange={(e) => void toggleDisclaimer(e.target.checked)}
            />
            <span>我已阅读并同意以上说明</span>
          </label>
          <p className="hint">勾选后会调用后端持久化（写入本机设置）；每次开始上传仍会再弹一次确认。</p>
        </div>

        {!canRun && (
          <p className="hint">{licenseHint || "未授权：开始上传会被拒绝，登录与预检仍可用。"}</p>
        )}

        <div className="actions">
          <button
            className="danger"
            disabled={!prepare || !cfg.disclaimerAccepted || running || batchFinished || busy === "upload"}
            title={!canRun ? licenseHint : undefined}
            onClick={() => void startUpload()}
          >
            {busy === "upload" ? "正在启动…" : "开始上传"}
          </button>
          <button className="secondary" onClick={() => void openWeb()}>
            打开网页端
          </button>
          {prepare && !batchFinished && <span className="hint">本次待上传 {prepare.uploadable} 个文件。</span>}
          {batchFinished && <span className="hint">本批次已结束；如需再次上传，请重新预检。</span>}
        </div>
      </section>

      {prepare && !upload && (
        <section className="card">
          <div className="sectionhead">
            <h2>预检结果</h2>
            <span className="badge">批次 {prepare.batchId}</span>
          </div>

          <div className="chips">
            <span className="chip">文件总数 <b>{prepare.total}</b></span>
            <span className="chip">待上传 <b>{prepare.uploadable}</b></span>
            <span className="chip">云端重复 <b>{prepare.duplicates}</b></span>
            {platform === "lanzou" && (
              <span className="chip">超 100 MiB <b>{prepare.skippedLarge}</b></span>
            )}
            <span className="chip">缓存跳过 <b>{prepare.cached}</b></span>
          </div>

          <p className="hint">
            上传目标：<b>{prepare.folderName || "根目录"}</b>（ID {prepare.folderId}）；本地目录：
            <code>{prepare.localDir}</code>
          </p>
          <div className="banner info" role="status">
            预检不会移动或上传任何文件。
          </div>
          {prepare.message && <p className="hint">{prepare.message}</p>}

          <ItemTable items={items} />
        </section>
      )}

      {upload && (
        <section className="card">
          <div className="sectionhead">
            <h2>上传进度</h2>
            <span className={`badge ${upload.phase === "done" ? "ok" : upload.phase === "failed" ? "danger" : "warn"}`}>
              {PHASE_LABELS[upload.phase] ?? upload.phase}
            </span>
          </div>

          <div className="chips">
            <span className="chip">
              完成 <b>{upload.done} / {upload.total}</b>
            </span>
            <span className="chip">
              字节 <b>{formatBytes(upload.bytesSent)} / {formatBytes(upload.bytesTotal)}</b>
            </span>
            <span className="chip">并发 <b>{upload.workers}</b></span>
            <span className="chip">间隔 <b>{upload.intervalMs} ms</b></span>
            <span className="chip">目标 <b>{upload.folderName || "根目录"}</b></span>
          </div>

          <div className={`progress ${upload.phase === "running" && upload.bytesTotal <= 0 ? "indeterminate" : ""}`}>
            <div className="bar" style={{ width: `${Math.round(progressRatio(upload) * 100)}%` }} />
          </div>
          <p className="hint mono ellipsis" title={active.join("、")}>
            {active.length > 0 ? `正在上传：${active.join("、")}` : "当前没有进行中的文件。"}
          </p>

          {upload.error && <div className="banner error">{upload.error}</div>}

          {summary && (
            <div className="lansummary">
              <span>汇总：共 <b>{summary.total}</b> 项</span>
              <span>成功 <b>{summary.success}</b></span>
              <span>秒传 <b>{summary.instant}</b></span>
              <span>缓存跳过 <b>{summary.cached}</b></span>
              {summary.skippedLarge > 0 && <span>超限跳过 <b>{summary.skippedLarge}</b></span>}
              <span>云端重复 <b>{summary.duplicate}</b></span>
              <span>已取消 <b>{summary.canceled}</b></span>
              <span className={summary.failed ? "red" : ""}>失败 <b>{summary.failed}</b></span>
            </div>
          )}

          {!running && (
            <p className="hint">
              报告：
              {upload.reportJson ? <code>{upload.reportJson}</code> : "尚未生成"}
              {upload.reportCsv ? <> · <code>{upload.reportCsv}</code></> : null}
            </p>
          )}

          <div className="actions">
            {running && (
              <button className="secondary" disabled={busy === "cancel"} onClick={() => void cancelUpload()}>
                {busy === "cancel" ? "正在取消…" : "取消上传"}
              </button>
            )}
            <button className="secondary" disabled={!upload.reportJson || running} onClick={() => void openReport()}>
              打开报告
            </button>
            <button className="secondary" onClick={() => void openWeb()}>
              打开网页端
            </button>
            {upload.startedAt && <span className="hint">开始时间 {upload.startedAt}</span>}
            {upload.finishedAt && <span className="hint">结束时间 {upload.finishedAt}</span>}
          </div>

          <h3 className="subhead">逐条结果（{items.length} 项）</h3>
          <ItemTable items={items} />
        </section>
      )}

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            关闭
          </button>
        </div>
      )}

      <div className="features">
        <h3>本页功能状态（如实标注）</h3>
        <p>
          <span className="kind kind-keep">已接通</span>{" "}
          {platform === "youxiang"
            ? "账号密码登录、退出登录、DPAPI 本地存凭据、云端目录浏览与面包屑、上传前预检（本机缓存 / 云端同名同大小判定）、并发与请求间隔设置、分片上传、进度轮询与取消、结果报告与逐条错误原文"
            : "账号密码登录、粘贴 Cookie 登录、从 C# 版导入、退出登录、DPAPI 本地存凭据、云端目录浏览与面包屑、上传前预检（超 100 MiB / 本机缓存 / 云端同名同大小判定）、并发与请求间隔设置、上传进度轮询与取消、结果报告与逐条错误原文"}
        </p>
        <p>
          <span className="kind kind-warn">未联调</span>{" "}
          {platform === "youxiang"
            ? "真实优享账号的登录、列目录、上传与判重尚未联调；分片上传 URL 等存疑项见 docs/ui-shell.md 的「未验证项」。"
            : "登录成功后的真实目录列表、真实上传与云端判重命中都需要蓝奏账号验证。"}
          分享创建不在本功能内，请到网页端手动操作。
        </p>
      </div>
    </div>
  );
}

function ItemTable({ items }: { items: LanzouItem[] }) {
  return (
    <div className="lantablewrap">
      <div className="tablehead lanzouitemhead">
        <span>状态</span>
        <span>文件</span>
        <span className="num">大小</span>
        <span>说明</span>
      </div>
      <div className="lanfilelist">
        {items.length === 0 && <p className="hint lanempty">没有文件。</p>}
        {items.map((it, i) => (
          <div className="row lanitemrow" key={`${it.localPath}-${i}`}>
            <span className={`kind ${KIND_CLASS[it.kind] ?? "kind-del"}`}>
              {KIND_LABELS[it.kind] ?? it.kind}
            </span>
            <span className="mname" title={it.localPath}>
              {it.name}
            </span>
            <span className="num">{sizeText(it.size)}</span>
            <span className="lanitemnote">
              {it.error ? <span className="lanerror">{it.error}</span> : null}
              {it.duplicateOf ? (
                <span className="inlinehint">
                  云端同名：{it.duplicateOf}
                  {it.remoteId ? `（ID ${it.remoteId}）` : ""}
                </span>
              ) : null}
              {it.remoteId && !it.duplicateOf ? <span className="inlinehint">远端 ID：{it.remoteId}</span> : null}
              {it.movedTo ? <span className="inlinehint">已移动到：{it.movedTo}</span> : null}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
