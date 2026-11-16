// 九步整理页面：接 cmd/filededup/organize.go。
// 步骤顺序与标题全部来自后端 GetOrganizeMeta（引擎跑空快照得到），前端不复制顺序表。
import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";
import type {
  Counter,
  KeepRule,
  OrganizeMeta,
  OrganizePreset,
  OrganizeRequest,
  OrganizeSettings,
  OrganizeState,
  OrganizeStepMeta,
  OrganizeTestResult,
  OrganizeTestRow,
  OrganizeUndoInfo,
  OrganizeUndoResult,
} from "./types";

const MB = 1024 * 1024;

const ORG_KEEP_LABELS: Record<KeepRule, string> = {
  pathShortest: "保留路径最短",
  newest: "保留最新修改",
  oldest: "保留最早修改",
  largest: "保留最大文件",
  nameShortest: "保留最短文件名",
};

const MODE_LABELS: Record<string, string> = {
  category: "按文件类型分类",
  extension: "按后缀名整理（扩展名大写）",
  namefeature: "按文件名字符特征（正则）",
  keyword: "按文件名关键字",
  fixedfolder: "按指定文件夹",
};

const CONFLICT_LABELS: Record<string, string> = {
  rename: "自动重命名 name (1).ext",
  skip: "跳过并计数",
  overwrite: "保留已存在目标，删除源文件",
};

const KIND_LABELS: Record<string, string> = {
  rename: "改名",
  move: "移动",
  delete: "删除",
  hardlink: "硬链接",
  clear_readonly: "去只读",
  remove_dir: "删空目录",
  ensure_dir: "建目录",
  suggest: "建议（仅报告）",
};

const STEP_FIELD: Record<string, keyof OrganizeSettings["steps"]> = {
  rename_prefix: "renamePrefix",
  rename_suffix: "renameSuffix",
  special_delete: "specialDelete",
  readonly: "readonly",
  content_dedup: "contentDedup",
  image_dedup: "imageDedup",
  audio_dedup: "audioDedup",
  organize: "organize",
  empty_dirs: "emptyDirs",
};

const SPECIAL_ACTION_LABELS: Record<OrganizeTestRow["specialAction"], string> = {
  disabled: "未启用",
  garbage: "垃圾文件（将被删除）",
  suspect: "存疑（默认保留，需人工确认）",
  keep: "保留",
  excluded: "命中保护词（排除）",
};

const ORGANIZE_SKIP_LABELS: Record<NonNullable<OrganizeTestRow["organizeSkip"]>, string> = {
  deleted: "已在特殊删除步骤删除",
  disabled: "整理步骤未启用",
  "no-target": "未选择整理目标",
  "keyword-empty": "未填写关键字",
  "keyword-miss": "未命中任一关键字",
  "regex-empty": "未填写正则",
  "regex-invalid": "正则无效",
  "regex-miss": "未命中正则",
};

const COUNTER_LABELS: [keyof Counter, string][] = [
  ["renamed", "改名"],
  ["specialDeleted", "特殊删除"],
  ["moved", "移动"],
  ["conflictSkip", "冲突跳过"],
  ["readonly", "去只读"],
  ["organizeSkip", "整理跳过"],
  ["contentDeleted", "内容重复删除"],
  ["imageDeleted", "相似图片删除"],
  ["audioDeleted", "音频删除"],
  ["emptyDirs", "空目录清理"],
  ["excluded", "排除"],
  ["hiddenSkipped", "隐藏/系统跳过"],
  ["suspectsKept", "存疑保留"],
  ["hardlinkMerged", "硬链接合并"],
];

function defaultOrganizeSettings(): OrganizeSettings {
  return {
    root: "",
    target: "",
    usePreviewPlan: false,
    steps: {
      renamePrefix: true,
      renameSuffix: true,
      specialDelete: false,
      readonly: true,
      contentDedup: false,
      imageDedup: false,
      audioDedup: false,
      organize: true,
      emptyDirs: false,
    },
    mode: "category",
    keyword: "",
    regex: "",
    fixedDir: "",
    conflict: "rename",
    deleteMode: "trash",
    keepPolicy: "pathShortest",
    imageKeepPolicy: "largest",
    imageThreshold: 8,
    audioTolerance: 2,
    strength: 1,
    minSize: 0,
    includeHidden: false,
    excludes: [],
    protectWords: "",
    hardlink: false,
    continueOnError: false,
  };
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function counterText(t: (text: string, params?: Record<string, string | number>) => string, c: Counter): string {
  const parts = COUNTER_LABELS.filter(([k]) => c[k] > 0).map(([k, label]) => `${t(label)} ${c[k]}`);
  return parts.length ? parts.join(" · ") : t("无计数变化");
}

function orderLabel(order: string): string {
  return order === "preview" ? "预览顺序（先去重后整理）" : "独立顺序（先改名后去重）";
}

export function Organizer({
  initial,
  onPersist,
  canRun = true,
  licenseHint,
}: {
  initial?: OrganizeSettings;
  onPersist: (s: OrganizeSettings) => void;
  canRun?: boolean;
  licenseHint?: string;
}) {
  const t = useT();
  const [cfg, setCfg] = useState<OrganizeSettings>(() => initial ?? defaultOrganizeSettings());
  const [excludesText, setExcludesText] = useState(() => (initial?.excludes ?? []).join(" "));
  const [meta, setMeta] = useState<OrganizeMeta>();
  const [metaError, setMetaError] = useState("");
  const [error, setError] = useState("");
  const [state, setState] = useState<OrganizeState>();
  const [jobId, setJobId] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [permanentAck, setPermanentAck] = useState(false);
  const [planOnlyRun, setPlanOnlyRun] = useState(false);
  // 存疑项决策：未出现在这里的项一律按“保留/驳回”处理。
  const [decisions, setDecisions] = useState<Record<string, "delete" | "keep">>({});
  const [undoInfo, setUndoInfo] = useState<OrganizeUndoInfo>();
  const [undoResult, setUndoResult] = useState<OrganizeUndoResult>();
  const [undoOpen, setUndoOpen] = useState(false);
  const [undoBusy, setUndoBusy] = useState(false);
  const [rulesBusy, setRulesBusy] = useState(false);
  const [notice, setNotice] = useState("");
  // 配置方案预设：命名保存在用户配置目录（浏览器预览落 localStorage）。
  const [presets, setPresets] = useState<OrganizePreset[]>([]);
  const [presetsPath, setPresetsPath] = useState("");
  const [presetsError, setPresetsError] = useState("");
  const [presetName, setPresetName] = useState("");
  const [presetBusy, setPresetBusy] = useState(false);
  // 规则测试器：纯计算，不落盘、不建任务。
  const [samplesText, setSamplesText] = useState("");
  const [testResult, setTestResult] = useState<OrganizeTestResult>();
  const [testerError, setTesterError] = useState("");
  const [testerBusy, setTesterBusy] = useState(false);

  // onPersist 每次渲染都是新函数，用 ref 固定，避免防抖 effect 反复重置。
  const persistRef = useRef(onPersist);
  persistRef.current = onPersist;

  useEffect(() => {
    let alive = true;
    api
      .getOrganizeMeta()
      .then((m) => {
        if (alive) setMeta(m);
      })
      .catch((err) => {
        if (alive) setMetaError(t("步骤顺序加载失败：{error}", { error: errorText(err) }));
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    let alive = true;
    api
      .getOrganizeUndoInfo()
      .then((info) => {
        if (alive) setUndoInfo(info);
      })
      .catch(() => {
        // 撤销信息读取失败不影响整理主流程，界面按“暂无可撤销”展示。
      });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    let alive = true;
    api
      .listOrganizePresets()
      .then((res) => {
        if (!alive) return;
        setPresets(res.presets);
        setPresetsPath(res.path);
      })
      .catch((err) => {
        if (alive) setPresetsError(t("读取预设失败：{error}", { error: errorText(err) }));
      });
    return () => {
      alive = false;
    };
  }, []);

  // 配置改动后 500ms 落盘，输入过程中不反复写文件。
  useEffect(() => {
    const timer = window.setTimeout(() => persistRef.current(cfg), 500);
    return () => window.clearTimeout(timer);
  }, [cfg]);

  useEffect(() => {
    if (!jobId) return;
    let stopped = false;
    const tick = async () => {
      try {
        const st = await api.getOrganizeState(jobId);
        if (stopped) return;
        setState(st);
        if (st.phase !== "running") {
          stopped = true;
          window.clearInterval(timer);
          if (!st.dryRun) {
            // 真实执行后刷新“撤销上次整理”的可用性。
            api
              .getOrganizeUndoInfo()
              .then(setUndoInfo)
              .catch(() => {});
          }
        }
      } catch (err) {
        if (stopped) return;
        stopped = true;
        window.clearInterval(timer);
        setError(t("读取整理进度失败：{error}", { error: errorText(err) }));
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 400);
    return () => {
      stopped = true;
      window.clearInterval(timer);
    };
  }, [jobId]);

  const standalone = meta?.standalone ?? [];
  const preview = meta?.preview ?? [];
  const activeOrder: OrganizeStepMeta[] = cfg.usePreviewPlan ? preview : standalone;
  const running = state?.phase === "running";
  const enabledCount = Object.values(cfg.steps).filter(Boolean).length;

  function patch(next: Partial<OrganizeSettings>) {
    setCfg((prev) => ({ ...prev, ...next }));
  }

  function toggleStep(id: string, on: boolean) {
    const field = STEP_FIELD[id];
    if (!field) return;
    setCfg((prev) => ({ ...prev, steps: { ...prev.steps, [field]: on } }));
  }

  async function pickDirectory() {
    try {
      const dir = await api.pickDirectory();
      if (dir) patch({ root: dir });
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function start(dryRun: boolean, opts?: { planOnly?: boolean; confirmed?: string[] }) {
    setError("");
    setNotice("");
    try {
      const confirmed = opts?.confirmed ?? (dryRun ? [] : confirmedKeys());
      const req: OrganizeRequest = {
        ...cfg,
        dryRun,
        planOnly: opts?.planOnly ?? false,
        confirmedSuspects: confirmed,
      };
      persistRef.current(cfg);
      const id = await api.startOrganize(req);
      setPlanOnlyRun(Boolean(opts?.planOnly));
      setState(undefined);
      setJobId(id);
    } catch (err) {
      setError(errorText(err));
    }
  }

  function decide(key: string, value: "delete" | "keep") {
    setDecisions((prev) => ({ ...prev, [key]: value }));
  }

  // 只把当前结果里仍存在、且被明确确认的存疑项交给后端。
  function confirmedKeys(): string[] {
    return (state?.suspects ?? [])
      .filter((s) => decisions[s.key] === "delete")
      .map((s) => s.path);
  }

  function decideAll(value: "delete" | "keep") {
    const next: Record<string, "delete" | "keep"> = {};
    for (const s of state?.suspects ?? []) next[s.key] = value;
    setDecisions(next);
  }

  async function openTarget() {
    const dir = state?.openDir;
    if (!dir) return;
    setError("");
    try {
      await api.openPath(dir);
    } catch (err) {
      setError(t("打开目标文件夹失败：{error}", { error: errorText(err) }));
    }
  }

  async function refreshPresets() {
    try {
      const res = await api.listOrganizePresets();
      setPresets(res.presets);
      setPresetsPath(res.path);
    } catch (err) {
      setPresetsError(t("读取预设失败：{error}", { error: errorText(err) }));
    }
  }

  async function savePreset() {
    const clean = presetName.trim();
    if (!clean) {
      setPresetsError(t("请输入预设名称"));
      return;
    }
    setError("");
    setNotice("");
    setPresetsError("");
    setPresetBusy(true);
    try {
      const entry = await api.saveOrganizePreset(clean, cfg);
      setPresetName("");
      setNotice(t("预设已保存：{name}", { name: entry.name }));
      await refreshPresets();
    } catch (err) {
      setPresetsError(t("保存预设失败：{error}", { error: errorText(err) }));
    } finally {
      setPresetBusy(false);
    }
  }

  function applyPreset(p: OrganizePreset) {
    // 预设只存规则，不绑定机器：应用时保留当前根目录。
    const next: OrganizeSettings = { ...p.rules, root: cfg.root };
    setCfg(next);
    setExcludesText((next.excludes ?? []).join(" "));
    setDecisions({});
    setError("");
    setNotice(t("已应用预设：{name}（当前根目录保持不变）", { name: p.name }));
  }

  async function deletePreset(p: OrganizePreset) {
    setError("");
    setNotice("");
    setPresetsError("");
    setPresetBusy(true);
    try {
      await api.deleteOrganizePreset(p.id);
      setNotice(t("预设已删除：{name}", { name: p.name }));
      await refreshPresets();
    } catch (err) {
      setPresetsError(t("删除预设失败：{error}", { error: errorText(err) }));
    } finally {
      setPresetBusy(false);
    }
  }

  async function runTester() {
    setTesterError("");
    setTestResult(undefined);
    const samples = samplesText
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean);
    if (samples.length === 0) {
      setTesterError(t("请输入至少一个样例文件名"));
      return;
    }
    setTesterBusy(true);
    try {
      const res = await api.testOrganizeRules({ rules: cfg, samples });
      setTestResult(res);
    } catch (err) {
      setTesterError(t("测试失败：{error}", { error: errorText(err) }));
    } finally {
      setTesterBusy(false);
    }
  }


  async function exportRules() {
    setError("");
    setNotice("");
    setRulesBusy(true);
    try {
      const res = await api.exportOrganizeRules(cfg);
      if (res.canceled) return;
      setNotice(t("规则已导出：{path}（{bytes} 字节）", { path: res.path, bytes: res.bytes }));
    } catch (err) {
      setError(t("导出规则失败：{error}", { error: errorText(err) }));
    } finally {
      setRulesBusy(false);
    }
  }

  async function importRules() {
    setError("");
    setNotice("");
    setRulesBusy(true);
    try {
      const res = await api.importOrganizeRules();
      if (res.canceled) return;
      // 根目录是机器相关状态，导入规则不改变当前根目录。
      const next: OrganizeSettings = { ...res.rules, root: cfg.root };
      setCfg(next);
      setExcludesText((next.excludes ?? []).join(" "));
      setDecisions({});
      setNotice(t("规则已导入：{path}（当前根目录保持不变）", { path: res.path }));
    } catch (err) {
      setError(t("导入规则失败：{error}", { error: errorText(err) }));
    } finally {
      setRulesBusy(false);
    }
  }

  async function doUndo() {
    setUndoOpen(false);
    setError("");
    setNotice("");
    setUndoBusy(true);
    try {
      const res = await api.undoLastOrganize();
      setUndoResult(res);
      const info = await api.getOrganizeUndoInfo();
      setUndoInfo(info);
      setState(undefined);
      setJobId("");
    } catch (err) {
      setError(t("撤销失败：{error}", { error: errorText(err) }));
      api
        .getOrganizeUndoInfo()
        .then(setUndoInfo)
        .catch(() => {});
    } finally {
      setUndoBusy(false);
    }
  }

  return (
    <div className="orgpage">
      <section className="card">
        <div className="sectionhead">
          <h2>{t("九步整理")}</h2>
          {state && (
            <span className={`badge ${state.dryRun ? "warn" : "danger"}`}>
              {planOnlyRun ? t("仅规划结果") : state.dryRun ? t("试运行结果") : t("真实执行结果")}
            </span>
          )}
        </div>

        <div className="dirrow">
          <input
            className="path"
            readOnly
            value={cfg.root}
            placeholder={t("尚未选择要整理的目录")}
            aria-label={t("整理根目录")}
          />
          <button className="secondary" onClick={pickDirectory} disabled={running}>
            {t("选择目录")}
          </button>
        </div>

        <div className="formgrid">
          <label>
            <span>{t("整理目标目录（留空 = 根目录；可填绝对路径放在源目录之外）")}</span>
            <input
              value={cfg.target}
              onChange={(e) => patch({ target: e.target.value })}
              placeholder={t("例如 整理结果（根内）或 D:\\整理输出（根外）")}
            />
            <span className="hint">{t("目标可在源目录之外；同卷按重命名移动，跨卷改为复制并保留源文件。")}</span>
          </label>
          <label>
            <span>{t("整理方式（第五步）")}</span>
            <select value={cfg.mode} onChange={(e) => patch({ mode: e.target.value })}>
              {Object.entries(MODE_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {t(l)}
                </option>
              ))}
            </select>
          </label>
          {cfg.mode === "keyword" && (
            <label>
              <span>{t("关键字（逗号/分号/空格分隔）")}</span>
              <input value={cfg.keyword} onChange={(e) => patch({ keyword: e.target.value })} />
            </label>
          )}
          {cfg.mode === "namefeature" && (
            <label>
              <span>{t("正则表达式")}</span>
              <input value={cfg.regex} onChange={(e) => patch({ regex: e.target.value })} placeholder={t("例如 ^\\d{4}-")} />
            </label>
          )}
          {cfg.mode === "fixedfolder" && (
            <label>
              <span>{t("指定文件夹名")}</span>
              <input value={cfg.fixedDir} onChange={(e) => patch({ fixedDir: e.target.value })} />
            </label>
          )}
          <label>
            <span>{t("冲突策略")}</span>
            <select value={cfg.conflict} onChange={(e) => patch({ conflict: e.target.value })}>
              {Object.entries(CONFLICT_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {t(l)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("删除方式（特殊删除与去重删除）")}</span>
            <select
              value={cfg.deleteMode}
              onChange={(e) => patch({ deleteMode: e.target.value as OrganizeSettings["deleteMode"] })}
            >
              <option value="trash">{t("移入回收站（可恢复）")}</option>
              <option value="permanent">{t("永久删除（不可恢复）")}</option>
            </select>
          </label>
          <label>
            <span>{t("内容去重保留规则")}</span>
            <select value={cfg.keepPolicy} onChange={(e) => patch({ keepPolicy: e.target.value as KeepRule })}>
              {Object.entries(ORG_KEEP_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {t(l)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>{t("相似图片保留规则")}</span>
            <select value={cfg.imageKeepPolicy} onChange={(e) => patch({ imageKeepPolicy: e.target.value as KeepRule })}>
              {Object.entries(ORG_KEEP_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {t(l)}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span title={t("0 = 指纹完全相同；数值越大判定越宽松，越容易把不同的图片当成相似；范围 0-16，默认 8")}>
              {t("相似图片阈值（0 = 完全相同，越大越宽松；默认 8，上限 16）")}
            </span>
            <input
              type="number"
              min={0}
              max={16}
              value={cfg.imageThreshold}
              onChange={(e) => patch({ imageThreshold: Math.max(0, Math.min(16, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>{t("音频时长容差（秒）")}</span>
            <input
              type="number"
              min={0}
              max={60}
              value={cfg.audioTolerance}
              onChange={(e) => patch({ audioTolerance: Math.max(0, Math.min(60, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>{t("垃圾识别强度（0 低 / 1 中 / 2 高）")}</span>
            <input
              type="number"
              min={0}
              max={2}
              value={cfg.strength}
              onChange={(e) => patch({ strength: Math.max(0, Math.min(2, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>{t("最小文件大小（MB，0 = 不限）")}</span>
            <input
              type="number"
              min={0}
              value={Math.round(cfg.minSize / MB)}
              onChange={(e) => patch({ minSize: Math.max(0, Number(e.target.value) || 0) * MB })}
            />
          </label>
          <label>
            <span>{t("排除目录（按目录名匹配；逗号或空格分隔）")}</span>
            <input
              value={excludesText}
              onChange={(e) => {
                setExcludesText(e.target.value);
                patch({ excludes: e.target.value.split(/[,，;；\s]+/).filter(Boolean) });
              }}
              placeholder={t("例如 node_modules .git")}
            />
            <span className="hint">
              {t("只对目录生效：目录名相同即跳过该目录及其内容，不按文件名或部分子串过滤。")}
            </span>
          </label>
          <label>
            <span>{t("保护词（跳过改名与特殊删除判定）")}</span>
            <input value={cfg.protectWords} onChange={(e) => patch({ protectWords: e.target.value })} />
          </label>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.includeHidden}
              onChange={(e) => patch({ includeHidden: e.target.checked })}
            />
            <span>{t("包含隐藏/系统文件（不勾选 = 跳过隐藏与系统文件）")}</span>
          </label>
          <label className="checkline">
            <input type="checkbox" checked={cfg.hardlink} onChange={(e) => patch({ hardlink: e.target.checked })} />
            <span>{t("去重时建立硬链接（同卷，替代删除）")}</span>
          </label>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.continueOnError}
              onChange={(e) => patch({ continueOnError: e.target.checked })}
            />
            <span>{t("出错后继续执行剩余动作")}</span>
          </label>
        </div>

        <div className="actions">
          <button className="secondary" disabled={running || rulesBusy} onClick={() => void exportRules()}>
            {t("导出规则")}
          </button>
          <button className="secondary" disabled={running || rulesBusy} onClick={() => void importRules()}>
            {t("导入规则")}
          </button>
          <span className="hint">
            {t("规则文件带版本号；导入会拒绝未知字段与非法值，失败时不改动当前设置，也不会改变当前根目录。")}
          </span>
        </div>
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>{t("配置方案预设")}</h2>
          <span className="hint">{t("预设保存当前整理规则（不含根目录），可一键切换；校验口径与导出规则相同。")}</span>
        </div>
        <div className="actions">
          <input
            value={presetName}
            onChange={(e) => setPresetName(e.target.value)}
            placeholder={t("预设名称")}
            maxLength={60}
            onKeyDown={(e) => {
              if (e.key === "Enter") void savePreset();
            }}
          />
          <button className="secondary" disabled={presetBusy || !presetName.trim()} onClick={() => void savePreset()}>
            {t("保存为预设")}
          </button>
          <button className="secondary" disabled={presetBusy} onClick={() => void refreshPresets()}>
            {t("刷新预设")}
          </button>
          <span className="hint">{t("同名预设会被覆盖（不区分大小写）。")}</span>
        </div>
        {presetsError && <div className="banner error">{presetsError}</div>}
        {presets.length === 0 ? (
          <p className="hint">{t("还没有保存的预设。")}</p>
        ) : (
          <div className="presetlist">
            {presets.map((p) => (
              <div className="presetrow" key={p.id}>
                <span className="presetname" title={`${p.createdAt} → ${p.updatedAt}`}>
                  {p.name}
                </span>
                <span className="hint">
                  {t(MODE_LABELS[p.rules.mode] ?? p.rules.mode)} ·{" "}
                  {t("启用 {n} 个步骤", { n: Object.values(p.rules.steps).filter(Boolean).length })}
                </span>
                <button className="secondary" disabled={presetBusy} onClick={() => applyPreset(p)}>
                  {t("应用")}
                </button>
                <button className="secondary" disabled={presetBusy} onClick={() => void deletePreset(p)}>
                  {t("删除")}
                </button>
              </div>
            ))}
          </div>
        )}
        {presetsPath && <p className="hint">{t("预设文件：{path}", { path: presetsPath })}</p>}
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>{t("规则测试器")}</h2>
          <span className="hint">{t("对样例文件名做纯计算：不读磁盘、不改文件，只用当前页面的开关与规则。")}</span>
        </div>
        <label className="field">
          <span>{t("样例文件名（每行一个，可带路径）")}</span>
          <textarea
            className="testertextarea"
            value={samplesText}
            onChange={(e) => setSamplesText(e.target.value)}
            placeholder={t("例如：001-2024-03-08-照片.jpg")}
          />
        </label>
        <div className="actions">
          <button className="secondary" disabled={testerBusy} onClick={() => void runTester()}>
            {t("运行测试")}
          </button>
          <span className="hint">{t("测试结果只反映当前配置，不代表已执行任何改动。")}</span>
        </div>
        {testerError && <div className="banner error">{testerError}</div>}
        {testResult && (
          <>
            <p className="hint">
              {t(
                "样例 {total} 个：改名 {renamed}，垃圾 {garbage}，存疑 {suspects}，保留 {kept}，保护词排除 {excluded}，可整理 {organized}，跳过 {skipped}。",
                {
                  total: testResult.rows.length,
                  renamed: testResult.renamed,
                  garbage: testResult.garbage,
                  suspects: testResult.suspects,
                  kept: testResult.kept,
                  excluded: testResult.excluded,
                  organized: testResult.organized,
                  skipped: testResult.skipped,
                },
              )}
            </p>
            <div className="tlwrap">
              <table className="tl-table">
                <thead>
                  <tr>
                    <th>{t("原名")}</th>
                    <th>{t("改名后")}</th>
                    <th>{t("特殊文件")}</th>
                    <th>{t("整理去向")}</th>
                    <th>{t("最终路径")}</th>
                  </tr>
                </thead>
                <tbody>
                  {testResult.rows.map((row, i) => (
                    <tr key={`${row.original}-${i}`}>
                      <td>{row.original}</td>
                      <td>{row.finalName}</td>
                      <td title={row.specialReason}>
                        {t(SPECIAL_ACTION_LABELS[row.specialAction])}
                      </td>
                      <td>
                        {row.organizeDir ??
                          (row.organizeSkip ? t(ORGANIZE_SKIP_LABELS[row.organizeSkip]) : "—")}
                      </td>
                      <td>{row.destPath ?? "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>{t("执行顺序与步骤开关")}</h2>
          <span className="hint">{t("顺序来自引擎，两套顺序只差“去重三步”的位置")}</span>
        </div>

        {metaError ? (
          <div className="banner error">{metaError}</div>
        ) : (
          <>
            <div className="orders">
              <button
                type="button"
                className={`ordercard ${cfg.usePreviewPlan ? "" : "active"}`}
                onClick={() => patch({ usePreviewPlan: false })}
              >
                <h4>{t("无预览计划 · 独立顺序")}</h4>
                <ol>
                  {standalone.map((s) => (
                    <li key={s.id} title={t(s.title)}>
                      {t(s.title)}
                    </li>
                  ))}
                </ol>
              </button>
              <button
                type="button"
                className={`ordercard ${cfg.usePreviewPlan ? "active" : ""}`}
                onClick={() => patch({ usePreviewPlan: true })}
              >
                <h4>{t("有预览计划 · 先去重后整理")}</h4>
                <ol>
                  {preview.map((s) => (
                    <li key={s.id} title={t(s.title)}>
                      {t(s.title)}
                    </li>
                  ))}
                </ol>
              </button>
            </div>

            <p className="hint">
              {t("当前执行顺序：")}
              <b>{t(orderLabel(cfg.usePreviewPlan ? "preview" : "standalone"))}</b>
              {t("，已启用 {n} / 9 步。开关只影响是否执行该步，不改变顺序。", { n: enabledCount })}
            </p>

            <div className="orgsteps">
              {activeOrder.map((s, i) => {
                const field = STEP_FIELD[s.id];
                const on = field ? cfg.steps[field] : false;
                return (
                  <label className={`orgstep ${on ? "" : "off"}`} key={s.id}>
                    <span className="orgstepno">{i + 1}</span>
                    <input
                      type="checkbox"
                      checked={on}
                      disabled={running || !field}
                      onChange={(e) => toggleStep(s.id, e.target.checked)}
                    />
                    <span className="orgsteptitle" title={t(s.title)}>
                      {t(s.title)}
                    </span>
                  </label>
                );
              })}
            </div>

            <div className="actions">
              <button
                className="secondary"
                disabled={!cfg.root || enabledCount === 0 || running}
                onClick={() => void start(true, { planOnly: true, confirmed: [] })}
              >
                {t("分析存疑项（不改文件）")}
              </button>
              <button className="secondary" disabled={!cfg.root || enabledCount === 0 || running} onClick={() => void start(true)}>
                {t("试运行（不改文件）")}
              </button>
              <button
                className="danger"
                disabled={!cfg.root || enabledCount === 0 || running || !canRun}
                title={!canRun ? licenseHint : undefined}
                onClick={() => {
                  setPermanentAck(false);
                  setConfirmOpen(true);
                }}
              >
                {t("执行（会改文件）")}
              </button>
              {running && <span className="hint">{t("正在执行，按钮暂时禁用。")}</span>}
              {!running && !canRun && (
                <span className="hint">{licenseHint || t("未授权：整理执行已禁用。")}</span>
              )}
            </div>

            <p className="hint">
              {t("试运行只创建目标目录，不移动、不删除、不修改属性；真实执行会按上面的动作落盘。两者都必须先经过确认。")}
            </p>
          </>
        )}

        <OrganizeFeatureStatus />
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>{t("撤销上次整理")}</h2>
          <span className={`badge ${undoInfo?.available ? "warn" : ""}`}>
            {undoInfo?.available
              ? t("可撤销 {n} 条记录", { n: undoInfo.records })
              : t("暂无可撤销记录")}
          </span>
        </div>
        {undoInfo?.available ? (
          <p className="hint">
            {t("任务")} <b>{undoInfo.id}</b>{t("；根目录")} <b>{undoInfo.root}</b>
            {t("；开始时间 {time}。", { time: undoInfo.startedAt ?? "" })}
          </p>
        ) : (
          <p className="hint">{t("真实执行过一次整理后，这里会出现最近一次的撤销入口。")}</p>
        )}
        <p className="hint">
          {t("可回滚：改名、移动、新建目录、只读属性。不可回滚：已删除文件（回收站或永久删除）与硬链接替换；这些项会在结果里单独计数，不会假装恢复成功。")}
        </p>
        <div className="actions">
          <button
            className="secondary"
            disabled={!undoInfo?.available || running || undoBusy}
            onClick={() => setUndoOpen(true)}
          >
            {undoBusy ? t("正在撤销…") : t("撤销上次整理")}
          </button>
          {undoResult && (
            <span className="hint">
              {t("上次撤销：恢复 {restored} · 跳过 {skipped} · 不可恢复 {unrestorable} · 失败 {failed}（共 {total} 条）", {
                restored: undoResult.restored,
                skipped: undoResult.skipped,
                unrestorable: undoResult.unrestorable,
                failed: undoResult.failed,
                total: undoResult.total,
              })}
            </span>
          )}
        </div>
        {undoResult && undoResult.errors.length > 0 && (
          <div className="skiplist">
            {undoResult.errors.map((line, i) => (
              <div className="skiprow" key={i}>
                <span className="kind kind-warn">{t("提示")}</span>
                <p className="skippath">{line}</p>
              </div>
            ))}
          </div>
        )}
      </section>

      {notice && (
        <div className="banner info" role="status">
          <span>{notice}</span>
          <button className="link" onClick={() => setNotice("")}>
            {t("关闭")}
          </button>
        </div>
      )}

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            {t("关闭")}
          </button>
        </div>
      )}

      {state && (
        <section className="card">
          <div className="sectionhead">
            <h2>{t("整理结果")}</h2>
            <div className="chips inline">
              <span className="chip">
                {t("顺序")} <b>{t(orderLabel(state.order))}</b>
              </span>
              <span className={`chip ${state.dryRun ? "chip-warn" : "chip-danger"}`}>
                {state.dryRun ? t("试运行（未落盘）") : t("真实执行")}
              </span>
              <span className="chip">
                {t("耗时")} <b>{(state.elapsedMs / 1000).toFixed(1)} s</b>
              </span>
            </div>
          </div>

          {state.phase === "running" && (
            <div className="banner info">
              <span>
                {t("阶段：")}
                <b>{state.stage ? t(state.stage) : t("准备中")}</b>
                {state.dryRun ? t("（试运行）") : t("（真实执行）")}
              </span>
            </div>
          )}

          {state.error && <div className="banner error">{state.error}</div>}

          {state.targetExternal && (
            <div className="banner info">
              <span>
                {t("整理目标在源目录之外：")}
                <b>{state.targetDir}</b>。
                {state.moveMode === "copy" && t("跨卷：将复制文件，原文件仍保留在源目录（不会被移走）。")}
                {state.moveMode === "move" && t("同卷：将重命名式移动，原文件会离开源目录。")}
                {state.moveMode === "unknown" && t("无法判断是否同卷：执行时按实际卷逐条判断，跨卷复制、同卷移动。")}
              </span>
            </div>
          )}

          {planOnlyRun ? (
            <div className="banner perf">
              {t("这是仅规划结果：只做了扫描与规划，没有创建目录、没有移动、没有删除、没有修改任何文件。")}
            </div>
          ) : state.dryRun ? (
            <div className="banner perf">
              {t("这是试运行结果：只创建了目标目录，文件没有移动、没有删除、没有改属性。下面的动作数与计数是规划结果。")}
            </div>
          ) : (
            !running && (
              <div className="banner danger">
                {t("这是真实执行结果：动作已落盘。")}
                {cfg.deleteMode === "trash" ? t("删除项已移入回收站。") : t("删除项已永久删除，不可恢复。")}
              </div>
            )
          )}

          {!running && !state.dryRun && !planOnlyRun && state.phase === "done" && state.openDir && (
            <div className="actions">
              <button className="secondary" onClick={() => void openTarget()}>
                {t("打开目标文件夹")}
              </button>
              <span className="hint" title={state.openDir}>
                {state.openDir}
              </span>
            </div>
          )}

          <div className="chips">
            <span className="chip">
              {t("规划动作")} <b>{state.actionTotal}</b>
            </span>
            <span className="chip">
              {t("执行总数")} <b>{state.exec.total}</b>
            </span>
            <span className="chip">
              {t("成功")} <b>{state.exec.applied}</b>
            </span>
            <span className="chip">
              {t("失败")} <b className={state.exec.failed ? "red" : ""}>{state.exec.failed}</b>
            </span>
            <span className="chip">
              {t("跳过")} <b>{state.exec.skipped}</b>
            </span>
          </div>

          {(state.suspects.length > 0 || (planOnlyRun && cfg.steps.specialDelete)) && (
            <>
              <h3 className="subhead">
                {t("存疑项（{n}）", { n: state.suspects.length })}
                {planOnlyRun ? t("：逐条确认或驳回后再执行") : t("：默认全部保留")}
              </h3>
              {state.suspects.length > 0 ? (
                <>
                  <div className="skiplist">
                    {state.suspects.map((s) => {
                      const d = decisions[s.key] ?? "keep";
                      return (
                        <div className="skiprow" key={s.key}>
                          <span className={`kind ${d === "delete" ? "kind-danger" : "kind-keep"}`}>
                            {d === "delete" ? t("将删除") : t("保留")}
                          </span>
                          <div className="suspectbody">
                            <p className="skippath">{s.path}</p>
                            <span className="hint">
                              {s.reason}
                              {s.suggestion ? t(" · 建议 {value}", { value: s.suggestion }) : ""}
                              {s.score ? t(" · 得分 {value}", { value: s.score }) : ""}
                            </span>
                          </div>
                          <div className="decision">
                            <button
                              className={`secondary ${d === "delete" ? "active" : ""}`}
                              onClick={() => decide(s.key, "delete")}
                            >
                              {t("确认删除")}
                            </button>
                            <button
                              className={`secondary ${d === "keep" ? "active" : ""}`}
                              onClick={() => decide(s.key, "keep")}
                            >
                              {t("保留（驳回）")}
                            </button>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                  <div className="actions">
                    <button className="secondary" onClick={() => decideAll("delete")}>
                      {t("全选确认")}
                    </button>
                    <button className="secondary" onClick={() => decideAll("keep")}>
                      {t("全部保留")}
                    </button>
                    <button
                      className="danger"
                      disabled={!canRun || running || confirmedKeys().length === 0}
                      title={!canRun ? licenseHint : undefined}
                      onClick={() => {
                        setPermanentAck(false);
                        setConfirmOpen(true);
                      }}
                    >
                      {t("执行并删除已确认项（{n}）", { n: confirmedKeys().length })}
                    </button>
                  </div>
                  <p className="hint">
                    {t("只有标为“确认删除”的存疑项会在下一次真实执行中删除；未确认或被驳回的一律保留。")}
                  </p>
                </>
              ) : (
                <p className="hint">{t("本次扫描没有发现存疑项。")}</p>
              )}
            </>
          )}

          {state.steps.length > 0 && (
            <>
              <h3 className="subhead">{t("九步明细")}</h3>
              <div className="orgsteps result">
                {state.steps.map((s) => (
                  <div className={`orgstep result ${s.skipped ? "off" : ""}`} key={s.id}>
                    <span className="orgstepno">{s.order}</span>
                    <span className={`kind ${s.skipped ? "kind-warn" : "kind-keep"}`}>
                      {s.skipped ? t("未执行") : t("已规划")}
                    </span>
                    <span className="orgsteptitle" title={t(s.title)}>
                      {t(s.title)}
                    </span>
                    <span className="num">{t("{n} 个动作", { n: s.actionNum })}</span>
                    <span className="hint orgcounters">{s.skipped ? "" : counterText(t, s.counters)}</span>
                  </div>
                ))}
              </div>
            </>
          )}

          {state.exec.errors.length > 0 && (
            <>
              <h3 className="subhead">{t("失败明细（{n}）", { n: state.exec.errors.length })}</h3>
              <div className="skiplist">
                {state.exec.errors.map((line, i) => (
                  <div className="skiprow" key={i}>
                    <span className="kind kind-danger">{t("失败")}</span>
                    <p className="skippath">{line}</p>
                  </div>
                ))}
              </div>
            </>
          )}

          <h3 className="subhead">
            {t("动作样例（前 200 条，共 {total} 条，显示 {shown} 条）", {
              total: state.actionTotal,
              shown: state.actions.length,
            })}
          </h3>
          <div className="samples orgsamples">
            {state.actions.map((a, i) => (
              <div className="sample" key={i}>
                <span className="seq">#{i + 1}</span>
                <span className={`kind ${a.kind === "suggest" ? "kind-warn" : a.kind === "delete" ? "kind-danger" : "kind-exact"}`}>
                  {a.kind === "move"
                    ? a.mode === "copy"
                      ? t("复制")
                      : a.mode === "unknown"
                        ? t("移动/复制")
                        : t(KIND_LABELS[a.kind] ?? a.kind)
                    : t(KIND_LABELS[a.kind] ?? a.kind)}
                  {a.kind === "delete" && cfg.deleteMode === "trash" ? t("（回收站）") : ""}
                </span>
                <span className="samplepath" title={`${a.step}\n${a.from ?? ""}${a.to ? "\n→ " + a.to : ""}`}>
                  {a.from}
                  {a.to ? ` → ${a.to}` : ""}
                  {a.reason ? `（${a.reason}）` : ""}
                </span>
              </div>
            ))}
            {state.actions.length === 0 && <p className="hint">{t("没有需要展示的动作。")}</p>}
          </div>
        </section>
      )}

      {confirmOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("确认执行九步整理")}</h3>
            <p>
              {t("根目录：")}
              <b>{cfg.root}</b>
            </p>
            <p>
              {t("整理目标：")}
              <b>{cfg.target || t("（根目录）")}</b>
            </p>
            <p>
              {t("执行顺序：")}
              <b>{t(orderLabel(cfg.usePreviewPlan ? "preview" : "standalone"))}</b>
              {t("；删除方式：")}
              <b>{cfg.deleteMode === "trash" ? t("移入回收站") : t("永久删除")}</b>
              {cfg.hardlink ? t("；去重改为建硬链接") : ""}
            </p>
            {confirmedKeys().length > 0 && (
              <p>
                {t("存疑项：将删除")} <b>{confirmedKeys().length}</b>{" "}
                {t("个已确认项；其余存疑项一律保留。")}
              </p>
            )}
            <p className="hint">{t("将执行以下步骤（按当前顺序）：")}</p>
            <ol className="confirmsteps">
              {activeOrder
                .filter((s) => {
                  const field = STEP_FIELD[s.id];
                  return field ? cfg.steps[field] : false;
                })
                .map((s) => (
                  <li key={s.id}>{t(s.title)}</li>
                ))}
            </ol>
            {cfg.deleteMode === "permanent" && (
              <label className="checkline">
                <input type="checkbox" checked={permanentAck} onChange={(e) => setPermanentAck(e.target.checked)} />
                <span>{t("我明白永久删除不可恢复，并已备份重要数据")}</span>
              </label>
            )}
            {!canRun && <p className="hint">{licenseHint || t("未授权：整理执行已禁用。")}</p>}
            <div className="actions">
              <button className="secondary" onClick={() => setConfirmOpen(false)}>
                {t("取消")}
              </button>
              <button
                className="danger"
                disabled={(cfg.deleteMode === "permanent" && !permanentAck) || !canRun}
                onClick={() => {
                  setConfirmOpen(false);
                  void start(false);
                }}
              >
                {t("确认执行")}
              </button>
            </div>
          </div>
        </div>
      )}
      {undoOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>{t("确认撤销上次整理")}</h3>
            <p>{t("将把最近一次真实整理中可还原的动作反向执行：改名/移动还原、新建目录删除、只读属性恢复。")}</p>
            <p className="hint">
              {t("已删除文件（回收站或永久删除）与硬链接替换无法由本程序还原，会在结果里如实计入“不可恢复”。")}
            </p>
            <div className="actions">
              <button className="secondary" onClick={() => setUndoOpen(false)}>
                {t("取消")}
              </button>
              <button className="danger" onClick={() => void doUndo()}>
                {t("确认撤销")}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function OrganizeFeatureStatus() {
  const t = useT();
  return (
    <div className="features">
      <h3>{t("本页功能状态（如实标注）")}</h3>
      <p>
        <span className="kind kind-keep">{t("已接通")}</span>{" "}
        {t("九步开关与两套执行顺序、试运行、真实执行、执行前确认、去重/改名/特殊删除/去只读/整理/清理空目录、冲突策略、回收站与永久删除、排除与大小过滤、结果明细、存疑项逐条确认或驳回并进入执行、整理后撤销（最近一次）、规则导入导出（版本化 JSON，未知字段与非法值校验）、配置方案预设（同名覆盖、不含根目录）、规则测试器（样例纯计算：改名/特殊判定/整理去向）、整理完成后打开目标文件夹")}
      </p>
      <p>
        <span className="kind kind-warn">{t("限制")}</span>{" "}
        {t("撤销只保留最近一次真实整理；已删除文件与硬链接替换无法还原。")}{" "}
        {t("macOS / Linux 的回收站、机器码、单实例已按平台实现并完成交叉编译验证，但尚未在真机运行。")}
      </p>
    </div>
  );
}
