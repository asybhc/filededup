import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, isDesktop } from "./api";
import { onAppEvent } from "./api";
import { mock } from "./mock";
import { VirtualList } from "./components/VirtualList";
import { Organizer } from "./Organizer";
import { Lanzou } from "./Lanzou";
import { SettingsDialog } from "./Settings";
import { Search } from "./Search";
import type {
  AppInfo,
  DedupUndoInfo,
  DedupUndoResult,
  DeleteMode,
  ExecuteState,
  GroupView,
  KeepPolicy,
  LanzouSettings,
  LicenseState,
  OrganizeSettings,
  PlanPreview,
  Row,
  ScanRequest,
  ScanState,
  SkipView,
  UISettings,
  UpdateCheckView,
  UpdateSettings,
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

const UPDATE_STATUS_LABELS: Record<string, string> = {
  disabled: "未配置更新源",
  manual: "手动检查模式",
  throttled: "检查间隔未到",
  uptodate: "已是最新",
  available: "有新版本",
  skipped: "已跳过此版本",
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
      rows.push({ type: "member", key: `m:${g.id}:${m.path}`, groupId: g.id, group: g, member: m });
    }
  }
  return rows;
}

// 默认勾选 = 每组按保留策略可删除的成员（即旧版"执行整组删除"的范围）。
function defaultDeleteSelection(groups: GroupView[]): Set<string> {
  const out = new Set<string>();
  for (const g of groups) {
    for (const m of g.members) {
      if (m.deletable) out.add(m.path);
    }
  }
  return out;
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
  // 勾选 = 将删除；未勾选 = 保留。默认与旧版整组删除一致。
  const [deleteSel, setDeleteSel] = useState<Set<string>>(new Set());
  const [selNote, setSelNote] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [permanentAck, setPermanentAck] = useState(false);
  const [exec, setExec] = useState<ExecuteState>();
  // 查重侧撤销：只保留最近一次“移入回收站”的执行，跨重启可读。
  const [undoInfo, setUndoInfo] = useState<DedupUndoInfo>();
  const [undoResult, setUndoResult] = useState<DedupUndoResult>();
  const [undoBusy, setUndoBusy] = useState(false);
  const [undoConfirm, setUndoConfirm] = useState(false);
  const [undoNote, setUndoNote] = useState("");
  const [error, setError] = useState("");
  const [detail, setDetail] = useState("");
  const [skipOpen, setSkipOpen] = useState(false);
  const [skips, setSkips] = useState<SkipView[]>([]);
  const [perfRows, setPerfRows] = useState<Row[]>([]);
  const [perfActive, setPerfActive] = useState(false);
  const [fps, setFps] = useState(0);
  const [view, setView] = useState<"dedup" | "organize" | "lanzou" | "search">("dedup");
  const [settings, setSettings] = useState<UISettings>();
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsPath, setSettingsPath] = useState("");
  const [settingsNote, setSettingsNote] = useState("");
  const settingsRef = useRef<UISettings | undefined>(undefined);
  const [license, setLicense] = useState<LicenseState>();
  const [licenseOpen, setLicenseOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [licenseText, setLicenseText] = useState("");
  const [licenseNote, setLicenseNote] = useState("");
  const licenseStop = Boolean(license && !license.canApply);
  const searchFeature = license?.featureList?.find((f) => f.key === "search");

  // 自动更新：更新源默认留空，只有配置后才检查。
  const [updateOpen, setUpdateOpen] = useState(false);
  const [updateBusy, setUpdateBusy] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<UpdateCheckView>();
  const [updateNote, setUpdateNote] = useState("");
  const [installArmed, setInstallArmed] = useState(false);
  const [updateForm, setUpdateForm] = useState<UpdateSettings>({
    manifestUrl: "",
    checkMode: "auto",
    intervalHours: 18,
    skippedVersion: "",
  });
  const autoChecked = useRef(false);

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
        setUpdateForm(s.update);
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

  // 主题：system 时监听系统深浅色变化；值来自设置，切换后写一份 localStorage 镜像，
  // 供 index.html 的早期脚本在 React 挂载前就选定配色，刷新不闪白。
  const theme = settings?.window.theme ?? "system";
  useEffect(() => {
    if (typeof window === "undefined" || !window.matchMedia) return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const dark = theme === "dark" || (theme === "system" && mq.matches);
      document.documentElement.dataset.theme = dark ? "dark" : "light";
      try {
        localStorage.setItem("filededup.theme", theme);
      } catch {
        /* 隐私模式下 localStorage 可能不可写，忽略即可 */
      }
    };
    apply();
    if (theme === "system") {
      mq.addEventListener("change", apply);
      return () => mq.removeEventListener("change", apply);
    }
  }, [theme]);

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

  const persistLanzou = (partial: Partial<LanzouSettings>) => {
    const base = settingsRef.current;
    if (!base) return;
    const merged: UISettings = { ...base, lanzou: { ...base.lanzou, ...partial } };
    settingsRef.current = merged;
    setSettings(merged);
    api.saveSettings(merged).catch((err) => setSettingsNote("设置保存失败：" + errorText(err)));
  };

  // 启动后按配置自动检查一次；有更新只点亮入口，不主动弹窗。
  useEffect(() => {
    if (!settingsReady || autoChecked.current) return;
    autoChecked.current = true;
    const cfg = settingsRef.current?.update;
    if (!cfg || !cfg.manifestUrl.trim()) return;
    api
      .checkForUpdate(false)
      .then((v) => {
        if (v.status === "available") setUpdateCheck(v);
      })
      .catch(() => undefined);
  }, [settingsReady]);

  const persistUpdate = async (next: UpdateSettings) => {
    setUpdateForm(next);
    const base = settingsRef.current;
    if (!base) return;
    const merged: UISettings = { ...base, update: next };
    settingsRef.current = merged;
    setSettings(merged);
    await api.saveSettings(merged);
  };

  const runCheck = async (manual: boolean) => {
    setUpdateBusy(true);
    if (manual) setUpdateNote("正在检查更新…");
    try {
      const v = await api.checkForUpdate(manual);
      setUpdateCheck(v);
      if (v.message) setUpdateNote(v.message);
    } catch (err) {
      setUpdateCheck(undefined);
      setUpdateNote("检查失败：" + errorText(err));
    } finally {
      setUpdateBusy(false);
    }
  };

  const openUpdate = () => {
    setUpdateOpen(true);
    setInstallArmed(false);
    setUpdateNote("");
    void runCheck(true);
  };

  const saveAndCheck = async () => {
    const url = updateForm.manifestUrl.trim();
    if (url && !/^https?:\/\//i.test(url)) {
      setUpdateNote("更新源必须是 http:// 或 https:// 地址。");
      return;
    }
    const interval = Math.min(720, Math.max(1, Number(updateForm.intervalHours) || 18));
    const next: UpdateSettings = { ...updateForm, manifestUrl: url, intervalHours: interval };
    setUpdateBusy(true);
    setUpdateNote("正在保存并检查…");
    try {
      await persistUpdate(next);
      const v = await api.checkForUpdate(true);
      setUpdateCheck(v);
      setUpdateNote(v.message || "检查完成。");
    } catch (err) {
      setUpdateCheck(undefined);
      setUpdateNote("检查失败：" + errorText(err));
    } finally {
      setUpdateBusy(false);
    }
  };

  const skipLatest = async () => {
    const v = updateCheck?.latestVersion;
    if (!v) return;
    setUpdateBusy(true);
    try {
      await api.skipUpdateVersion(v);
      setUpdateForm((prev) => ({ ...prev, skippedVersion: v }));
      const base = settingsRef.current;
      if (base) {
        const merged: UISettings = { ...base, update: { ...base.update, skippedVersion: v } };
        settingsRef.current = merged;
        setSettings(merged);
      }
      setUpdateCheck((prev) => (prev ? { ...prev, status: "skipped", message: `已跳过版本 ${v}。` } : prev));
      setUpdateNote(`已跳过版本 ${v}，后续检查不再提示。`);
    } catch (err) {
      setUpdateNote("跳过版本失败：" + errorText(err));
    } finally {
      setUpdateBusy(false);
    }
  };

  const clearSkipped = async () => {
    setUpdateBusy(true);
    try {
      await api.clearSkippedUpdateVersion();
      setUpdateForm((prev) => ({ ...prev, skippedVersion: "" }));
      const base = settingsRef.current;
      if (base) {
        const merged: UISettings = { ...base, update: { ...base.update, skippedVersion: "" } };
        settingsRef.current = merged;
        setSettings(merged);
      }
      setUpdateNote("已取消跳过版本。");
      setUpdateCheck(undefined);
    } catch (err) {
      setUpdateNote("取消跳过失败：" + errorText(err));
    } finally {
      setUpdateBusy(false);
    }
  };

  const installLatest = async () => {
    if (!installArmed) {
      setInstallArmed(true);
      return;
    }
    setUpdateBusy(true);
    setUpdateNote("正在下载、验签并替换程序，请勿关闭窗口…");
    try {
      const r = await api.installUpdate();
      setUpdateNote(`已更新到 ${r.version}，程序即将重启。备份：${r.backupPath}`);
    } catch (err) {
      // 浏览器预览会在这里直接给出“不能安装”的明确原因。
      setUpdateNote("安装已取消：" + errorText(err));
      setInstallArmed(false);
    } finally {
      setUpdateBusy(false);
    }
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
    setDeleteSel(defaultDeleteSelection(gs));
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
      .previewPlan(sessionId, deleteMode, Array.from(deleteSel))
      .then((p) => {
        if (!stopped) setPreview(p);
      })
      .catch((err) => {
        if (!stopped) setError(errorText(err));
      });
    return () => {
      stopped = true;
    };
  }, [step, sessionId, deleteMode, deleteSel]);

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

  // 进入“完成”步骤后读取撤销记录；读取失败不影响主流程，按“暂无可撤销”展示。
  useEffect(() => {
    if (step !== "done") return;
    let alive = true;
    api
      .getDedupUndoInfo()
      .then((info) => {
        if (alive) setUndoInfo(info);
      })
      .catch(() => {
        if (alive) setUndoInfo(undefined);
      });
    return () => {
      alive = false;
    };
  }, [step, sessionId]);

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
      setDeleteSel(defaultDeleteSelection(all));
      setSelNote("");
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
    setDeleteSel(new Set());
    setSelNote("");
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

  // 托盘菜单“扫描”：后端只负责把窗口置前并发出事件，实际扫描参数用界面当前选择。
  const startScanRef = useRef(startScan);
  startScanRef.current = startScan;
  useEffect(() => {
    const offScan = onAppEvent("tray:scan", () => {
      void startScanRef.current();
    });
    const offSched = onAppEvent("schedule:done", (payload: { message?: string; status?: string }) => {
      setSettingsNote(`定时扫描：${payload?.message ?? payload?.status ?? "已结束"}`);
    });
    return () => {
      offScan();
      offSched();
    };
  }, []);

  async function cancelScan() {
    // 本地先置为 cancelling：按钮立即变灰，不等下一次 300ms 轮询。
    // 若取消失败，轮询会把状态刷回 running，界面自愈。
    const markCancelling = () =>
      setScan((prev) => (prev && prev.phase === "running" ? { ...prev, phase: "cancelling" } : prev));
    markCancelling();
    try {
      await api.cancelScan(sessionId);
      // 等待期间轮询可能写回 running，成功后补一次。
      markCancelling();
    } catch (err) {
      setError(errorText(err));
    }
  }

  function toggleMember(group: GroupView, path: string) {
    setSelNote("");
    const next = new Set(deleteSel);
    if (next.has(path)) {
      next.delete(path);
      setDeleteSel(next);
      return;
    }
    next.add(path);
    if (group.members.every((m) => next.has(m.path))) {
      setSelNote("每组至少要保留一个文件：已阻止把整组都勾选删除。");
      return;
    }
    setDeleteSel(next);
  }

  // all/none 只作用于"可删除"成员；invert 逐成员反转；keep 把当前勾选项视为要保留。
  function applyGroupSelection(group: GroupView, action: "all" | "none" | "invert" | "keep") {
    setSelNote("");
    const next = new Set(deleteSel);
    if (action === "all") {
      for (const m of group.members) {
        if (m.deletable) next.add(m.path);
      }
    } else if (action === "none") {
      for (const m of group.members) next.delete(m.path);
    } else if (action === "invert") {
      for (const m of group.members) {
        if (next.has(m.path)) next.delete(m.path);
        else next.add(m.path);
      }
      if (group.members.every((m) => next.has(m.path))) {
        const fallback = group.members.find((m) => m.keep) ?? group.members[0];
        next.delete(fallback.path);
        setSelNote(`反转后会删除整组，已自动留出「${fallback.name}」保留。`);
      }
    } else {
      const kept = group.members.filter((m) => next.has(m.path));
      if (kept.length === 0) {
        setSelNote("请先勾选要保留的文件，再点「仅保留所选」。");
        return;
      }
      for (const m of group.members) {
        if (next.has(m.path)) next.delete(m.path);
        else next.add(m.path);
      }
      setSelNote(`已保留勾选的 ${kept.length} 个文件，其余 ${group.members.length - kept.length} 个改为待删除。`);
    }
    setDeleteSel(next);
  }

  function selectAllDeletable() {
    setSelNote("");
    setDeleteSel(defaultDeleteSelection(groups));
  }

  // 全局反选：逐组把“未勾选”变“勾选”；若某组反选后变成整组删除，自动留出该组的保留项。
  function invertAllSelection() {
    setSelNote("");
    const next = new Set<string>();
    let adjusted = 0;
    for (const g of groups) {
      for (const m of g.members) {
        if (!deleteSel.has(m.path)) next.add(m.path);
      }
      if (g.members.every((m) => next.has(m.path))) {
        const fallback = g.members.find((m) => m.keep) ?? g.members[0];
        next.delete(fallback.path);
        adjusted++;
      }
    }
    if (adjusted > 0) {
      setSelNote(`反选后有 ${adjusted} 组会变成整组删除，已自动留出每组一个保留项。`);
    }
    setDeleteSel(next);
  }

  function clearAllSelection() {
    setSelNote("");
    setDeleteSel(new Set());
  }

  async function openPreview() {
    setError("");
    try {
      const p = await api.previewPlan(sessionId, deleteMode, Array.from(deleteSel));
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
      await api.startExecute(sessionId, deleteMode, Array.from(deleteSel), true);
    } catch (err) {
      setExec(undefined);
      setError(errorText(err));
    }
  }

  async function doDedupUndo() {
    setUndoConfirm(false);
    setUndoBusy(true);
    setUndoNote("");
    try {
      const res = await api.undoLastDedup();
      setUndoResult(res);
      if (res.binEmpty) {
        setUndoNote("回收站已被清空，删除的文件无法自动恢复。");
      } else if (res.failed > 0) {
        setUndoNote(`有 ${res.failed} 个文件未能恢复，原因见下方逐条说明。`);
      }
      const info = await api.getDedupUndoInfo();
      setUndoInfo(info);
    } catch (err) {
      setUndoNote("撤销失败：" + errorText(err));
      api
        .getDedupUndoInfo()
        .then(setUndoInfo)
        .catch(() => {});
    } finally {
      setUndoBusy(false);
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
    setUndoInfo(undefined);
    setUndoResult(undefined);
    setUndoNote("");
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
            <p>本地扫描 · 回收站可恢复 · 云端上传为可选</p>
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
          <button className="badge" title="设置：外观、托盘、定时任务、AI、日志" onClick={() => setSettingsOpen(true)}>
            设置
          </button>
          <span className="badge">v{info.version || "—"}</span>
          <button
            className={`badge update-badge ${updateCheck?.status === "available" ? "has-update" : ""}`}
            title="检查更新（清单与安装包均验签）"
            onClick={openUpdate}
          >
            {updateCheck?.status === "available" ? `有更新 v${updateCheck.latestVersion}` : "检查更新"}
          </button>
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
        <button className={view === "lanzou" ? "active" : ""} onClick={() => setView("lanzou")}>
          蓝奏云上传
        </button>
        <button className={view === "search" ? "active" : ""} onClick={() => setView("search")}>
          本地搜索
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
        ) : view === "lanzou" ? (
          settingsReady ? (
            <Lanzou
              initial={settings?.lanzou}
              onPersist={persistLanzou}
              canRun={!licenseStop}
              licenseHint="未授权：开始上传会被拒绝，登录与预检仍可用。"
            />
          ) : (
            <section className="card">
              <h2>蓝奏云上传</h2>
              <p className="hint">正在读取设置…</p>
            </section>
          )
        ) : view === "search" ? (
          <Search
            canRun={searchFeature?.unlocked ?? false}
            licenseHint={
              searchFeature
                ? `未解锁：${searchFeature.reason ?? "本地搜索需要标准版许可证"}（本机机器码 ${
                    license?.machineCode ?? "读取中"
                  }）。`
                : "正在读取授权状态，请稍候。"
            }
          />
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
                <button
                  className="primary"
                  onClick={openPreview}
                  disabled={perfActive || rows.length === 0 || deleteSel.size === 0}
                  title={!perfActive && deleteSel.size === 0 ? "请先勾选要删除的文件" : undefined}
                >
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

            {!perfActive && (
              <div className="toolbar">
                <span className="hint">
                  勾选 = <b>删除</b>，未勾选 = <b>保留</b>；已勾选 <b>{deleteSel.size}</b> 项，执行范围以勾选为准，不再按整组删除。
                  {selNote ? <span className="warn"> {selNote}</span> : null}
                </span>
                <div className="actions inline">
                  <button className="link" onClick={selectAllDeletable}>
                    全选可删除项
                  </button>
                  <button className="link" onClick={invertAllSelection}>
                    反选
                  </button>
                  <button className="link" onClick={clearAllSelection}>
                    全不选
                  </button>
                </div>
              </div>
            )}

            <div className="tablehead">
              <span>选择 / 状态</span>
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
              renderRow={(row) => (
                <ResultRow
                  row={row}
                  onSelect={setDetail}
                  selectable={!perfActive}
                  selected={deleteSel}
                  onToggle={toggleMember}
                  onGroupAction={applyGroupSelection}
                />
              )}
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
                {license?.machineCode ? (
                  <>
                    {" "}
                    机器码：<code>{license.machineCode}</code>
                  </>
                ) : null}
              </p>
            )}

            <p className="hint">
              本次只处理结果页勾选的 <b>{deleteSel.size}</b> 个文件；同一组未勾选的副本会保留。
            </p>

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

            <div className="sectionhead">
              <h3>撤销上次删除</h3>
              <span className={`badge ${undoInfo?.available ? "warn" : ""}`}>
                {undoInfo?.available ? `可撤销 ${undoInfo.trash} 个文件` : "暂无可撤销记录"}
              </span>
            </div>
            {undoInfo?.available ? (
              <>
                <p className="hint">
                  批次时间 {undoInfo.finishedAt ? formatTime(undoInfo.finishedAt) : "未知"} · 删除方式{" "}
                  {undoInfo.deleteMode === "permanent" ? "永久删除" : "移入回收站"}（只保留最近一次批次）。
                  {undoInfo.truncated && " 明细只展示前 200 条。"}
                </p>
                <div className="skiplist">
                  {undoInfo.items.map((it) => (
                    <div className="skiprow" key={it.path}>
                      <span className="kind">{formatBytes(it.size)}</span>
                      <div>
                        <p className="skippath">{it.path}</p>
                        <p className="hint">
                          来源目录 {it.dir}
                          {it.modTime ? ` · 文件修改时间 ${formatTime(it.modTime)}` : ""}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
                {undoInfo.permanent > 0 && (
                  <p className="hint">
                    另有 {undoInfo.permanent} 个文件为永久删除：
                    {undoInfo.permanentReason || "永久删除未进入回收站，无法撤销"}。
                  </p>
                )}
                <div className="actions">
                  <button className="secondary" disabled={undoBusy} onClick={() => setUndoConfirm(true)}>
                    {undoBusy ? "正在撤销…" : "撤销上次删除（恢复到原路径）"}
                  </button>
                </div>
                <p className="hint">
                  撤销会逐条检查：目标位置已有同名文件时不覆盖；原目录不存在时先重建目录。
                  永久删除与已清空的回收站都无法恢复，界面会如实标注失败原因。
                </p>
              </>
            ) : (
              <p className="hint">
                真实执行过一次“移入回收站”的删除后，这里会出现最近一次的撤销入口；记录跨重启仍可读取。
              </p>
            )}
            {undoNote && <div className="banner info">{undoNote}</div>}
            {undoResult && (
              <p className="hint">
                上次撤销：恢复 {undoResult.restored} · 失败 {undoResult.failed} · 不可撤销{" "}
                {undoResult.unrestorable}（共 {undoResult.total} 条）
              </p>
            )}
            {undoResult && undoResult.errors.length > 0 && (
              <div className="skiplist">
                {undoResult.errors.map((line, i) => (
                  <div className="skiprow" key={i}>
                    <span className="kind kind-warn">原因</span>
                    <p className="skippath">{line}</p>
                  </div>
                ))}
              </div>
            )}

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
                ? "移入系统回收站；执行完成后可在本页“撤销上次删除”中恢复到原路径。"
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

      {undoConfirm && undoInfo?.available && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>确认撤销上次删除</h3>
            <p>
              将把 <b>{undoInfo.trash}</b> 个文件从回收站恢复到原路径。
            </p>
            <p className="hint">
              目标位置已有同名文件时会跳过并给出原因，不会覆盖你后来新建的文件；
              原目录不存在时会先重建目录。此操作无法撤销。
            </p>
            <div className="actions">
              <button className="secondary" onClick={() => setUndoConfirm(false)}>
                取消
              </button>
              <button className="primary" disabled={undoBusy} onClick={doDedupUndo}>
                确认撤销
              </button>
            </div>
          </div>
        </div>
      )}

      {settingsOpen && (
        <SettingsDialog
          onClose={() => setSettingsOpen(false)}
          onSaved={(next) => {
            settingsRef.current = next;
            setSettings(next);
          }}
        />
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
              <dt>版本</dt>
              <dd>{license?.edition || (license?.status === "unlicensed" ? "未授权" : "—")}</dd>
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
            {license?.featureList?.length ? (
              <div className="featurelist" aria-label="功能解锁状态">
                {license.featureList.map((f) => (
                  <div
                    key={f.key}
                    className={`featureitem ${f.unlocked ? "on" : "off"}`}
                    title={f.unlocked ? "已解锁" : `${f.edition}解锁 · ${f.reason ?? ""}`}
                  >
                    <span className="mark">{f.unlocked ? "已解锁" : "未解锁"}</span>
                    <span className="name">{f.name}</span>
                    <span className="meta">{f.unlocked ? f.edition : `${f.edition} · ${f.reason ?? ""}`}</span>
                  </div>
                ))}
              </div>
            ) : null}
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

      {updateOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>软件更新</h3>
            <dl className="licenseinfo">
              <dt>当前版本</dt>
              <dd>v{info.version || "—"}</dd>
              <dt>更新源</dt>
              <dd className="break">{updateForm.manifestUrl || "未配置（不检查）"}</dd>
              <dt>检查模式</dt>
              <dd>{updateForm.checkMode === "auto" ? `自动，每 ${updateForm.intervalHours} 小时` : "手动"}</dd>
              <dt>跳过版本</dt>
              <dd>{updateForm.skippedVersion || "—"}</dd>
              {updateCheck && (
                <>
                  <dt>检查结果</dt>
                  <dd>{UPDATE_STATUS_LABELS[updateCheck.status] ?? updateCheck.status}</dd>
                </>
              )}
            </dl>

            <label className="field">
              <span>更新清单地址（留空则不检查）</span>
              <input
                value={updateForm.manifestUrl}
                placeholder="https://example.com/filededup/update.json"
                onChange={(e) => setUpdateForm((prev) => ({ ...prev, manifestUrl: e.target.value }))}
              />
            </label>
            <label className="field">
              <span>检查模式</span>
              <select
                value={updateForm.checkMode}
                onChange={(e) => setUpdateForm((prev) => ({ ...prev, checkMode: e.target.value as "auto" | "manual" }))}
              >
                <option value="auto">自动检查</option>
                <option value="manual">仅手动检查</option>
              </select>
            </label>
            <label className="field">
              <span>检查间隔（小时，1–720）</span>
              <input
                type="number"
                min={1}
                max={720}
                value={updateForm.intervalHours}
                onChange={(e) => setUpdateForm((prev) => ({ ...prev, intervalHours: Number(e.target.value) }))}
              />
            </label>

            <div className="actions">
              <button disabled={updateBusy} onClick={() => void saveAndCheck()}>
                保存并立即检查
              </button>
              {updateForm.skippedVersion && (
                <button className="secondary" disabled={updateBusy} onClick={() => void clearSkipped()}>
                  取消跳过 {updateForm.skippedVersion}
                </button>
              )}
              <button className="secondary" onClick={() => setUpdateOpen(false)}>
                关闭
              </button>
            </div>

            {updateCheck?.status === "available" && (
              <>
                <div className="banner warn" role="status">
                  <span>
                    发现新版本 v{updateCheck.latestVersion}
                    {updateCheck.assetSize ? `（${formatBytes(updateCheck.assetSize)}）` : ""}
                    {updateCheck.releasedAt ? ` · ${updateCheck.releasedAt}` : ""}
                  </span>
                </div>
                {updateCheck.notes && <p className="hint">{updateCheck.notes}</p>}
                <div className="actions">
                  <button className="secondary" disabled={updateBusy} onClick={() => void skipLatest()}>
                    跳过此版本
                  </button>
                  <button className="danger" disabled={updateBusy} onClick={() => void installLatest()}>
                    {installArmed ? "确认覆盖当前程序并重启" : "下载并安装"}
                  </button>
                </div>
                {installArmed && (
                  <p className="hint warn">
                    将下载安装包并覆盖当前程序；替换前会备份为 .old，长度、SHA-256、清单签名或包签名任一不符都会拒绝安装并回滚。
                  </p>
                )}
              </>
            )}

            {updateNote && <p className="hint">{updateNote}</p>}
            <p className="hint">
              清单与安装包都必须通过内置的更新专用 Ed25519 公钥验签，并校验长度与 SHA-256；失败一律拒绝安装，没有“忽略签名继续”的选项。
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
  selectable = false,
  selected,
  onToggle,
  onGroupAction,
}: {
  row: Row;
  onSelect: (path: string) => void;
  selectable?: boolean;
  selected?: Set<string>;
  onToggle?: (group: GroupView, path: string) => void;
  onGroupAction?: (group: GroupView, action: "all" | "none" | "invert" | "keep") => void;
}) {
  if (row.type === "group") {
    const g = row.group;
    let checked = 0;
    if (selected) {
      for (const m of g.members) if (selected.has(m.path)) checked++;
    }
    return (
      <div className="row grouprow" onClick={() => onSelect(g.keep)}>
        <span className="gsel">
          <span className={`kind kind-${g.kind}`}>{KIND_LABELS[g.kind] ?? g.kind}</span>
          {selectable && <span className="selcount">删 {checked}/{g.members.length}</span>}
        </span>
        <span className="gtitle">重复组 · {g.members.length} 个文件</span>
        <span className="gkeep">策略保留：{g.keep.slice(g.keep.lastIndexOf("\\") + 1)}</span>
        {selectable ? (
          <span className="gbuttons" onClick={(e) => e.stopPropagation()}>
            <button className="link" onClick={() => onGroupAction?.(g, "all")}>全选</button>
            <button className="link" onClick={() => onGroupAction?.(g, "none")}>全不选</button>
            <button className="link" onClick={() => onGroupAction?.(g, "invert")}>反选</button>
            <button className="link" onClick={() => onGroupAction?.(g, "keep")} title="把当前勾选项视为要保留的文件，其余成员改为待删除">
              仅保留所选
            </button>
          </span>
        ) : (
          <span className="num">可回收 {formatBytes(g.reclaimable)}</span>
        )}
        <span className="num">{selectable ? `${formatBytes(g.reclaimable)}` : ""}</span>
      </div>
    );
  }
  const m = row.member;
  const checked = selected?.has(m.path) ?? false;
  return (
    <div
      className={`row memberrow ${m.keep ? "iskeep" : ""} ${checked ? "isdel" : "isretain"}`}
      onClick={() => onSelect(m.path)}
    >
      <span className="selcell" onClick={(e) => e.stopPropagation()}>
        {selectable && (
          <input
            type="checkbox"
            checked={checked}
            onChange={() => onToggle?.(row.group, m.path)}
            aria-label={checked ? `取消删除 ${m.name}` : `标记删除 ${m.name}`}
          />
        )}
        <span className={`kind ${checked ? "kind-del" : "kind-keep"}`}>{checked ? "将删除" : "保留"}</span>
      </span>
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
        目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、
        九步整理（试运行/真实执行/存疑项确认/最近一次撤销/规则导入导出）、
        自动更新（Ed25519 清单与安装包验签、备份与回滚）、
        Ed25519 许可证导入与状态显示、旧版 ECDSA 许可证只读兼容、试用期（PBKDF2 + 双存储 + 回拨检测）
      </p>
      <p>
        <span className="kind kind-warn">未联调</span>{" "}
        蓝奏云上传页（登录/目录浏览/预检/上传门面已接通，真实列表与上传需要蓝奏账号验证）
      </p>
      <p>
        <span className="kind kind-warn">未实现</span>{" "}
        系统托盘、自定义保留项、查重侧执行撤销、撤销历史
      </p>
    </div>
  );
}
