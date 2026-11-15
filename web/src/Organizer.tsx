// 九步整理页面：接 cmd/filededup/organize.go。
// 步骤顺序与标题全部来自后端 GetOrganizeMeta（引擎跑空快照得到），前端不复制顺序表。
import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import type {
  Counter,
  KeepRule,
  OrganizeMeta,
  OrganizeRequest,
  OrganizeSettings,
  OrganizeState,
  OrganizeStepMeta,
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
  suggest: "存疑（仅报告）",
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

function counterText(c: Counter): string {
  const parts = COUNTER_LABELS.filter(([k]) => c[k] > 0).map(([k, label]) => `${label} ${c[k]}`);
  return parts.length ? parts.join(" · ") : "无计数变化";
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
  const [cfg, setCfg] = useState<OrganizeSettings>(() => initial ?? defaultOrganizeSettings());
  const [excludesText, setExcludesText] = useState(() => (initial?.excludes ?? []).join(" "));
  const [meta, setMeta] = useState<OrganizeMeta>();
  const [metaError, setMetaError] = useState("");
  const [error, setError] = useState("");
  const [state, setState] = useState<OrganizeState>();
  const [jobId, setJobId] = useState("");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [permanentAck, setPermanentAck] = useState(false);

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
        if (alive) setMetaError("步骤顺序加载失败：" + errorText(err));
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
        }
      } catch (err) {
        if (stopped) return;
        stopped = true;
        window.clearInterval(timer);
        setError("读取整理进度失败：" + errorText(err));
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

  async function start(dryRun: boolean) {
    setError("");
    try {
      const req: OrganizeRequest = { ...cfg, dryRun };
      persistRef.current(cfg);
      const id = await api.startOrganize(req);
      setState(undefined);
      setJobId(id);
    } catch (err) {
      setError(errorText(err));
    }
  }

  return (
    <div className="orgpage">
      <section className="card">
        <div className="sectionhead">
          <h2>九步整理</h2>
          {state && (
            <span className={`badge ${state.dryRun ? "warn" : "danger"}`}>
              {state.dryRun ? "试运行结果" : "真实执行结果"}
            </span>
          )}
        </div>

        <div className="dirrow">
          <input className="path" readOnly value={cfg.root} placeholder="尚未选择要整理的目录" aria-label="整理根目录" />
          <button className="secondary" onClick={pickDirectory} disabled={running}>
            选择目录
          </button>
        </div>

        <div className="formgrid">
          <label>
            <span>整理目标子目录（相对根目录，留空 = 根目录）</span>
            <input value={cfg.target} onChange={(e) => patch({ target: e.target.value })} placeholder="例如 整理结果" />
          </label>
          <label>
            <span>整理方式（第五步）</span>
            <select value={cfg.mode} onChange={(e) => patch({ mode: e.target.value })}>
              {Object.entries(MODE_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          {cfg.mode === "keyword" && (
            <label>
              <span>关键字（逗号/分号/空格分隔）</span>
              <input value={cfg.keyword} onChange={(e) => patch({ keyword: e.target.value })} />
            </label>
          )}
          {cfg.mode === "namefeature" && (
            <label>
              <span>正则表达式</span>
              <input value={cfg.regex} onChange={(e) => patch({ regex: e.target.value })} placeholder="例如 ^\d{4}-" />
            </label>
          )}
          {cfg.mode === "fixedfolder" && (
            <label>
              <span>指定文件夹名</span>
              <input value={cfg.fixedDir} onChange={(e) => patch({ fixedDir: e.target.value })} />
            </label>
          )}
          <label>
            <span>冲突策略</span>
            <select value={cfg.conflict} onChange={(e) => patch({ conflict: e.target.value })}>
              {Object.entries(CONFLICT_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>删除方式（特殊删除与去重删除）</span>
            <select
              value={cfg.deleteMode}
              onChange={(e) => patch({ deleteMode: e.target.value as OrganizeSettings["deleteMode"] })}
            >
              <option value="trash">移入回收站（可恢复）</option>
              <option value="permanent">永久删除（不可恢复）</option>
            </select>
          </label>
          <label>
            <span>内容去重保留规则</span>
            <select value={cfg.keepPolicy} onChange={(e) => patch({ keepPolicy: e.target.value as KeepRule })}>
              {Object.entries(ORG_KEEP_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>相似图片保留规则</span>
            <select value={cfg.imageKeepPolicy} onChange={(e) => patch({ imageKeepPolicy: e.target.value as KeepRule })}>
              {Object.entries(ORG_KEEP_LABELS).map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>相似图片阈值（0 = 完全相同，8 = 默认）</span>
            <input
              type="number"
              min={0}
              max={64}
              value={cfg.imageThreshold}
              onChange={(e) => patch({ imageThreshold: Math.max(0, Math.min(64, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>音频时长容差（秒）</span>
            <input
              type="number"
              min={0}
              max={60}
              value={cfg.audioTolerance}
              onChange={(e) => patch({ audioTolerance: Math.max(0, Math.min(60, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>垃圾识别强度（0 低 / 1 中 / 2 高）</span>
            <input
              type="number"
              min={0}
              max={2}
              value={cfg.strength}
              onChange={(e) => patch({ strength: Math.max(0, Math.min(2, Number(e.target.value) || 0)) })}
            />
          </label>
          <label>
            <span>最小文件大小（MB，0 = 不限）</span>
            <input
              type="number"
              min={0}
              value={Math.round(cfg.minSize / MB)}
              onChange={(e) => patch({ minSize: Math.max(0, Number(e.target.value) || 0) * MB })}
            />
          </label>
          <label>
            <span>排除子串（逗号或空格分隔）</span>
            <input
              value={excludesText}
              onChange={(e) => {
                setExcludesText(e.target.value);
                patch({ excludes: e.target.value.split(/[,，;；\s]+/).filter(Boolean) });
              }}
              placeholder="例如 node_modules .git"
            />
          </label>
          <label>
            <span>保护词（跳过改名与特殊删除判定）</span>
            <input value={cfg.protectWords} onChange={(e) => patch({ protectWords: e.target.value })} />
          </label>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.includeHidden}
              onChange={(e) => patch({ includeHidden: e.target.checked })}
            />
            <span>包含隐藏/系统文件</span>
          </label>
          <label className="checkline">
            <input type="checkbox" checked={cfg.hardlink} onChange={(e) => patch({ hardlink: e.target.checked })} />
            <span>去重时建立硬链接（同卷，替代删除）</span>
          </label>
          <label className="checkline">
            <input
              type="checkbox"
              checked={cfg.continueOnError}
              onChange={(e) => patch({ continueOnError: e.target.checked })}
            />
            <span>出错后继续执行剩余动作</span>
          </label>
        </div>
      </section>

      <section className="card">
        <div className="sectionhead">
          <h2>执行顺序与步骤开关</h2>
          <span className="hint">顺序来自引擎，两套顺序只差“去重三步”的位置</span>
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
                <h4>无预览计划 · 独立顺序</h4>
                <ol>
                  {standalone.map((s) => (
                    <li key={s.id} title={s.title}>
                      {s.title}
                    </li>
                  ))}
                </ol>
              </button>
              <button
                type="button"
                className={`ordercard ${cfg.usePreviewPlan ? "active" : ""}`}
                onClick={() => patch({ usePreviewPlan: true })}
              >
                <h4>有预览计划 · 先去重后整理</h4>
                <ol>
                  {preview.map((s) => (
                    <li key={s.id} title={s.title}>
                      {s.title}
                    </li>
                  ))}
                </ol>
              </button>
            </div>

            <p className="hint">
              当前执行顺序：<b>{orderLabel(cfg.usePreviewPlan ? "preview" : "standalone")}</b>
              ，已启用 {enabledCount} / 9 步。开关只影响是否执行该步，不改变顺序。
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
                    <span className="orgsteptitle" title={s.title}>
                      {s.title}
                    </span>
                  </label>
                );
              })}
            </div>

            <div className="actions">
              <button className="secondary" disabled={!cfg.root || enabledCount === 0 || running} onClick={() => void start(true)}>
                试运行（不改文件）
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
                执行（会改文件）
              </button>
              {running && <span className="hint">正在执行，按钮暂时禁用。</span>}
              {!running && !canRun && (
                <span className="hint">{licenseHint || "未授权：整理执行已禁用。"}</span>
              )}
            </div>

            <p className="hint">
              试运行只创建目标目录，不移动、不删除、不修改属性；真实执行会按上面的动作落盘。两者都必须先经过确认。
            </p>
          </>
        )}

        <OrganizeFeatureStatus />
      </section>

      {error && (
        <div className="banner error" role="alert">
          <span>{error}</span>
          <button className="link" onClick={() => setError("")}>
            关闭
          </button>
        </div>
      )}

      {state && (
        <section className="card">
          <div className="sectionhead">
            <h2>整理结果</h2>
            <div className="chips inline">
              <span className="chip">顺序 <b>{orderLabel(state.order)}</b></span>
              <span className={`chip ${state.dryRun ? "chip-warn" : "chip-danger"}`}>
                {state.dryRun ? "试运行（未落盘）" : "真实执行"}
              </span>
              <span className="chip">耗时 <b>{(state.elapsedMs / 1000).toFixed(1)} s</b></span>
            </div>
          </div>

          {state.phase === "running" && (
            <div className="banner info">
              <span>
                阶段：<b>{state.stage || "准备中"}</b>
                {state.dryRun ? "（试运行）" : "（真实执行）"}
              </span>
            </div>
          )}

          {state.error && <div className="banner error">{state.error}</div>}

          {state.dryRun ? (
            <div className="banner perf">
              这是试运行结果：只创建了目标目录，文件没有移动、没有删除、没有改属性。下面的动作数与计数是规划结果。
            </div>
          ) : (
            !running && (
              <div className="banner danger">
                这是真实执行结果：动作已落盘。
                {cfg.deleteMode === "trash" ? "删除项已移入回收站。" : "删除项已永久删除，不可恢复。"}
              </div>
            )
          )}

          <div className="chips">
            <span className="chip">规划动作 <b>{state.actionTotal}</b></span>
            <span className="chip">执行总数 <b>{state.exec.total}</b></span>
            <span className="chip">成功 <b>{state.exec.applied}</b></span>
            <span className="chip">失败 <b className={state.exec.failed ? "red" : ""}>{state.exec.failed}</b></span>
            <span className="chip">跳过 <b>{state.exec.skipped}</b></span>
          </div>

          {state.steps.length > 0 && (
            <>
              <h3 className="subhead">九步明细</h3>
              <div className="orgsteps result">
                {state.steps.map((s) => (
                  <div className={`orgstep result ${s.skipped ? "off" : ""}`} key={s.id}>
                    <span className="orgstepno">{s.order}</span>
                    <span className={`kind ${s.skipped ? "kind-warn" : "kind-keep"}`}>
                      {s.skipped ? "未执行" : "已规划"}
                    </span>
                    <span className="orgsteptitle" title={s.title}>
                      {s.title}
                    </span>
                    <span className="num">{s.actionNum} 个动作</span>
                    <span className="hint orgcounters">{s.skipped ? "" : counterText(s.counters)}</span>
                  </div>
                ))}
              </div>
            </>
          )}

          {state.exec.errors.length > 0 && (
            <>
              <h3 className="subhead">失败明细（{state.exec.errors.length}）</h3>
              <div className="skiplist">
                {state.exec.errors.map((line, i) => (
                  <div className="skiprow" key={i}>
                    <span className="kind kind-danger">失败</span>
                    <p className="skippath">{line}</p>
                  </div>
                ))}
              </div>
            </>
          )}

          <h3 className="subhead">
            动作样例（前 200 条，共 {state.actionTotal} 条，显示 {state.actions.length} 条）
          </h3>
          <div className="samples orgsamples">
            {state.actions.map((a, i) => (
              <div className="sample" key={i}>
                <span className="seq">#{i + 1}</span>
                <span className={`kind ${a.kind === "suggest" ? "kind-warn" : a.kind === "delete" ? "kind-danger" : "kind-exact"}`}>
                  {KIND_LABELS[a.kind] ?? a.kind}
                  {a.kind === "delete" && cfg.deleteMode === "trash" ? "（回收站）" : ""}
                </span>
                <span className="samplepath" title={`${a.step}\n${a.from ?? ""}${a.to ? "\n→ " + a.to : ""}`}>
                  {a.from}
                  {a.to ? ` → ${a.to}` : ""}
                  {a.reason ? `（${a.reason}）` : ""}
                </span>
              </div>
            ))}
            {state.actions.length === 0 && <p className="hint">没有需要展示的动作。</p>}
          </div>
        </section>
      )}

      {confirmOpen && (
        <div className="modal" role="dialog" aria-modal="true">
          <div className="modalbodysmall">
            <h3>确认执行九步整理</h3>
            <p>
              根目录：<b>{cfg.root}</b>
            </p>
            <p>
              整理目标：<b>{cfg.target || "（根目录）"}</b>
            </p>
            <p>
              执行顺序：<b>{orderLabel(cfg.usePreviewPlan ? "preview" : "standalone")}</b>；删除方式：
              <b>{cfg.deleteMode === "trash" ? "移入回收站" : "永久删除"}</b>
              {cfg.hardlink ? "；去重改为建硬链接" : ""}
            </p>
            <p className="hint">将执行以下步骤（按当前顺序）：</p>
            <ol className="confirmsteps">
              {activeOrder
                .filter((s) => {
                  const field = STEP_FIELD[s.id];
                  return field ? cfg.steps[field] : false;
                })
                .map((s) => (
                  <li key={s.id}>{s.title}</li>
                ))}
            </ol>
            {cfg.deleteMode === "permanent" && (
              <label className="checkline">
                <input type="checkbox" checked={permanentAck} onChange={(e) => setPermanentAck(e.target.checked)} />
                <span>我明白永久删除不可恢复，并已备份重要数据</span>
              </label>
            )}
            {!canRun && <p className="hint">{licenseHint || "未授权：整理执行已禁用。"}</p>}
            <div className="actions">
              <button className="secondary" onClick={() => setConfirmOpen(false)}>
                取消
              </button>
              <button
                className="danger"
                disabled={(cfg.deleteMode === "permanent" && !permanentAck) || !canRun}
                onClick={() => {
                  setConfirmOpen(false);
                  void start(false);
                }}
              >
                确认执行
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function OrganizeFeatureStatus() {
  return (
    <div className="features">
      <h3>本页功能状态（如实标注）</h3>
      <p>
        <span className="kind kind-keep">已接通</span> 九步开关与两套执行顺序、试运行、真实执行、执行前确认、去重/改名/
        特殊删除/去只读/整理/清理空目录、冲突策略、回收站与永久删除、排除与大小过滤、结果明细
      </p>
      <p>
        <span className="kind kind-warn">未实现</span> 存疑项逐项人工确认（当前只记为“存疑（仅报告）”，不落盘）、整理后撤销、
        规则导入导出
      </p>
    </div>
  );
}
