import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, isDesktop } from "./api";
import { mock } from "./mock";
import { VirtualList } from "./components/VirtualList";
import { Organizer } from "./Organizer";
import type {
  AppInfo,
  DeleteMode,
  ExecuteState,
  GroupView,
  KeepPolicy,
  LicenseState,
  OrganizeSettings,
  PlanPreview,
  Row,
  ScanRequest,
  ScanState,
  SkipView,
  UISettings,
} from "./types";

const MB = 1024 * 1024;

const KEEP_LABELS: Record<KeepPolicy, string> = {
  pathShortest: "最短路径优先",
  newest: "最新修改优先",
  oldest: "最早修改优先",
  nameShortest: "最短文件名优先",
};

const KIND_LABELS: Record<string, string> = {
  exact: "内容相同",
  image: "相似图片",
  audio: "相似音频",
};

type Step = "pick" | "scan" | "results" | "preview" | "done";

const STEPS: { key: Step; label: string }[] = [
  { key: "pick", label: "选择目录" },
  { key: "scan", label: "扫描" },
  { key: "results", label: "结果" },
  { key: "preview", label: "预览执行" },
  { key: "done", label: "完成" },
];

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

function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString("zh-CN", { hour12: false });
}

function flatten(groups: GroupView[]): Row[] {
  const rows: Row[] = [];
  for (const g of groups) {
    rows.push({ type: "group", key: `g:${g.id}`, group: g });
    for (const m of g.members) {
      rows.push({ type: "member", key: `m:${g.id}:${m.path}`, groupId: g.id, member: m });
    }
  }
  return rows;
}

function ratio(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(1, done / total));
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function licenseSourceLabel(src?: string): string {
  switch (src) {
    case "ed25519":
      return "Ed25519 许可证";
    case "legacy-ecdsa":
      return "旧版 ECDSA 许可证（只读兼容）";
    case "trial":
      return "试用期";
    default:
      return "无";
  }
}

function licenseLabel(st?: LicenseState): string {
  if (!st) return "授权状态读取中";
  switch (st.status) {
    case "licensed":
      return st.forever ? `已授权 · ${st.edition ?? ""} · 永久` : `已授权 · ${st.expiry ?? ""} 到期`;
    case "trial":
      return `试用中 · 剩余 ${st.trialDaysLeft} 天`;
    case "trial-expired":
      return "试用已结束 · 未授权";
    default:
      return "未授权";
  }
}

export default function App() {
  const [info, setInfo] = useState<AppInfo>({ version: "", platform: "" });
  const [step, setStep] = useState<Step>("pick");
  const [req, setReq] = useState<ScanRequest & { minSizeMB: number }>({
    root: "",
    keepPolicy: "pathShortest",
    similarImage: true,
    imageThreshold: 8,
    similarAudio: false,
    audioTolerance: 2,
    minSize: 0,
    includeHidden: false,
    excludes: [],
    minSizeMB: 0,
  });
  const [excludesText, setExcludesText] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [scan, setScan] = useState<ScanState>();
  const [groups, setGroups] = useState<GroupView[]>([]);
  const [groupsTotal, setGroupsTotal] = useState(0);
  const [loadingGroups, setLoadingGroups] = useState(false);
  const [preview, setPreview] = useState<PlanPreview>();
  const [deleteMode, setDeleteMode] = useState<DeleteMode>("trash");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [permanentAck, setPermanentAck] = useState(false);
  const [exec, setExec] = useState<ExecuteState>();
  const [error, setError] = useState("");
  const [detail, setDetail] = useState("");
  const [skipOpen, setSkipOpen] = useState(false);
  const [skips, setSkips] = useState<SkipView[]>([]);
  const [perfRows, setPerfRows] = useState<Row[]>([]);
  const [perfActive, setPerfActive] = useState(false);
  const [fps, setFps] = useState(0);
  const [view, setView] = useState<"dedup" | "organize">("dedup");
  const [settings, setSettings] = useState<UISettings>();
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsPath, setSettingsPath] = useState("");
  const [settingsNote, setSettingsNote] = useState("");
  const settingsRef = useRef<UISettings | undefined>(undefined);
  const [license, setLicense] = useState<LicenseState>();
  const [licenseOpen, setLicenseOpen] = useState(false);
  const [licenseText, setLicenseText] = useState("");
  const [licenseNote, setLicenseNote] = useState("");
  const licenseStop = Boolean(license && !license.canApply);

  // 启动时恢复设置：缺键由后端补默认值，坏文件只给提示不回退到崩溃。
  useEffect(() => {
    let alive = true;
    api
      .getSettings()
      .then((r) => {
        if (!alive) return;
        const s = r.settings;
        settingsRef.current = s;
        setSettings(s);
        setSettingsPath(r.path);
        if (r.warning) setSettingsNote(r.warning);
        setReq((prev) => ({
          ...prev,
          root: s.scan.root || prev.root,
          keepPolicy: s.scan.keepPolicy,
          minSizeMB: Math.round((s.scan.minSize || 0) / MB),
          includeHidden: s.scan.includeHidden,
          similarImage: s.scan.similarImage,
          imageThreshold: s.scan.imageThreshold,
          similarAudio: s.scan.similarAudio,
          audioTolerance: s.scan.audioTolerance,
        }));
        setDeleteMode(s.scan.deleteMode);
        setExcludesText((s.scan.excludes || []).join(" "));
        setSettingsReady(true);
      })
      .catch((err) => {
        if (!alive) return;
        setSettingsNote("设置读取失败，已使用默认值：" + errorText(err));
        setSettingsReady(true);
      });
    return () => {
      alive = false;
    };
  }, []);

  // 扫描选项变更后自动落盘（600ms 防抖），不阻塞输入。
  useEffect(() => {
    const base = settingsRef.current;
    if (!base) return;
    const timer = window.setTimeout(() => {
      const next: UISettings = {
        ...base,
        scan: {
          ...base.scan,
          root: req.root,
          keepPolicy: req.keepPolicy,
          deleteMode,
          similarImage: req.similarImage,
          imageThreshold: req.imageThreshold,
          similarAudio: req.similarAudio,
          audioTolerance: req.audioTolerance,
          minSize: Math.round(req.minSizeMB * MB),
          includeHidden: req.includeHidden,
          excludes: excludesText.split(/[,，;；\s]+/).filter(Boolean),
        },
      };
      settingsRef.current = next;
      setSettings(next);
      api.saveSettings(next).catch((err) => setSettingsNote("设置保存失败：" + errorText(err)));
    }, 600);
    return () => window.clearTimeout(timer);
  }, [req, deleteMode, excludesText]);

  const persistOrganize = (next: OrganizeSettings) => {
    const base = settingsRef.current;
    if (!base) return;
    const merged: UISettings = { ...base, organize: next };
    settingsRef.current = merged;
    setSettings(merged);
    api.saveSettings(merged).catch((err) => setSettingsNote("设置保存失败：" + errorText(err)));
  };

  useEffect(() => {
    api.getAppInfo().then(setInfo).catch(() => undefined);
  }, []);

  // 启动时读取授权状态；导入许可证后刷新同一份状态。
  useEffect(() => {
    let alive = true;
    api
      .getLicenseState()
      .then((st) => {
        if (alive) setLicense(st);
      })
      .catch((err) => {
        if (alive) setLicenseNote("授权状态读取失败：" + errorText(err));
      });
    return () => {
      alive = false;
    };
  }, []);

  // 浏览器开发预览：?demo=results|preview|done|perf 直接落入对应步骤。
  // 只在 Wails 之外生效，且顶部有"合成数据"横幅。
  useEffect(() => {
    if (isDesktop) return;
    const demo = new URLSearchParams(window.location.search).get("demo");
    if (!demo) return;
    const gs = mock.demoGroups(demo === "perf" ? 33334 : 120);
    if (demo === "perf") {
      setPerfRows(flatten(gs));
      setPerfActive(true);
      setGroupsTotal(gs.length);
      setScan(mock.demoScanState(gs));
      setStep("results");
      return;
    }
    setGroups(gs);
    setGroupsTotal(gs.length);
    setScan(mock.demoScanState(gs));
    if (demo === "preview") {
      setPreview(mock.demoPreview(gs, "trash"));
      setStep("preview");
    } else if (demo === "done") {
      setExec(mock.demoExec(gs, "trash"));
      setStep("done");
    } else {
      setStep("results");
    }
  }, []);

  // 扫描进度轮询。
  useEffect(() => {
    if (step !== "scan" || !sessionId) return;
    let stopped = false;
    const tick = async () => {
      try {
        const st = await api.getScanState(sessionId);
        if (stopped) return;
        setScan(st);
        if (st.phase === "done") {
          stopped = true;
          setStep("results");
          void loadGroups(sessionId);
        } else if (st.phase === "failed") {
          stopped = true;
          setError(st.error || "扫描失败");
        } else if (st.phase === "cancelled") {
          // 取消后停在扫描页展示已扫到的数量，不再继续轮询。
          stopped = true;
        }
      } catch (err) {
        if (!stopped) setError(errorText(err));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 300);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
    // loadGroups 是稳定的本地函数，不需要进依赖。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step, sessionId]);

  // 预览随删除方式变化重新生成。
  useEffect(() => {
    if (step !== "preview" || !sessionId) return;
    let stopped = false;
    api
      .previewPlan(sessionId, deleteMode)
      .then((p) => {
        if (!stopped) setPreview(p);
      })
      .catch((err) => {
        if (!stopped) setError(errorText(err));
      });
    return () => {
      stopped = true;
    };
  }, [step, sessionId, deleteMode]);

  // 执行进度轮询。
  useEffect(() => {
    if (!exec?.running) return;
    const timer = window.setInterval(async () => {
      try {
        const st = await api.getExecuteState(sessionId);
        setExec(st);
        if (st.done) {
          window.clearInterval(timer);
          setStep("done");
        }
      } catch (err) {
        setError(errorText(err));
      }
    }, 400);
    return () => window.clearInterval(timer);
  }, [exec?.running, sessionId]);

  // 性能自检时的帧率采样（1 秒窗口）。
  useEffect(() => {
    if (!perfActive) return;
    let frames = 0;
    let last = performance.now();
    let raf = 0;
    let stopped = false;
    const tick = (t: number) => {
      frames++;
      if (t - last >= 1000) {
        setFps(Math.round((frames * 1000) / (t - last)));
        frames = 0;
        last = t;
      }
      if (!stopped) raf = window.requestAnimationFrame(tick);
    };
    raf = window.requestAnimationFrame(tick);
    return () => {
      stopped = true;
      window.cancelAnimationFrame(raf);
    };
  }, [perfActive]);

  async function loadGroups(id: string) {
    setLoadingGroups(true);
    try {
      const limit = 500;
      const first = await api.getGroups(id, 0, limit);
      setGroupsTotal(first.total);
      let all = first.items;
      setGroups(all.slice());
      while (all.length < first.total) {
        const page = await api.getGroups(id, all.length, limit);
        if (page.items.length === 0) break;
        all = all.concat(page.items);
        setGroups(all.slice());
      }
    } catch (err) {
      setError(errorText(err));
    } finally {
      setLoadingGroups(false);
    }
  }

  async function pickDirectory() {
    setError("");
    try {
      const dir = await api.pickDirectory();
      if (dir) setReq((prev) => ({ ...prev, root: dir }));
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function startScan() {
    setError("");
    setScan(undefined);
    setExec(undefined);
    setPreview(undefined);
    setGroups([]);
    setGroupsTotal(0);
    try {
      const id = await api.startScan({
        ...req,
        minSize: Math.round(req.minSizeMB * MB),
        excludes: excludesText
          .split(/[,，;；\s]+/)
          .map((s) => s.trim())
          .filter(Boolean),
      });
      setSessionId(id);
      setStep("scan");
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function cancelScan() {
    try {
      await api.cancelScan(sessionId);
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function openPreview() {
    setError("");
    try {
      const p = await api.previewPlan(sessionId, deleteMode);
      setPreview(p);
      setStep("preview");
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function startExecute() {
    setConfirmOpen(false);
    setError("");
    setExec({ running: true, done: false, total: preview?.total ?? 0, applied: 0, failed: 0, skipped: 0, rolledBack: 0, reclaimed: 0 });
    try {
      await api.startExecute(sessionId, deleteMode, true);
    } catch (err) {
      setExec(undefined);
      setError(errorText(err));
    }
  }

  async function importLicenseFile() {
    setLicenseNote("");
    try {
      const st = await api.importLicenseFile();
      setLicense(st);
      if (st.licensed) setLicenseNote("许可证导入成功。");
      else setLicenseNote(st.warning || "未识别到有效许可证。");
    } catch (err) {
      setLicenseNote("导入失败：" + errorText(err));
    }
  }

  async function importLicensePasted() {
    setLicenseNote("");
    try {
      const st = await api.importLicenseText(licenseText);
      setLicense(st);
      setLicenseText("");
      setLicenseNote(st.licensed ? "许可证导入成功。" : st.warning || "未识别到有效许可证。");
    } catch (err) {
      setLicenseNote("导入失败：" + errorText(err));
    }
  }

  async function copyMachineCode() {
    const code = license?.machineCode ?? "";
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      setLicenseNote("机器码已复制。");
    } catch {
      setLicenseNote("复制失败，请手动选中机器码复制。");
    }
  }

  async function openSkips() {
    try {
      const page = await api.getSkipped(sessionId, 0, 200);
      setSkips(page.items);
      setSkipOpen(true);
    } catch (err) {
      setError(errorText(err));
    }
  }

  function runPerf() {
    const gs = mock.demoGroups(33334);
    setPerfRows(flatten(gs));
    setPerfActive(true);
    setGroupsTotal(gs.length);
    setScan(mock.demoScanState(gs));
    setStep("results");
  }

  function reset() {
    setStep("pick");
    setSessionId("");
    setScan(undefined);
    setGroups([]);
    setGroupsTotal(0);
    setPreview(undefined);
    setExec(undefined);
    setPerfActive(false);
    setPerfRows([]);
    setError("");
    setDetail("");
  }

  const rows = useMemo(() => (perfActive ? perfRows : flatten(groups)), [perfActive, perfRows, groups]);
  const stepIndex = STEPS.findIndex((s) => s.key === step);

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <div className="logo">FD</div>
          <div>
            <h1>文件去重</h1>
            <p>本地扫描 · 回收站可恢复 · 不做云端上传</p>
          </div>
        </div>
        <div className="topmeta">
          {!isDesktop && <span className="badge warn">浏览器预览（合成数据）</span>}
          <button
            className={`badge license-badge ${license?.status ?? ""}`}
            title="查看授权状态、机器码与许可证导入"
            onClick={() => {
              setLicenseNote("");
              setLicenseOpen(true);
            }}
          >
            {licenseLabel(license)}
          </button>
          <span className="badge">v{info.version || "—"}</span>
          <span className="badge">{info.platform || "—"}</span>
        </div>
      </header>

      {licenseStop && (
        <div className="license-banner" role="status">
          <span>
            <b>未授权</b>：{license?.limits}
            {license?.warning ? ` ${license.warning}` : ""}
          </span>
          <button className="secondary" onClick={() => setLicenseOpen(true)}>
            去授权
          </button>
        </div>
      )}

      <nav className="tabs" aria-label="模块">
        <button className={view === "dedup" ? "active" : ""} onClick={() => setView("dedup")}>
          查重与清理
        </button>
        <button className={view === "organize" ? "active" : ""} onClick={() => setView("organize")}>
          九步整理
        </button>
        {view === "dedup" && (
          <span className="tabs-hint">
            当前流程：{STEPS.map((s) => s.label).join(" → ")}
          </span>
        )}
      </nav>

      {view === "dedup" && (
        <nav className="stepper" aria-label="流程">
          {STEPS.map((s, i) => (
            <div key={s.key} className={`step ${i === stepIndex ? "active" : ""} ${i < stepIndex ? "done" : ""}`}>
              <span className="step-no">{i + 1}</span>
              <span>{s.label}</span>
            </div>
          ))}
        </nav>
      )}

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            关闭
          </button>
        </div>
      )}

      <main className="main">
        {view === "organize" ? (
          settingsReady ? (
            <Organizer
              initial={settings?.organize}
              onPersist={persistOrganize}
              canRun={!licenseStop}
              licenseHint="未授权：整理的真实执行已禁用，请先导入许可证；试运行仍可用。"
            />
          ) : (
            <section className="card">
              <h2>九步整理</h2>
              <p className="hint">正在读取设置…</p>
            </section>
          )
        ) : (
          <>
        {step === "pick" && (
          <section className="card">
            <h2>1. 选择目录与扫描设置</h2>
            <div className="dirrow">
              <input className="path" readOnly value={req.root} placeholder="尚未选择目录" aria-label="扫描目录" />
              <button className="secondary" onClick={pickDirectory}>
                选择目录
              </button>
            </div>

            <div className="formgrid">
              <label>
                <span>保留策略</span>
                <select
                  value={req.keepPolicy}
                  onChange={(e) => setReq({ ...req, keepPolicy: e.target.value as KeepPolicy })}
                >
                  {Object.entries(KEEP_LABELS).map(([value, label]) => (
                    <option key={value} value={value}>
                      {label}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>删除方式</span>
                <select value={deleteMode} onChange={(e) => setDeleteMode(e.target.value as DeleteMode)}>
                  <option value="trash">移入回收站（可恢复）</option>
                  <option value="permanent">永久删除（不可恢复）</option>
                </select>
              </label>
              <label>
                <span>最小文件大小（MB，0 = 不限）</span>
                <input
                  type="number"
                  min={0}
                  step={1}
                  value={req.minSizeMB}
                  onChange={(e) => setReq({ ...req, minSizeMB: Math.max(0, Number(e.target.value) || 0) })}
                />
              </label>
              <label>
                <span>排除子串（逗号或空格分隔）</span>
                <input value={excludesText} onChange={(e) => setExcludesText(e.target.value)} placeholder="例如 node_modules .git" />
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.similarImage}
                  onChange={(e) => setReq({ ...req, similarImage: e.target.checked })}
                />
                <span>相似图片（dHash）</span>
              </label>
              <label>
                <span>相似阈值（0 = 完全相同，8 = 默认）</span>
                <input
                  type="number"
                  min={0}
                  max={64}
                  value={req.imageThreshold}
                  disabled={!req.similarImage}
                  onChange={(e) => setReq({ ...req, imageThreshold: Math.max(0, Math.min(64, Number(e.target.value) || 0)) })}
                />
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.similarAudio}
                  onChange={(e) => setReq({ ...req, similarAudio: e.target.checked })}
                />
                <span>相似音频（时长容差）</span>
              </label>
              <label>
                <span>音频容差（秒）</span>
                <input
                  type="number"
                  min={0}
                  max={60}
                  value={req.audioTolerance}
                  disabled={!req.similarAudio}
                  onChange={(e) => setReq({ ...req, audioTolerance: Math.max(0, Math.min(60, Number(e.target.value) || 0)) })}
                />
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.includeHidden}
                  onChange={(e) => setReq({ ...req, includeHidden: e.target.checked })}
                />
                <span>包含隐藏/系统文件</span>
              </label>
            </div>

            <p className="hint">
              {settingsPath ? (
                <>
                  设置自动保存到 <code>{settingsPath}</code>，重启后自动恢复。
                  {settingsNote ? `（${settingsNote}）` : ""}
                </>
              ) : (
                "正在读取设置…"
              )}
            </p>

            <div className="actions">
              <button className="primary" disabled={!req.root} onClick={startScan}>
                开始扫描
              </button>
            </div>

            <FeatureStatus />
          </section>
        )}

        {step === "scan" && (
          <section className="card">
            <h2>2. 扫描中</h2>
            <p className="mono ellipsis" title={req.root}>
              {req.root}
            </p>
            <ScanProgress scan={scan} onCancel={cancelScan}>
              <button className="secondary" onClick={reset}>
                返回
              </button>
            </ScanProgress>
          </section>
        )}

        {step === "results" && (
          <section className="card fill">
            <div className="sectionhead">
              <h2>3. 查重结果</h2>
              <div className="actions inline">
                <button className="secondary" onClick={reset}>
                  重新扫描
                </button>
                <button className="primary" onClick={openPreview} disabled={perfActive || rows.length === 0}>
                  生成执行预览
                </button>
              </div>
            </div>

            <div className="chips">
              <span className="chip">重复组 <b>{scan?.groups ?? groupsTotal}</b></span>
              <span className="chip">重复文件 <b>{scan?.duplicateFiles ?? 0}</b></span>
              <span className="chip">可回收 <b>{formatBytes(scan?.reclaimable ?? 0)}</b></span>
              <span className="chip">跳过 <b>{scan?.skipped ?? 0}</b></span>
              <span className="chip">保留策略 <b>{KEEP_LABELS[req.keepPolicy]}</b></span>
              <button className="link" onClick={openSkips} disabled={!sessionId}>
                查看跳过项
              </button>
            </div>

            {perfActive && (
              <div className="banner perf">
                <span>
                  性能自检：当前是 <b>{perfRows.length.toLocaleString("zh-CN")} 行</b>合成数据（≥ 10 万），不是真实扫描结果。滚动帧率约{" "}
                  <b>{fps || "—"} fps</b>（1 秒采样）。
                </span>
                <button className="link" onClick={() => setPerfActive(false)}>
                  退出自检
                </button>
              </div>
            )}

            {!perfActive && (
              <div className="toolbar">
                <span className="hint">
                  {loadingGroups
                    ? `正在载入结果：${groups.length} / ${groupsTotal} 组`
                    : `共 ${groupsTotal} 组，已全部载入；点击任意行查看完整路径。`}
                </span>
                <button className="link" onClick={runPerf}>
                  性能自检：载入 10 万行合成数据
                </button>
              </div>
            )}

            <div className="tablehead">
              <span>状态</span>
              <span>文件 / 组</span>
              <span>完整路径</span>
              <span className="num">大小</span>
              <span>修改时间</span>
            </div>
            <VirtualList
              className="tablebody"
              rows={rows}
              rowHeight={72}
              rowKey={(row) => row.key}
              renderRow={(row) => <ResultRow row={row} onSelect={setDetail} />}
            />
          </section>
        )}

        {step === "preview" && (
          <section className="card fill">
            <div className="sectionhead">
              <h2>4. 执行预览</h2>
              <div className="actions inline">
                <button className="secondary" onClick={() => setStep("results")}>
                  返回结果
                </button>
                <button
                  className="danger"
                  disabled={!preview || preview.total === 0 || Boolean(exec?.running) || licenseStop}
                  title={licenseStop ? "未授权，请先在“授权”中导入许可证" : undefined}
                  onClick={() => {
                    setPermanentAck(false);
                    setConfirmOpen(true);
                  }}
                >
                  确认执行
                </button>
              </div>
            </div>

            {licenseStop && (
              <p className="hint">
                未授权：确认执行已禁用，只能扫描与预览。请先在“授权”中导入许可证。
              </p>
            )}

            <div className="formgrid narrow">
              <label>
                <span>删除方式</span>
                <select value={deleteMode} onChange={(e) => setDeleteMode(e.target.value as DeleteMode)}>
                  <option value="trash">移入回收站（可恢复）</option>
                  <option value="permanent">永久删除（不可恢复）</option>
                </select>
              </label>
            </div>

            {deleteMode === "permanent" && (
              <div className="banner danger">
                永久删除不可恢复，也不会进入回收站。请确认已备份重要数据。
              </div>
            )}

            <div className="chips">
              <span className="chip">动作总数 <b>{preview?.total ?? 0}</b></span>
              <span className="chip">涉及组 <b>{preview?.groups ?? 0}</b></span>
              <span className="chip">可回收 <b>{formatBytes(preview?.reclaimableBytes ?? 0)}</b></span>
              {preview &&
                Object.entries(preview.kindCounts).map(([kind, count]) => (
                  <span className="chip" key={kind}>
                    {kind === "delete_permanent" ? "永久删除" : "回收站"} <b>{count}</b>
                  </span>
                ))}
            </div>

            {exec?.running && (
              <div className="banner info">
                <span>正在执行：{exec.total} 个动作，请勿关闭窗口。</span>
              </div>
            )}

            <h3 className="subhead">动作样例（前 200 条，共 {preview?.total ?? 0} 条）</h3>
            <div className="samples">
              {(preview?.samples ?? []).map((a) => (
                <div className="sample" key={a.seq}>
                  <span className="seq">#{a.seq}</span>
                  <span className={`kind kind-${a.kind === "delete_permanent" ? "danger" : "exact"}`}>
                    {a.kind === "delete_permanent" ? "永久删除" : "回收站"}
                  </span>
                  <span className="samplepath" title={a.src}>
                    {a.src}
                  </span>
                  <span className="num">{formatBytes(a.size)}</span>
                </div>
              ))}
            </div>
          </section>
        )}

        {step === "done" && (
          <section className="card">
            <h2>5. 执行完成</h2>
            <div className="stats">
              <div className="stat">
                <span>成功</span>
                <b>{exec?.applied ?? 0}</b>
              </div>
              <div className="stat">
                <span>失败</span>
                <b className={exec?.failed ? "red" : ""}>{exec?.failed ?? 0}</b>
              </div>
              <div className="stat">
                <span>跳过</span>
                <b>{exec?.skipped ?? 0}</b>
              </div>
              <div className="stat">
                <span>回滚</span>
                <b>{exec?.rolledBack ?? 0}</b>
              </div>
              <div className="stat wide">
                <span>释放空间</span>
                <b>{formatBytes(exec?.reclaimed ?? 0)}</b>
              </div>
            </div>
            {exec?.error && <div className="banner error">{exec.error}</div>}
            <p className="hint">
              执行日志已写入用户配置目录下的 <code>FileDedup/ops</code>。删除后的文件不在结果列表中刷新，建议重新扫描确认。
            </p>
            <div className="actions">
              <button className="secondary" onClick={reset}>
                重新扫描
              </button>
            </div>
          </section>
        )}
          </>
        )}
      </main>

      {detail && (
        <aside className="drawer" aria-label="路径详情">
          <div className="drawerhead">
            <h3>完整路径</h3>
            <button className="link" onClick={() => setDetail("")}>
              关闭
            </button>
          </div>
          <p className="fullpath">{detail}</p>
        </aside>
      )}

      {skipOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>扫描期间跳过（前 200 项）</h3>
            {skips.length === 0 ? (
              <p className="hint">没有跳过项。</p>
            ) : (
              <div className="skiplist">
                {skips.map((s, i) => (
                  <div className="skiprow" key={`${s.path}-${i}`}>
                    <span className="kind kind-warn">{s.code}</span>
                    <div>
                      <p className="skippath">{s.path}</p>
                      <p className="hint">{s.message}</p>
                    </div>
                  </div>
                ))}
              </div>
            )}
            <div className="actions">
              <button className="secondary" onClick={() => setSkipOpen(false)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmOpen && preview && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>确认执行</h3>
            <p>
              即将对 <b>{preview.total}</b> 个文件执行
              <b>{deleteMode === "trash" ? "移入回收站" : "永久删除"}</b>，预计释放{" "}
              <b>{formatBytes(preview.reclaimableBytes)}</b>。
            </p>
            <p className="hint">
              {deleteMode === "trash"
                ? "可在系统回收站中恢复，但恢复后不会自动回到本工具的记录里。"
                : "永久删除不可恢复，也不会进入回收站。"}
            </p>
            {deleteMode === "permanent" && (
              <label className="checkline">
                <input type="checkbox" checked={permanentAck} onChange={(e) => setPermanentAck(e.target.checked)} />
                <span>我明白永久删除不可恢复，并已备份重要数据</span>
              </label>
            )}
            {licenseStop && (
              <p className="hint">
                未授权：确认执行已禁用。请先关闭此窗口，在“授权”中导入许可证后重试。
              </p>
            )}
            <div className="actions">
              <button className="secondary" onClick={() => setConfirmOpen(false)}>
                取消
              </button>
              <button
                className="danger"
                disabled={(deleteMode === "permanent" && !permanentAck) || licenseStop}
                onClick={startExecute}
              >
                确认执行
              </button>
            </div>
          </div>
        </div>
      )}

      {licenseOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>授权</h3>
            <dl className="licenseinfo">
              <dt>状态</dt>
              <dd>{licenseLabel(license)}</dd>
              <dt>来源</dt>
              <dd>{licenseSourceLabel(license?.source)}</dd>
              <dt>到期</dt>
              <dd>
                {license?.licensed
                  ? license.forever
                    ? "永久"
                    : license.expiry || "—"
                  : license?.trialActive
                    ? `${license.trialStart} 至 ${license.trialEnd}`
                    : "—"}
              </dd>
              <dt>机器码</dt>
              <dd>
                <code>{license?.machineCode || "—"}</code>
              </dd>
              <dt>许可证文件</dt>
              <dd className="break">{license?.licensePath || "—"}</dd>
            </dl>

            <p className="hint">{license?.limits}</p>
            {license?.warning && <p className="hint warn">{license.warning}</p>}
            {license?.trialActive && (
              <p className="hint">
                试用期从首次启动算起共 3 天，剩余 {license.trialDaysLeft} 天；到期后删除执行与整理执行会被禁用。
              </p>
            )}

            <div className="actions">
              <button className="secondary" onClick={() => void copyMachineCode()}>
                复制机器码
              </button>
              <button onClick={() => void importLicenseFile()}>导入许可证文件…</button>
              <button className="secondary" onClick={() => setLicenseOpen(false)}>
                关闭
              </button>
            </div>

            <label className="field">
              <span>或粘贴许可证文本</span>
              <textarea
                rows={3}
                value={licenseText}
                placeholder="FDLIC1:… 或旧版 Base32 许可证"
                onChange={(e) => setLicenseText(e.target.value)}
              />
            </label>
            <div className="actions">
              <button className="secondary" disabled={!licenseText.trim()} onClick={() => void importLicensePasted()}>
                导入粘贴内容
              </button>
            </div>
            {licenseNote && <p className="hint">{licenseNote}</p>}
            <p className="hint">
              未授权时仍可扫描与预览，但删除执行和整理执行会被禁用，并在界面上明确提示，不会静默降级。
            </p>
          </div>
        </div>
      )}
    </div>
  );
}

function ScanProgress({
  scan,
  onCancel,
  children,
}: {
  scan?: ScanState;
  onCancel: () => void;
  children?: ReactNode;
}) {
  if (!scan) {
    return (
      <>
        <p className="hint">正在启动扫描…</p>
        <div className="actions">{children}</div>
      </>
    );
  }
  const hashRatio = ratio(scan.hashDone, scan.hashTargets);
  const phaseLabel =
    scan.phase === "cancelling"
      ? "正在取消…"
      : scan.phase === "cancelled"
        ? "已取消（结果不完整）"
        : scan.enumerationDone
          ? "正在计算哈希"
          : "正在枚举文件";
  return (
    <>
      <div className="chips">
        <span className="chip">阶段 <b>{phaseLabel}</b></span>
        <span className="chip">已发现文件 <b>{scan.files.toLocaleString("zh-CN")}</b></span>
        <span className="chip">已读取 <b>{formatBytes(scan.bytes)}</b></span>
        <span className="chip">用时 <b>{(scan.elapsedMs / 1000).toFixed(1)} s</b></span>
      </div>
      <div className={`progress ${scan.enumerationDone ? "" : "indeterminate"}`}>
        <div className="bar" style={{ width: `${Math.round(hashRatio * 100)}%` }} />
      </div>
      <p className="hint">
        {scan.enumerationDone
          ? `哈希进度 ${scan.hashDone.toLocaleString("zh-CN")} / ${scan.hashTargets.toLocaleString("zh-CN")}（${formatBytes(scan.hashBytes)} / ${formatBytes(scan.hashTotalBytes)}）`
          : "枚举阶段的总量未知，进度条为不确定状态；取消会保留已扫描的部分结果。"}
      </p>
      <div className="actions">
        <button className="secondary" disabled={scan.phase !== "running"} onClick={onCancel}>
          取消扫描
        </button>
        {children}
      </div>
    </>
  );
}

const ResultRow = memo(function ResultRow({
  row,
  onSelect,
}: {
  row: Row;
  onSelect: (path: string) => void;
}) {
  if (row.type === "group") {
    return (
      <div className="row grouprow" onClick={() => onSelect(row.group.keep)}>
        <span className={`kind kind-${row.group.kind}`}>{KIND_LABELS[row.group.kind] ?? row.group.kind}</span>
        <span className="gtitle">重复组 · {row.group.members.length} 个文件</span>
        <span className="gkeep">
          保留：{row.group.keep.slice(row.group.keep.lastIndexOf("\\") + 1)}
        </span>
        <span className="num">可回收 {formatBytes(row.group.reclaimable)}</span>
        <span />
      </div>
    );
  }
  const m = row.member;
  return (
    <div className={`row memberrow ${m.keep ? "iskeep" : ""}`} onClick={() => onSelect(m.path)}>
      <span className={`kind ${m.keep ? "kind-keep" : "kind-del"}`}>{m.keep ? "保留" : "可删除"}</span>
      <span className="mname" title={m.name}>
        {m.name}
      </span>
      <span className="mpath" title={m.path}>
        {m.path}
      </span>
      <span className="num">{formatBytes(m.size)}</span>
      <span className="mtime">{formatTime(m.modTime)}</span>
    </div>
  );
});

function FeatureStatus() {
  return (
    <div className="features">
      <h3>功能状态（如实标注）</h3>
      <p>
        <span className="kind kind-keep">已接通</span>{" "}
        目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、九步整理（试运行/真实执行）、
        Ed25519 许可证导入与状态显示、旧版 ECDSA 许可证只读兼容、试用期（PBKDF2 + 双存储 + 回拨检测）
      </p>
      <p>
        <span className="kind kind-warn">未实现</span>{" "}
        系统托盘、自动更新、蓝奏云上传、自定义保留项、执行撤销、撤销历史
      </p>
    </div>
  );
}
