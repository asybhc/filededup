import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api, isDesktop } from "./api";
import { onAppEvent } from "./api";
import { mock } from "./mock";
import { VirtualList } from "./components/VirtualList";
import { Organizer } from "./Organizer";
import { Lanzou } from "./Lanzou";
import { SettingsDialog } from "./Settings";
import { Search } from "./Search";
import TimeOrganizer from "./TimeOrganizer";
import { DiagnosticsDialog } from "./Diagnostics";
import logo from "./assets/logo.png";
import { getLang, setLang, tr, useT } from "./i18n";
import type {
  AppInfo,
  DedupUndoInfo,
  DedupUndoResult,
  DeleteMode,
  ExecuteState,
  ExportFormat,
  ExportResult,
  FullDiskPlan,
  GroupView,
  ImagePreview,
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

const IMAGE_EXTS = /\.(jpe?g|png|bmp|gif|webp|tiff?)$/i;

function isImagePath(path: string): boolean {
  return IMAGE_EXTS.test(path);
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

// 列表分隔符随界面语言变化：中文用顿号，英文用逗号加空格。
function listSep(): string {
  return getLang() === "zh" ? "、" : ", ";
}

function licenseSourceLabel(src?: string): string {
  switch (src) {
    case "ed25519":
      return tr("Ed25519 许可证");
    case "legacy-ecdsa":
      return tr("旧版 ECDSA 许可证（只读兼容）");
    case "trial":
      return tr("试用期");
    default:
      return tr("无");
  }
}

function licenseLabel(st?: LicenseState): string {
  if (!st) return tr("授权状态读取中");
  switch (st.status) {
    case "licensed":
      return st.forever
        ? tr("已授权 · {edition} · 永久", { edition: st.edition ?? "" })
        : tr("已授权 · {expiry} 到期", { expiry: st.expiry ?? "" });
    case "trial":
      return tr("试用中 · 剩余 {days} 天", { days: st.trialDaysLeft });
    case "trial-expired":
      return tr("试用已结束 · 未授权");
    default:
      return tr("未授权");
  }
}

export default function App() {
  const t = useT();
  const [info, setInfo] = useState<AppInfo>({ version: "", platform: "" });
  const [step, setStep] = useState<Step>("pick");
  const [req, setReq] = useState<ScanRequest & { minSizeMB: number }>({
    root: "",
    keepPolicy: "pathShortest",
    similarImage: false,
    imageThreshold: 8,
    similarAudio: false,
    audioTolerance: 2,
    minSize: 0,
    includeHidden: false,
    excludes: [],
    fullDisk: false,
    quickHash: false,
    minSizeMB: 0,
  });
  const [excludesText, setExcludesText] = useState("");
  // 全盘查重预检：可选卷与后端追加的强制排除项。
  const [fullDiskPlan, setFullDiskPlan] = useState<FullDiskPlan>();
  const [fullDiskNote, setFullDiskNote] = useState("");
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
  // 查重结果导出：HTML/CSV 写到本地，成功后就地显示路径。
  const [exportBusy, setExportBusy] = useState(false);
  const [exportNote, setExportNote] = useState("");
  const [exportError, setExportError] = useState("");
  const [exportResult, setExportResult] = useState<ExportResult>();
  const [error, setError] = useState("");
  const [detail, setDetail] = useState("");
  const [skipOpen, setSkipOpen] = useState(false);
  const [skips, setSkips] = useState<SkipView[]>([]);
  const [perfRows, setPerfRows] = useState<Row[]>([]);
  const [perfActive, setPerfActive] = useState(false);
  const [fps, setFps] = useState(0);
  // 结果页排序与筛选：只影响显示顺序/可见组，不改变勾选状态与执行范围。
  const [resultSort, setResultSort] = useState<"default" | "size" | "reclaimable" | "name" | "time">("default");
  const [resultDesc, setResultDesc] = useState(true);
  const [resultQuery, setResultQuery] = useState("");
  const [resultKind, setResultKind] = useState("all");
  // 大图预览：按需解码，不自动加载，避免大目录卡顿。
  const [imagePreview, setImagePreview] = useState<ImagePreview>();
  const [imagePreviewBusy, setImagePreviewBusy] = useState(false);
  const [imagePreviewError, setImagePreviewError] = useState("");
  const [view, setView] = useState<"dedup" | "organize" | "timeline" | "lanzou" | "search">("dedup");
  const [settings, setSettings] = useState<UISettings>();
  const [settingsReady, setSettingsReady] = useState(false);
  const [settingsPath, setSettingsPath] = useState("");
  const [settingsNote, setSettingsNote] = useState("");
  const settingsRef = useRef<UISettings | undefined>(undefined);
  const [license, setLicense] = useState<LicenseState>();
  const [licenseOpen, setLicenseOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [diagOpen, setDiagOpen] = useState(false);
  // 拖放目录的结果提示：成功/不是文件夹/读取失败都在这里说明。
  const [dropNote, setDropNote] = useState("");
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
        setSettingsNote(t("设置读取失败，已使用默认值：{error}", { error: errorText(err) }));
        setSettingsReady(true);
      });
    return () => {
      alive = false;
    };
  }, []);

  // 界面语言跟随设置；设置对话框或顶栏切换后立即生效。
  useEffect(() => {
    if (settings?.lang) setLang(settings.lang);
  }, [settings?.lang]);

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

  // 拖放文件夹：Wails 原生拖放事件给绝对路径；只有“选择目录”步骤、且落点带
  // --wails-drop-target 的元素才接收，其余区域保持默认拖放行为。
  useEffect(() => {
    if (!isDesktop) return;
    const rt = (window as any).runtime;
    if (!rt || typeof rt.OnFileDrop !== "function") return;
    rt.OnFileDrop((_x: number, _y: number, paths: string[]) => {
      if (view !== "dedup" || step !== "pick" || !paths || !paths.length) return;
      api
        .inspectDroppedPath(paths[0])
        .then((r) => {
          setDropNote(r.message);
          if (r.path && (r.isDir || r.fromFile)) setReq((prev) => ({ ...prev, root: r.path }));
        })
        .catch((err) => setDropNote(t("拖放处理失败：{error}", { error: errorText(err) })));
    });
    return () => {
      if (typeof rt.OnFileDropOff === "function") rt.OnFileDropOff();
    };
  }, [view, step]);

  // F1 打开帮助，Esc 收起最上层的浮层；只处理键盘事件，不改业务状态。
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "F1") {
        e.preventDefault();
        setHelpOpen(true);
        return;
      }
      if (e.key !== "Escape") return;
      if (helpOpen) {
        setHelpOpen(false);
        return;
      }
      if (diagOpen) {
        setDiagOpen(false);
        return;
      }
      if (updateOpen) {
        setUpdateOpen(false);
        return;
      }
      if (licenseOpen) {
        setLicenseOpen(false);
        return;
      }
      if (settingsOpen) {
        setSettingsOpen(false);
        return;
      }
      if (skipOpen) {
        setSkipOpen(false);
        return;
      }
      if (undoConfirm) {
        setUndoConfirm(false);
        return;
      }
      if (confirmOpen) setConfirmOpen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [helpOpen, diagOpen, updateOpen, licenseOpen, settingsOpen, skipOpen, undoConfirm, confirmOpen]);

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
      api.saveSettings(next).catch((err) => setSettingsNote(t("设置保存失败：{error}", { error: errorText(err) })));
    }, 600);
    return () => window.clearTimeout(timer);
  }, [req, deleteMode, excludesText]);

  const persistOrganize = (next: OrganizeSettings) => {
    const base = settingsRef.current;
    if (!base) return;
    const merged: UISettings = { ...base, organize: next };
    settingsRef.current = merged;
    setSettings(merged);
    api.saveSettings(merged).catch((err) => setSettingsNote(t("设置保存失败：{error}", { error: errorText(err) })));
  };

  const persistLanzou = (partial: Partial<LanzouSettings>) => {
    const base = settingsRef.current;
    if (!base) return;
    const merged: UISettings = { ...base, lanzou: { ...base.lanzou, ...partial } };
    settingsRef.current = merged;
    setSettings(merged);
    api.saveSettings(merged).catch((err) => setSettingsNote(t("设置保存失败：{error}", { error: errorText(err) })));
  };

  // 启动后按配置自动检查一次；有更新只点亮入口，不主动弹窗。
  useEffect(() => {
    if (!settingsReady || autoChecked.current) return;
    autoChecked.current = true;
    const cfg = settingsRef.current?.update;
    if (!cfg) return;
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
    if (manual) setUpdateNote(t("正在检查更新…"));
    try {
      const v = await api.checkForUpdate(manual);
      setUpdateCheck(v);
      if (v.message) setUpdateNote(v.message);
    } catch (err) {
      setUpdateCheck(undefined);
      setUpdateNote(t("检查失败：{error}", { error: errorText(err) }));
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
      setUpdateNote(t("更新源必须是 http:// 或 https:// 地址。"));
      return;
    }
    const interval = Math.min(720, Math.max(1, Number(updateForm.intervalHours) || 18));
    const next: UpdateSettings = { ...updateForm, manifestUrl: url, intervalHours: interval };
    setUpdateBusy(true);
    setUpdateNote(t("正在保存并检查…"));
    try {
      await persistUpdate(next);
      const v = await api.checkForUpdate(true);
      setUpdateCheck(v);
      setUpdateNote(v.message || t("检查完成。"));
    } catch (err) {
      setUpdateCheck(undefined);
      setUpdateNote(t("检查失败：{error}", { error: errorText(err) }));
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
      setUpdateCheck((prev) =>
        prev ? { ...prev, status: "skipped", message: t("已跳过版本 {version}。", { version: v }) } : prev,
      );
      setUpdateNote(t("已跳过版本 {version}，后续检查不再提示。", { version: v }));
    } catch (err) {
      setUpdateNote(t("跳过版本失败：{error}", { error: errorText(err) }));
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
      setUpdateNote(t("已取消跳过版本。"));
      setUpdateCheck(undefined);
    } catch (err) {
      setUpdateNote(t("取消跳过失败：{error}", { error: errorText(err) }));
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
    setUpdateNote(t("正在下载、验签并替换程序，请勿关闭窗口…"));
    try {
      const r = await api.installUpdate();
      setUpdateNote(t("已更新到 {version}，程序即将重启。备份：{backup}", { version: r.version, backup: r.backupPath }));
    } catch (err) {
      // 浏览器预览会在这里直接给出“不能安装”的明确原因。
      setUpdateNote(t("安装已取消：{error}", { error: errorText(err) }));
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
        if (alive) setLicenseNote(t("授权状态读取失败：{error}", { error: errorText(err) }));
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
          setError(st.error || t("扫描失败"));
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

  async function switchLang() {
    const base = settingsRef.current;
    if (!base) return;
    const next: UISettings = { ...base, lang: base.lang === "en" ? "zh" : "en" };
    try {
      await api.saveSettings(next);
      settingsRef.current = next;
      setSettings(next);
      setLang(next.lang);
      setSettingsNote("");
    } catch (err) {
      setSettingsNote(t("语言设置保存失败：{error}", { error: errorText(err) }));
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

  async function toggleFullDisk(next: boolean) {
    setFullDiskNote("");
    if (!next) {
      setReq((prev) => ({ ...prev, fullDisk: false }));
      return;
    }
    try {
      const plan = await api.getFullDiskPlan();
      setFullDiskPlan(plan);
      if (!plan.supported) {
        setFullDiskNote(plan.message || t("当前环境不支持全盘查重"));
        setReq((prev) => ({ ...prev, fullDisk: false }));
        return;
      }
      setReq((prev) => ({ ...prev, fullDisk: true }));
    } catch (err) {
      setFullDiskNote(errorText(err));
      setReq((prev) => ({ ...prev, fullDisk: false }));
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
      setSettingsNote(t("定时扫描：{message}", { message: payload?.message ?? payload?.status ?? t("已结束") }));
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
      setSelNote(t("每组至少要保留一个文件：已阻止把整组都勾选删除。"));
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
        setSelNote(t("反转后会删除整组，已自动留出「{name}」保留。", { name: fallback.name }));
      }
    } else {
      const kept = group.members.filter((m) => next.has(m.path));
      if (kept.length === 0) {
        setSelNote(t("请先勾选要保留的文件，再点「仅保留所选」。"));
        return;
      }
      for (const m of group.members) {
        if (next.has(m.path)) next.delete(m.path);
        else next.add(m.path);
      }
      setSelNote(
        t("已保留勾选的 {kept} 个文件，其余 {rest} 个改为待删除。", {
          kept: kept.length,
          rest: group.members.length - kept.length,
        }),
      );
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
      setSelNote(t("反选后有 {count} 组会变成整组删除，已自动留出每组一个保留项。", { count: adjusted }));
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

  async function doExport(format: ExportFormat) {
    setExportBusy(true);
    setExportNote("");
    setExportError("");
    try {
      const res = await api.exportResults(sessionId, format);
      setExportResult(res);
      setExportNote(t("已导出 {format}：{path}", { format: format.toUpperCase(), path: res.path }));
    } catch (err) {
      setExportResult(undefined);
      setExportError(t("导出失败：{error}", { error: errorText(err) }));
    } finally {
      setExportBusy(false);
    }
  }

  // 结果页逐文件「打开所在位置」：在文件管理器中定位该文件。
  async function revealResult(path: string) {
    try {
      await api.revealPath(path);
    } catch (err) {
      setError(t("打开所在位置失败：{error}", { error: errorText(err) }));
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
        setUndoNote(t("回收站已被清空，删除的文件无法自动恢复。"));
      } else if (res.failed > 0) {
        setUndoNote(t("有 {count} 个文件未能恢复，原因见下方逐条说明。", { count: res.failed }));
      }
      const info = await api.getDedupUndoInfo();
      setUndoInfo(info);
    } catch (err) {
      setUndoNote(t("撤销失败：{error}", { error: errorText(err) }));
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
      if (st.licensed) setLicenseNote(t("许可证导入成功。"));
      else setLicenseNote(st.warning || t("未识别到有效许可证。"));
    } catch (err) {
      setLicenseNote(t("导入失败：{error}", { error: errorText(err) }));
    }
  }

  async function importLicensePasted() {
    setLicenseNote("");
    try {
      const st = await api.importLicenseText(licenseText);
      setLicense(st);
      setLicenseText("");
      setLicenseNote(st.licensed ? t("许可证导入成功。") : st.warning || t("未识别到有效许可证。"));
    } catch (err) {
      setLicenseNote(t("导入失败：{error}", { error: errorText(err) }));
    }
  }

  async function copyMachineCode() {
    const code = license?.machineCode ?? "";
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      setLicenseNote(t("机器码已复制。"));
    } catch {
      setLicenseNote(t("复制失败，请手动选中机器码复制。"));
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
    setExportBusy(false);
    setExportNote("");
    setExportError("");
    setExportResult(undefined);
    setPerfActive(false);
    setPerfRows([]);
    setError("");
    setDetail("");
  }

  const visibleGroups = useMemo(() => {
    let out = groups;
    if (resultKind !== "all") out = out.filter((g) => g.kind === resultKind);
    const q = resultQuery.trim().toLowerCase();
    if (q) {
      out = out.filter(
        (g) =>
          g.keep.toLowerCase().includes(q) ||
          g.members.some((m) => m.path.toLowerCase().includes(q) || m.name.toLowerCase().includes(q)),
      );
    }
    if (resultSort === "default") return out;
    const maxSize = (g: GroupView) => g.members.reduce((acc, m) => Math.max(acc, m.size), 0);
    const maxTime = (g: GroupView) => g.members.reduce((acc, m) => (m.modTime > acc ? m.modTime : acc), "");
    const keepName = (g: GroupView) => g.keep.slice(g.keep.lastIndexOf("\\") + 1).toLowerCase();
    const sorted = [...out].sort((a, b) => {
      let cmp = 0;
      switch (resultSort) {
        case "size":
          cmp = maxSize(a) - maxSize(b);
          break;
        case "reclaimable":
          cmp = a.reclaimable - b.reclaimable;
          break;
        case "name":
          cmp = keepName(a).localeCompare(keepName(b), "zh-CN");
          break;
        case "time":
          cmp = maxTime(a).localeCompare(maxTime(b));
          break;
      }
      return resultDesc ? -cmp : cmp;
    });
    return sorted;
  }, [groups, resultKind, resultQuery, resultSort, resultDesc]);

  useEffect(() => {
    setImagePreview(undefined);
    setImagePreviewError("");
  }, [detail]);

  async function loadImagePreview() {
    if (!detail) return;
    setImagePreviewBusy(true);
    setImagePreviewError("");
    try {
      setImagePreview(await api.readImagePreview(detail, 1600));
    } catch (err) {
      setImagePreviewError(errorText(err));
    } finally {
      setImagePreviewBusy(false);
    }
  }

  const rows = useMemo(() => (perfActive ? perfRows : flatten(visibleGroups)), [perfActive, perfRows, visibleGroups]);
  const stepIndex = STEPS.findIndex((s) => s.key === step);

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <img className="logo" src={logo} alt={t("文件整理.一杯好茶")} />
          <div>
            <h1>{t("文件整理.一杯好茶")}</h1>
            <p>{t("本地扫描 · 回收站可恢复 · 云端上传为可选")}</p>
          </div>
        </div>
        <div className="topmeta">
          {!isDesktop && <span className="badge warn">{t("浏览器预览（合成数据）")}</span>}
          <button
            className={`badge license-badge ${license?.status ?? ""}`}
            title={t("查看授权状态、机器码与许可证导入")}
            onClick={() => {
              setLicenseNote("");
              setLicenseOpen(true);
            }}
          >
            {licenseLabel(license)}
          </button>
          <button
            className="badge"
            title={t("切换界面语言（中文 / English）")}
            onClick={() => void switchLang()}
            disabled={!settings}
          >
            {getLang() === "en" ? "中文" : "English"}
          </button>
          <button
            className="badge"
            title={t("设置：外观、托盘、定时任务、AI、日志")}
            onClick={() => setSettingsOpen(true)}
          >
            {t("设置")}
          </button>
          <button className="badge" title={t("帮助（快捷键 F1）")} onClick={() => setHelpOpen(true)}>
            {t("帮助 F1")}
          </button>
          <button className="badge" title={t("查看日志与环境自检")} onClick={() => setDiagOpen(true)}>
            {t("诊断")}
          </button>
          <span className="badge">v{info.version || "—"}</span>
          <button
            className={`badge update-badge ${updateCheck?.status === "available" ? "has-update" : ""}`}
            title={t("检查更新（清单与安装包均验签）")}
            onClick={openUpdate}
          >
            {updateCheck?.status === "available"
              ? t("有更新 v{version}", { version: updateCheck.latestVersion ?? "" })
              : t("检查更新")}
          </button>
          <span className="badge">{info.platform || "—"}</span>
        </div>
      </header>

      {licenseStop && (
        <div className="license-banner" role="status">
          <span>
            <b>{t("未授权")}</b>
            {t("：")}
            {license?.limits}
            {license?.warning ? ` ${license.warning}` : ""}
          </span>
          <button className="secondary" onClick={() => setLicenseOpen(true)}>
            {t("去授权")}
          </button>
        </div>
      )}

      <nav className="tabs" aria-label={t("模块")}>
        <button className={view === "dedup" ? "active" : ""} onClick={() => setView("dedup")}>
          {t("查重与清理")}
        </button>
        <button className={view === "organize" ? "active" : ""} onClick={() => setView("organize")}>
          {t("九步整理")}
        </button>
        <button className={view === "timeline" ? "active" : ""} onClick={() => setView("timeline")}>
          {t("按时间整理")}
        </button>
        <button className={view === "lanzou" ? "active" : ""} onClick={() => setView("lanzou")}>
          {t("蓝奏云上传")}
        </button>
        <button className={view === "search" ? "active" : ""} onClick={() => setView("search")}>
          {t("本地搜索")}
        </button>
        {view === "dedup" && (
          <span className="tabs-hint">
            {t("当前流程：{steps}", { steps: STEPS.map((s) => t(s.label)).join(" → ") })}
          </span>
        )}
      </nav>

      {view === "dedup" && (
        <nav className="stepper" aria-label={t("流程")}>
          {STEPS.map((s, i) => (
            <div key={s.key} className={`step ${i === stepIndex ? "active" : ""} ${i < stepIndex ? "done" : ""}`}>
              <span className="step-no">{i + 1}</span>
              <span>{t(s.label)}</span>
            </div>
          ))}
        </nav>
      )}

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            {t("关闭")}
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
              licenseHint={t("未授权：整理的真实执行已禁用，请先导入许可证；试运行仍可用。")}
            />
          ) : (
            <section className="card">
              <h2>{t("九步整理")}</h2>
              <p className="hint">{t("正在读取设置…")}</p>
            </section>
          )
        ) : view === "timeline" ? (
          <TimeOrganizer canRun={!licenseStop} licenseHint={t("未授权：按时间整理的真实执行已禁用，预演仍可用。")} />
        ) : view === "lanzou" ? (
          settingsReady ? (
            <Lanzou
              initial={settings?.lanzou}
              onPersist={persistLanzou}
              canRun={!licenseStop}
              licenseHint={t("未授权：开始上传会被拒绝，登录与预检仍可用。")}
            />
          ) : (
            <section className="card">
              <h2>{t("蓝奏云上传")}</h2>
              <p className="hint">{t("正在读取设置…")}</p>
            </section>
          )
        ) : view === "search" ? (
          <Search
            canRun={searchFeature?.unlocked ?? false}
            licenseHint={
              searchFeature
                ? t("未解锁：{reason}（本机机器码 {code}）。", {
                    reason: searchFeature.reason ?? t("本地搜索需要标准版许可证"),
                    code: license?.machineCode ?? t("读取中"),
                  })
                : t("正在读取授权状态，请稍候。")
            }
          />
        ) : (
          <>
        {step === "pick" && (
          <section className="card">
            <h2>{t("1. 选择目录与扫描设置")}</h2>
            <div className="dirrow dropzone">
              <input
                className="path"
                readOnly
                value={
                  req.fullDisk
                    ? t("全盘：{volumes}", {
                        volumes: (fullDiskPlan?.volumes ?? []).map((v) => v.path).join(listSep()),
                      })
                    : req.root
                }
                placeholder={t("尚未选择目录（也可把文件夹拖到这里）")}
                aria-label={t("扫描目录")}
                disabled={req.fullDisk}
              />
              <button className="secondary" onClick={pickDirectory} disabled={req.fullDisk}>
                {t("选择目录")}
              </button>
            </div>
            {dropNote && <p className="hint">{dropNote}</p>}

            <div className="formgrid">
              <label>
                <span>{t("保留策略")}</span>
                <select
                  value={req.keepPolicy}
                  onChange={(e) => setReq({ ...req, keepPolicy: e.target.value as KeepPolicy })}
                >
                  {Object.entries(KEEP_LABELS).map(([value, label]) => (
                    <option key={value} value={value}>
                      {t(label)}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                <span>{t("删除方式")}</span>
                <select value={deleteMode} onChange={(e) => setDeleteMode(e.target.value as DeleteMode)}>
                  <option value="trash">{t("移入回收站（可恢复）")}</option>
                  <option value="permanent">{t("永久删除（不可恢复）")}</option>
                </select>
              </label>
              <label>
                <span>{t("最小文件大小（MB，0 = 不限）")}</span>
                <input
                  type="number"
                  min={0}
                  step={1}
                  value={req.minSizeMB}
                  onChange={(e) => setReq({ ...req, minSizeMB: Math.max(0, Number(e.target.value) || 0) })}
                />
              </label>
              <label>
                <span>{t("排除目录（目录名或路径前缀；逗号或空格分隔）")}</span>
                <input
                  value={excludesText}
                  onChange={(e) => setExcludesText(e.target.value)}
                  placeholder={t("例如 node_modules .git")}
                />
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.fullDisk}
                  onChange={(e) => void toggleFullDisk(e.target.checked)}
                />
                <span>{t("全盘查重（扫描所有本地固定磁盘，自动跳过系统目录与卷根系统文件）")}</span>
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.quickHash}
                  disabled={req.similarImage}
                  onChange={(e) => setReq({ ...req, quickHash: e.target.checked })}
                />
                <span>
                  {t("64KB 快筛（先用前 64KB 指纹滤掉不可能重复的文件，候选项仍做完整 SHA-256 复核）")}
                  {req.similarImage ? t("（相似图片开启时不可用）") : ""}
                </span>
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.similarImage}
                  onChange={(e) =>
                    setReq({
                      ...req,
                      similarImage: e.target.checked,
                      quickHash: e.target.checked ? false : req.quickHash,
                    })
                  }
                />
                <span>{t("相似图片（dHash）")}</span>
              </label>
              <label>
                <span title={t("0 = 指纹完全相同；数值越大判定越宽松，越容易把不同的图片当成相似；范围 0-16，默认 8")}>
                  {t("相似阈值（0 = 完全相同，越大越宽松；默认 8，上限 16）")}
                </span>
                <input
                  type="number"
                  min={0}
                  max={16}
                  value={req.imageThreshold}
                  disabled={!req.similarImage}
                  onChange={(e) => setReq({ ...req, imageThreshold: Math.max(0, Math.min(16, Number(e.target.value) || 0)) })}
                />
              </label>
              <label className="checkline">
                <input
                  type="checkbox"
                  checked={req.similarAudio}
                  onChange={(e) => setReq({ ...req, similarAudio: e.target.checked })}
                />
                <span>{t("相似音频（时长容差）")}</span>
              </label>
              <label>
                <span>{t("音频容差（秒）")}</span>
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
                <span>{t("包含隐藏/系统文件（不勾选 = 跳过隐藏与系统文件）")}</span>
              </label>
            </div>

            <p className="hint">
              {t("排除项只匹配目录：填目录名会跳过所有同名目录及其内容，填带路径的项按路径前缀匹配；不会按文件名子串过滤。")}
            </p>

            {fullDiskNote && <p className="hint">{fullDiskNote}</p>}
            {req.fullDisk && fullDiskPlan?.supported && (
              <p className="hint">
                {t(
                  "全盘范围：{volumes}。强制排除 {count} 项：Windows、Program Files、Program Files (x86)、ProgramData、$Recycle.Bin、System Volume Information、Recovery 等系统目录，以及各卷根下的 pagefile.sys、hiberfil.sys、swapfile.sys。这些项不受上方排除设置影响。",
                  {
                    volumes: fullDiskPlan.volumes
                      .map((v) =>
                        t("{path}（可用 {free}）", { path: v.path, free: formatBytes(v.free) }),
                      )
                      .join(listSep()),
                    count: fullDiskPlan.hardExcludes.length,
                  },
                )}
              </p>
            )}
            {req.quickHash && (
              <p className="hint">
                {t("快筛只减少需要完整哈希的文件数量；最终分组与保留/删除判断仍以完整 SHA-256 结果为准。")}
              </p>
            )}

            <p className="hint">
              {t(
                "相似图片默认关闭，勾选后才参与查重。阈值 0 表示指纹完全相同；数值越大判定越宽松，更容易把不同的图片当成相似（误判风险更高），数值越小越严格；可调范围 0-16，默认 8。",
              )}
            </p>

            <p className="hint">
              {settingsPath ? (
                <>
                  {t("设置自动保存到 {path}，重启后自动恢复。", { path: settingsPath })}
                  {settingsNote ? t("（{note}）", { note: settingsNote }) : ""}
                </>
              ) : (
                t("正在读取设置…")
              )}
            </p>

            <div className="actions">
              <button className="primary" disabled={!req.fullDisk && !req.root} onClick={startScan}>
                {t("开始扫描")}
              </button>
            </div>

            <FeatureStatus />
          </section>
        )}

        {step === "scan" && (
          <section className="card">
            <h2>{t("2. 扫描中")}</h2>
            <p className="mono ellipsis" title={req.fullDisk ? t("全盘查重") : req.root}>
              {req.fullDisk ? t("全盘查重：所有本地固定磁盘") : req.root}
            </p>
            <ScanProgress scan={scan} onCancel={cancelScan}>
              <button className="secondary" onClick={reset}>
                {t("返回")}
              </button>
            </ScanProgress>
          </section>
        )}

        {step === "results" && (
          <section className="card fill">
            <div className="sectionhead">
              <h2>{t("3. 查重结果")}</h2>
              <div className="actions inline">
                <button className="secondary" onClick={reset}>
                  {t("重新扫描")}
                </button>
                <button
                  className="secondary"
                  onClick={() => doExport("html")}
                  disabled={perfActive || !sessionId || exportBusy}
                  title={t("导出包含分组、成员、大小、保留策略与扫描参数的报告")}
                >
                  {t("导出 HTML")}
                </button>
                <button className="secondary" onClick={() => doExport("csv")} disabled={perfActive || !sessionId || exportBusy}>
                  {t("导出 CSV")}
                </button>
                <button
                  className="primary"
                  onClick={openPreview}
                  disabled={perfActive || rows.length === 0 || deleteSel.size === 0}
                  title={!perfActive && deleteSel.size === 0 ? t("请先勾选要删除的文件") : undefined}
                >
                  {t("生成执行预览")}
                </button>
              </div>
            </div>

            <div className="chips">
              <span className="chip">
                {t("重复组")} <b>{scan?.groups ?? groupsTotal}</b>
              </span>
              <span className="chip">
                {t("重复文件")} <b>{scan?.duplicateFiles ?? 0}</b>
              </span>
              <span className="chip">
                {t("可回收")} <b>{formatBytes(scan?.reclaimable ?? 0)}</b>
              </span>
              <span className="chip">
                {t("跳过")} <b>{scan?.skipped ?? 0}</b>
              </span>
              <span className="chip">
                {t("保留策略")} <b>{t(KEEP_LABELS[req.keepPolicy])}</b>
              </span>
              <button className="link" onClick={openSkips} disabled={!sessionId}>
                {t("查看跳过项")}
              </button>
            </div>

            {exportNote && (
              <p className="hint">
                {exportNote}
                {exportResult ? (
                  <>
                    {" "}
                    <button className="link" onClick={() => api.revealPath(exportResult.path)}>
                      {t("在资源管理器中显示")}
                    </button>
                  </>
                ) : null}
              </p>
            )}
            {exportError && <p className="hint warn">{exportError}</p>}

            {perfActive && (
              <div className="banner perf">
                <span>
                  {t("性能自检：当前是 {rows} 行合成数据（≥ 10 万），不是真实扫描结果。滚动帧率约 {fps} fps（1 秒采样）。", {
                    rows: perfRows.length.toLocaleString("zh-CN"),
                    fps: fps || "—",
                  })}
                </span>
                <button className="link" onClick={() => setPerfActive(false)}>
                  {t("退出自检")}
                </button>
              </div>
            )}

            {!perfActive && (
              <div className="toolbar">
                <span className="hint">
                  {loadingGroups
                    ? t("正在载入结果：{loaded} / {total} 组", { loaded: groups.length, total: groupsTotal })
                    : t("共 {total} 组，已全部载入；点击任意行查看完整路径。", { total: groupsTotal })}
                </span>
                <button className="link" onClick={runPerf}>
                  {t("性能自检：载入 10 万行合成数据")}
                </button>
              </div>
            )}

            {!perfActive && (
              <div className="toolbar">
                <span className="hint">
                  {t("勾选 = 删除，未勾选 = 保留；已勾选 {count} 项，执行范围以勾选为准，不再按整组删除。", {
                    count: deleteSel.size,
                  })}
                  {selNote ? <span className="warn"> {selNote}</span> : null}
                </span>
                <div className="actions inline">
                  <button className="link" onClick={selectAllDeletable}>
                    {t("全选可删除项")}
                  </button>
                  <button className="link" onClick={invertAllSelection}>
                    {t("反选")}
                  </button>
                  <button className="link" onClick={clearAllSelection}>
                    {t("全不选")}
                  </button>
                </div>
              </div>
            )}

            {!perfActive && (
              <div className="toolbar filterbar">
                <span className="filterfields">
                  <input
                    className="search"
                    placeholder={t("筛选：路径或文件名包含…")}
                    value={resultQuery}
                    onChange={(e) => setResultQuery(e.target.value)}
                    aria-label={t("筛选结果")}
                  />
                  <select value={resultKind} onChange={(e) => setResultKind(e.target.value)} aria-label={t("按类型筛选")}>
                    <option value="all">{t("全部类型")}</option>
                    {Object.entries(KIND_LABELS).map(([value, label]) => (
                      <option key={value} value={value}>
                        {t(label)}
                      </option>
                    ))}
                  </select>
                  <select
                    value={resultSort}
                    onChange={(e) => setResultSort(e.target.value as typeof resultSort)}
                    aria-label={t("排序方式")}
                  >
                    <option value="default">{t("默认顺序")}</option>
                    <option value="size">{t("按文件大小")}</option>
                    <option value="reclaimable">{t("按可回收空间")}</option>
                    <option value="name">{t("按保留项文件名")}</option>
                    <option value="time">{t("按修改时间")}</option>
                  </select>
                  <button className="secondary" onClick={() => setResultDesc((v) => !v)} title={t("切换升序/降序")}>
                    {resultDesc ? t("降序 ↓") : t("升序 ↑")}
                  </button>
                </span>
                <span className="hint">
                  {t("显示 {shown} / {total} 组（筛选只影响显示，勾选与执行范围不变）", {
                    shown: visibleGroups.length,
                    total: groupsTotal,
                  })}
                </span>
              </div>
            )}

            <div className="tablehead">
              <span>{t("选择 / 状态")}</span>
              <span>{t("文件 / 组")}</span>
              <span>{t("完整路径")}</span>
              <span className="num">{t("大小")}</span>
              <span>{t("修改时间")}</span>
              <span>{t("操作")}</span>
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
                  onReveal={perfActive ? undefined : revealResult}
                />
              )}
            />
          </section>
        )}

        {step === "preview" && (
          <section className="card fill">
            <div className="sectionhead">
              <h2>{t("4. 执行预览")}</h2>
              <div className="actions inline">
                <button className="secondary" onClick={() => setStep("results")}>
                  {t("返回结果")}
                </button>
                <button
                  className="danger"
                  disabled={!preview || preview.total === 0 || Boolean(exec?.running) || licenseStop}
                  title={licenseStop ? t("未授权，请先在“授权”中导入许可证") : undefined}
                  onClick={() => {
                    setPermanentAck(false);
                    setConfirmOpen(true);
                  }}
                >
                  {t("确认执行")}
                </button>
              </div>
            </div>

            {licenseStop && (
              <p className="hint">
                {t("未授权：确认执行已禁用，只能扫描与预览。请先在“授权”中导入许可证。")}
                {license?.machineCode ? (
                  <>
                    {" "}
                    {t("机器码：{code}", { code: license.machineCode })}
                  </>
                ) : null}
              </p>
            )}

            <p className="hint">
              {t("本次只处理结果页勾选的 {count} 个文件；同一组未勾选的副本会保留。", {
                count: deleteSel.size,
              })}
            </p>

            <div className="formgrid narrow">
              <label>
                <span>{t("删除方式")}</span>
                <select value={deleteMode} onChange={(e) => setDeleteMode(e.target.value as DeleteMode)}>
                  <option value="trash">{t("移入回收站（可恢复）")}</option>
                  <option value="permanent">{t("永久删除（不可恢复）")}</option>
                </select>
              </label>
            </div>

            {deleteMode === "permanent" && (
              <div className="banner danger">
                {t("永久删除不可恢复，也不会进入回收站。请确认已备份重要数据。")}
              </div>
            )}

            <div className="chips">
              <span className="chip">
                {t("动作总数")} <b>{preview?.total ?? 0}</b>
              </span>
              <span className="chip">
                {t("涉及组")} <b>{preview?.groups ?? 0}</b>
              </span>
              <span className="chip">
                {t("可回收")} <b>{formatBytes(preview?.reclaimableBytes ?? 0)}</b>
              </span>
              {preview &&
                Object.entries(preview.kindCounts).map(([kind, count]) => (
                  <span className="chip" key={kind}>
                    {kind === "delete_permanent" ? t("永久删除") : t("回收站")} <b>{count}</b>
                  </span>
                ))}
            </div>

            {exec?.running && (
              <div className="banner info">
                <span>{t("正在执行：{count} 个动作，请勿关闭窗口。", { count: exec.total })}</span>
              </div>
            )}

            <h3 className="subhead">{t("动作样例（前 200 条，共 {count} 条）", { count: preview?.total ?? 0 })}</h3>
            <div className="samples">
              {(preview?.samples ?? []).map((a) => (
                <div className="sample" key={a.seq}>
                  <span className="seq">#{a.seq}</span>
                  <span className={`kind kind-${a.kind === "delete_permanent" ? "danger" : "exact"}`}>
                    {a.kind === "delete_permanent" ? t("永久删除") : t("回收站")}
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
            <h2>{t("5. 执行完成")}</h2>
            <div className="stats">
              <div className="stat">
                <span>{t("成功")}</span>
                <b>{exec?.applied ?? 0}</b>
              </div>
              <div className="stat">
                <span>{t("失败")}</span>
                <b className={exec?.failed ? "red" : ""}>{exec?.failed ?? 0}</b>
              </div>
              <div className="stat">
                <span>{t("跳过")}</span>
                <b>{exec?.skipped ?? 0}</b>
              </div>
              <div className="stat">
                <span>{t("回滚")}</span>
                <b>{exec?.rolledBack ?? 0}</b>
              </div>
              <div className="stat wide">
                <span>{t("释放空间")}</span>
                <b>{formatBytes(exec?.reclaimed ?? 0)}</b>
              </div>
            </div>
            {exec?.error && <div className="banner error">{exec.error}</div>}

            <div className="sectionhead">
              <h3>{t("撤销上次删除")}</h3>
              <span className={`badge ${undoInfo?.available ? "warn" : ""}`}>
                {undoInfo?.available
                  ? t("可撤销 {count} 个文件", { count: undoInfo.trash })
                  : t("暂无可撤销记录")}
              </span>
            </div>
            {undoInfo?.available ? (
              <>
                <p className="hint">
                  {t("批次时间 {time} · 删除方式 {mode}（只保留最近一次批次）。", {
                    time: undoInfo.finishedAt ? formatTime(undoInfo.finishedAt) : t("未知"),
                    mode: undoInfo.deleteMode === "permanent" ? t("永久删除") : t("移入回收站"),
                  })}
                  {undoInfo.truncated ? ` ${t("明细只展示前 200 条。")}` : ""}
                </p>
                <div className="skiplist">
                  {undoInfo.items.map((it) => (
                    <div className="skiprow" key={it.path}>
                      <span className="kind">{formatBytes(it.size)}</span>
                      <div>
                        <p className="skippath">{it.path}</p>
                        <p className="hint">
                          {t("来源目录 {dir}", { dir: it.dir })}
                          {it.modTime ? ` · ${t("文件修改时间 {time}", { time: formatTime(it.modTime) })}` : ""}
                        </p>
                      </div>
                    </div>
                  ))}
                </div>
                {undoInfo.permanent > 0 && (
                  <p className="hint">
                    {t("另有 {count} 个文件为永久删除：{reason}。", {
                      count: undoInfo.permanent,
                      reason: undoInfo.permanentReason || t("永久删除未进入回收站，无法撤销"),
                    })}
                  </p>
                )}
                <div className="actions">
                  <button className="secondary" disabled={undoBusy} onClick={() => setUndoConfirm(true)}>
                    {undoBusy ? t("正在撤销…") : t("撤销上次删除（恢复到原路径）")}
                  </button>
                </div>
                <p className="hint">
                  {t(
                    "撤销会逐条检查：目标位置已有同名文件时不覆盖；原目录不存在时先重建目录。永久删除与已清空的回收站都无法恢复，界面会如实标注失败原因。",
                  )}
                </p>
              </>
            ) : (
              <p className="hint">
                {t("真实执行过一次“移入回收站”的删除后，这里会出现最近一次的撤销入口；记录跨重启仍可读取。")}
              </p>
            )}
            {undoNote && <div className="banner info">{undoNote}</div>}
            {undoResult && (
              <p className="hint">
                {t("上次撤销：恢复 {restored} · 失败 {failed} · 不可撤销 {unrestorable}（共 {total} 条）", {
                  restored: undoResult.restored,
                  failed: undoResult.failed,
                  unrestorable: undoResult.unrestorable,
                  total: undoResult.total,
                })}
              </p>
            )}
            {undoResult && undoResult.errors.length > 0 && (
              <div className="skiplist">
                {undoResult.errors.map((line, i) => (
                  <div className="skiprow" key={i}>
                    <span className="kind kind-warn">{t("原因")}</span>
                    <p className="skippath">{line}</p>
                  </div>
                ))}
              </div>
            )}

            <p className="hint">
              {t("执行日志已写入用户配置目录下的 {path}。删除后的文件不在结果列表中刷新，建议重新扫描确认。", {
                path: "文件整理.一杯好茶/logs/ops",
              })}
            </p>
            <div className="actions">
              <button className="secondary" onClick={reset}>
                {t("重新扫描")}
              </button>
            </div>
          </section>
        )}
          </>
        )}
      </main>

      {detail && (
        <aside className="drawer" aria-label={t("路径详情")}>
          <div className="drawerhead">
            <h3>{t("完整路径")}</h3>
            <button className="link" onClick={() => setDetail("")}>
              {t("关闭")}
            </button>
          </div>
          <p className="fullpath">{detail}</p>
          {isImagePath(detail) && (
            <div className="previewbox">
              <div className="actions">
                <button className="secondary" disabled={imagePreviewBusy} onClick={() => void loadImagePreview()}>
                  {imagePreviewBusy
                    ? t("正在解码…")
                    : imagePreview
                      ? t("重新加载预览")
                      : t("加载大图预览（≤1600px）")}
                </button>
              </div>
              {imagePreviewError && <p className="hint warn">{imagePreviewError}</p>}
              {imagePreview && (
                <>
                  <img className="bigpreview" src={imagePreview.dataUrl} alt={t("预览：{path}", { path: detail })} />
                  <p className="hint">
                    {t("原图 {width} × {height} · 解码格式 {format}（预览已按 EXIF 方向校正）", {
                      width: imagePreview.width,
                      height: imagePreview.height,
                      format: imagePreview.format,
                    })}
                  </p>
                </>
              )}
            </div>
          )}
        </aside>
      )}

      {skipOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("扫描期间跳过（前 200 项）")}</h3>
            {skips.length === 0 ? (
              <p className="hint">{t("没有跳过项。")}</p>
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
                {t("关闭")}
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmOpen && preview && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("确认执行")}</h3>
            <p>
              {t("即将对 {count} 个文件执行{mode}，预计释放 {size}。", {
                count: preview.total,
                mode: deleteMode === "trash" ? t("移入回收站") : t("永久删除"),
                size: formatBytes(preview.reclaimableBytes),
              })}
            </p>
            <p className="hint">
              {deleteMode === "trash"
                ? t("移入系统回收站；执行完成后可在本页“撤销上次删除”中恢复到原路径。")
                : t("永久删除不可恢复，也不会进入回收站。")}
            </p>
            {deleteMode === "permanent" && (
              <label className="checkline">
                <input type="checkbox" checked={permanentAck} onChange={(e) => setPermanentAck(e.target.checked)} />
                <span>{t("我明白永久删除不可恢复，并已备份重要数据")}</span>
              </label>
            )}
            {licenseStop && (
              <p className="hint">
                {t("未授权：确认执行已禁用。请先关闭此窗口，在“授权”中导入许可证后重试。")}
              </p>
            )}
            <div className="actions">
              <button className="secondary" onClick={() => setConfirmOpen(false)}>
                {t("取消")}
              </button>
              <button
                className="danger"
                disabled={(deleteMode === "permanent" && !permanentAck) || licenseStop}
                onClick={startExecute}
              >
                {t("确认执行")}
              </button>
            </div>
          </div>
        </div>
      )}

      {undoConfirm && undoInfo?.available && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("确认撤销上次删除")}</h3>
            <p>
              {t("将把 {count} 个文件从回收站恢复到原路径。", { count: undoInfo.trash })}
            </p>
            <p className="hint">
              {t("目标位置已有同名文件时会跳过并给出原因，不会覆盖你后来新建的文件；原目录不存在时会先重建目录。此操作无法撤销。")}
            </p>
            <div className="actions">
              <button className="secondary" onClick={() => setUndoConfirm(false)}>
                {t("取消")}
              </button>
              <button className="primary" disabled={undoBusy} onClick={doDedupUndo}>
                {t("确认撤销")}
              </button>
            </div>
          </div>
        </div>
      )}

      {helpOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("帮助与快捷键")}</h3>
            <dl className="licenseinfo">
              <dt>F1</dt>
              <dd>{t("打开本帮助（任何界面都可用）")}</dd>
              <dt>Esc</dt>
              <dd>{t("收起最上层浮层：帮助 → 更新 → 授权 → 设置 → 跳过列表 → 撤销确认 → 删除确认")}</dd>
              <dt>{t("拖放")}</dt>
              <dd>
                {t("在「选择目录」步骤把文件夹拖到路径框区域，等同于点击「选择目录」；拖入单个文件会改用其所在目录。")}
              </dd>
            </dl>
            <p className="hint">
              {t(
                "查重流程：选择目录与设置 → 扫描 → 预览分组 → 勾选要删除的文件 → 确认执行（移入回收站可撤销，永久删除不可恢复）。所有删除范围以勾选为准，未勾选的文件一律保留。",
              )}
            </p>
            <p className="hint">
              {t("九步整理、网盘上传、本地搜索各有独立页签；具体参数与限制见对应页面的说明文字。")}
            </p>
            <div className="actions">
              <button className="primary" onClick={() => setHelpOpen(false)}>
                {t("关闭")}
              </button>
            </div>
          </div>
        </div>
      )}

      {diagOpen && <DiagnosticsDialog onClose={() => setDiagOpen(false)} />}

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
            <h3>{t("授权")}</h3>
            <dl className="licenseinfo">
              <dt>{t("状态")}</dt>
              <dd>{licenseLabel(license)}</dd>
              <dt>{t("来源")}</dt>
              <dd>{licenseSourceLabel(license?.source)}</dd>
              <dt>{t("版本")}</dt>
              <dd>{license?.edition || (license?.status === "unlicensed" ? t("未授权") : "—")}</dd>
              <dt>{t("到期")}</dt>
              <dd>
                {license?.licensed
                  ? license.forever
                    ? t("永久")
                    : license.expiry || "—"
                  : license?.trialActive
                    ? t("{start} 至 {end}", { start: license.trialStart ?? "", end: license.trialEnd ?? "" })
                    : "—"}
              </dd>
              <dt>{t("机器码")}</dt>
              <dd>
                <code>{license?.machineCode || "—"}</code>
              </dd>
              <dt>{t("许可证文件")}</dt>
              <dd className="break">{license?.licensePath || "—"}</dd>
            </dl>

            <p className="hint">{license?.limits}</p>
            {license?.featureList?.length ? (
              <div className="featurelist" aria-label={t("功能解锁状态")}>
                {license.featureList.map((f) => (
                  <div
                    key={f.key}
                    className={`featureitem ${f.unlocked ? "on" : "off"}`}
                    title={
                      f.unlocked
                        ? t("已解锁")
                        : t("{edition}解锁 · {reason}", { edition: f.edition, reason: f.reason ?? "" })
                    }
                  >
                    <span className="mark">{f.unlocked ? t("已解锁") : t("未解锁")}</span>
                    <span className="name">{f.name}</span>
                    <span className="meta">
                      {f.unlocked ? f.edition : t("{edition} · {reason}", { edition: f.edition, reason: f.reason ?? "" })}
                    </span>
                  </div>
                ))}
              </div>
            ) : null}
            {license?.warning && <p className="hint warn">{license.warning}</p>}
            {license?.trialActive && (
              <p className="hint">
                {t("试用期从首次启动算起共 3 天，剩余 {days} 天；到期后删除执行与整理执行会被禁用。", {
                  days: license.trialDaysLeft,
                })}
              </p>
            )}

            <div className="actions">
              <button className="secondary" onClick={() => void copyMachineCode()}>
                {t("复制机器码")}
              </button>
              <button onClick={() => void importLicenseFile()}>{t("导入许可证文件…")}</button>
              <button className="secondary" onClick={() => setLicenseOpen(false)}>
                {t("关闭")}
              </button>
            </div>

            <label className="field">
              <span>{t("或粘贴许可证文本")}</span>
              <textarea
                rows={3}
                value={licenseText}
                placeholder={t("FDLIC1:… 或旧版 Base32 许可证")}
                onChange={(e) => setLicenseText(e.target.value)}
              />
            </label>
            <div className="actions">
              <button className="secondary" disabled={!licenseText.trim()} onClick={() => void importLicensePasted()}>
                {t("导入粘贴内容")}
              </button>
            </div>
            {licenseNote && <p className="hint">{licenseNote}</p>}
            <p className="hint">
              {t("未授权时仍可扫描与预览，但删除执行和整理执行会被禁用，并在界面上明确提示，不会静默降级。")}
            </p>
          </div>
        </div>
      )}

      {updateOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("软件更新")}</h3>
            <dl className="licenseinfo">
              <dt>{t("当前版本")}</dt>
              <dd>v{info.version || "—"}</dd>
              <dt>{t("更新源")}</dt>
              <dd className="break">
                {updateForm.manifestUrl ||
                  t("内置默认（GitHub {repo} 最新发布）", { repo: "asybhc/filededup" })}
              </dd>
              <dt>{t("检查模式")}</dt>
              <dd>
                {updateForm.checkMode === "auto"
                  ? t("自动，每 {hours} 小时", { hours: updateForm.intervalHours })
                  : t("手动")}
              </dd>
              <dt>{t("跳过版本")}</dt>
              <dd>{updateForm.skippedVersion || "—"}</dd>
              {updateCheck && (
                <>
                  <dt>{t("检查结果")}</dt>
                  <dd>{t(UPDATE_STATUS_LABELS[updateCheck.status] ?? updateCheck.status)}</dd>
                </>
              )}
            </dl>

            <label className="field">
              <span>{t("更新清单地址（留空使用内置默认源）")}</span>
              <input
                value={updateForm.manifestUrl}
                placeholder={t("留空即用内置默认源，或填 https://…/update.json")}
                onChange={(e) => setUpdateForm((prev) => ({ ...prev, manifestUrl: e.target.value }))}
              />
            </label>
            <label className="field">
              <span>{t("检查模式")}</span>
              <select
                value={updateForm.checkMode}
                onChange={(e) => setUpdateForm((prev) => ({ ...prev, checkMode: e.target.value as "auto" | "manual" }))}
              >
                <option value="auto">{t("自动检查")}</option>
                <option value="manual">{t("仅手动检查")}</option>
              </select>
            </label>
            <label className="field">
              <span>{t("检查间隔（小时，1–720）")}</span>
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
                {t("保存并立即检查")}
              </button>
              {updateForm.skippedVersion && (
                <button className="secondary" disabled={updateBusy} onClick={() => void clearSkipped()}>
                  {t("取消跳过 {version}", { version: updateForm.skippedVersion })}
                </button>
              )}
              <button className="secondary" onClick={() => setUpdateOpen(false)}>
                {t("关闭")}
              </button>
            </div>

            {updateCheck?.status === "available" && (
              <>
                <div className="banner warn" role="status">
                  <span>
                    {t("发现新版本 v{version}", { version: updateCheck.latestVersion ?? "" })}
                    {updateCheck.assetSize ? t("（{size}）", { size: formatBytes(updateCheck.assetSize) }) : ""}
                    {updateCheck.releasedAt ? ` · ${updateCheck.releasedAt}` : ""}
                  </span>
                </div>
                {updateCheck.notes && <p className="hint">{updateCheck.notes}</p>}
                <div className="actions">
                  <button className="secondary" disabled={updateBusy} onClick={() => void skipLatest()}>
                    {t("跳过此版本")}
                  </button>
                  <button className="danger" disabled={updateBusy} onClick={() => void installLatest()}>
                    {installArmed ? t("确认覆盖当前程序并重启") : t("下载并安装")}
                  </button>
                </div>
                {installArmed && (
                  <p className="hint warn">
                    {t("将下载安装包并覆盖当前程序；替换前会备份为 .old，长度、SHA-256、清单签名或包签名任一不符都会拒绝安装并回滚。")}
                  </p>
                )}
              </>
            )}

            {updateNote && <p className="hint">{updateNote}</p>}
            <p className="hint">
              {t("清单与安装包都必须通过内置的更新专用 Ed25519 公钥验签，并校验长度与 SHA-256；失败一律拒绝安装，没有“忽略签名继续”的选项。")}
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
  const t = useT();
  if (!scan) {
    return (
      <>
        <p className="hint">{t("正在启动扫描…")}</p>
        <div className="actions">{children}</div>
      </>
    );
  }
  const hashRatio = ratio(scan.hashDone, scan.hashTargets);
  const phaseLabel =
    scan.phase === "cancelling"
      ? t("正在取消…")
      : scan.phase === "cancelled"
        ? t("已取消（结果不完整）")
        : scan.enumerationDone
          ? t("正在计算哈希")
          : t("正在枚举文件");
  return (
    <>
      <div className="chips">
        <span className="chip">
          {t("阶段")} <b>{phaseLabel}</b>
        </span>
        <span className="chip">
          {t("已发现文件")} <b>{scan.files.toLocaleString("zh-CN")}</b>
        </span>
        <span className="chip">
          {t("已读取")} <b>{formatBytes(scan.bytes)}</b>
        </span>
        <span className="chip">
          {t("用时")} <b>{(scan.elapsedMs / 1000).toFixed(1)} s</b>
        </span>
      </div>
      <div className={`progress ${scan.enumerationDone ? "" : "indeterminate"}`}>
        <div className="bar" style={{ width: `${Math.round(hashRatio * 100)}%` }} />
      </div>
      <p className="hint">
        {scan.enumerationDone
          ? t("哈希进度 {a} / {b}（{x} / {y}）", {
              a: scan.hashDone.toLocaleString("zh-CN"),
              b: scan.hashTargets.toLocaleString("zh-CN"),
              x: formatBytes(scan.hashBytes),
              y: formatBytes(scan.hashTotalBytes),
            })
          : t("枚举阶段的总量未知，进度条为不确定状态；取消会保留已扫描的部分结果。")}
      </p>
      <div className="actions">
        <button className="secondary" disabled={scan.phase !== "running"} onClick={onCancel}>
          {t("取消扫描")}
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
  onReveal,
}: {
  row: Row;
  onSelect: (path: string) => void;
  selectable?: boolean;
  selected?: Set<string>;
  onToggle?: (group: GroupView, path: string) => void;
  onGroupAction?: (group: GroupView, action: "all" | "none" | "invert" | "keep") => void;
  onReveal?: (path: string) => void;
}) {
  const t = useT();
  if (row.type === "group") {
    const g = row.group;
    let checked = 0;
    if (selected) {
      for (const m of g.members) if (selected.has(m.path)) checked++;
    }
    return (
      <div className="row grouprow" onClick={() => onSelect(g.keep)}>
        <span className="gsel">
          <span className={`kind kind-${g.kind}`}>
            {KIND_LABELS[g.kind] ? t(KIND_LABELS[g.kind]) : g.kind}
          </span>
          {selectable && (
            <span className="selcount">{t("删 {checked}/{total}", { checked, total: g.members.length })}</span>
          )}
        </span>
        <span className="gtitle">{t("重复组 · {count} 个文件", { count: g.members.length })}</span>
        <span className="gkeep">{t("策略保留：{name}", { name: g.keep.slice(g.keep.lastIndexOf("\\") + 1) })}</span>
        {selectable ? (
          <span className="gbuttons" onClick={(e) => e.stopPropagation()}>
            <button className="link" onClick={() => onGroupAction?.(g, "all")}>{t("全选")}</button>
            <button className="link" onClick={() => onGroupAction?.(g, "none")}>{t("全不选")}</button>
            <button className="link" onClick={() => onGroupAction?.(g, "invert")}>{t("反选")}</button>
            <button
              className="link"
              onClick={() => onGroupAction?.(g, "keep")}
              title={t("把当前勾选项视为要保留的文件，其余成员改为待删除")}
            >
              {t("仅保留所选")}
            </button>
          </span>
        ) : (
          <span className="num">{t("可回收 {size}", { size: formatBytes(g.reclaimable) })}</span>
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
            aria-label={
              checked
                ? t("取消删除 {name}", { name: m.name })
                : t("标记删除 {name}", { name: m.name })
            }
          />
        )}
        <span className={`kind ${checked ? "kind-del" : "kind-keep"}`}>
          {checked ? t("将删除") : t("保留")}
        </span>
      </span>
      <span className="mname" title={m.name}>
        {m.name}
      </span>
      <span className="mpath" title={m.path}>
        {m.path}
      </span>
      <span className="num">{formatBytes(m.size)}</span>
      <span className="mtime">{formatTime(m.modTime)}</span>
      <span className="mreveal" onClick={(e) => e.stopPropagation()}>
        {onReveal && (
          <button className="link" onClick={() => onReveal(m.path)}>
            {t("打开所在位置")}
          </button>
        )}
      </span>
    </div>
  );
});

function FeatureStatus() {
  const t = useT();
  return (
    <div className="features">
      <h3>{t("功能状态（如实标注）")}</h3>
      <p>
        <span className="kind kind-keep">{t("已接通")}</span>{" "}
        {t(
          "目录扫描、内容/图片/音频查重、结果分页与虚拟滚动、回收站与永久删除、执行前确认、执行统计、执行日志、设置持久化、查重结果导出（HTML/CSV）、九步整理（试运行/真实执行/存疑项确认/最近一次撤销/规则导入导出）、自动更新（Ed25519 清单与安装包验签、备份与回滚）、Ed25519 许可证导入与状态显示、旧版 ECDSA 许可证只读兼容、试用期（PBKDF2 + 双存储 + 回拨检测）",
        )}
      </p>
      <p>
        <span className="kind kind-warn">{t("未联调")}</span>{" "}
        {t("蓝奏云上传页（登录/目录浏览/预检/上传门面已接通，真实列表与上传需要蓝奏账号验证）")}
      </p>
      <p>
        <span className="kind kind-warn">{t("未实现")}</span>{" "}
        {t("系统托盘、自定义保留项、查重侧执行撤销、撤销历史")}
      </p>
    </div>
  );
}
