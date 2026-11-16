import { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";
import type { TimelineMeta, TimelinePlan, TimelineRequest, TimelineState } from "./types";

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function formatBytes(n: number): string {
  if (n < 0) return "—";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

const FALLBACK_META: TimelineMeta = {
  buckets: [
    { value: "day", label: "按日" },
    { value: "week", label: "按周" },
    { value: "month", label: "按月" },
    { value: "year", label: "按年" },
  ],
  actions: [
    { value: "move", label: "移动" },
    { value: "copy", label: "复制" },
  ],
  conflicts: [
    { value: "rename", label: "改名保留" },
    { value: "skip", label: "跳过同名" },
    { value: "overwrite", label: "覆盖同名" },
  ],
};

export default function TimeOrganizer(props: { canRun: boolean; licenseHint: string }) {
  const t = useT();
  const [meta, setMeta] = useState<TimelineMeta>(FALLBACK_META);
  const [root, setRoot] = useState("");
  const [target, setTarget] = useState("");
  const [bucket, setBucket] = useState("day");
  const [action, setAction] = useState("move");
  const [conflict, setConflict] = useState("rename");
  const [includeHidden, setIncludeHidden] = useState(false);
  const [busy, setBusy] = useState(false);
  const [plan, setPlan] = useState<TimelinePlan>();
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [jobId, setJobId] = useState("");
  const [job, setJob] = useState<TimelineState>();

  useEffect(() => {
    let alive = true;
    api
      .getTimelineMeta()
      .then((m) => alive && setMeta(m))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!jobId) return;
    let alive = true;
    const tick = () => {
      api
        .getTimelineState(jobId)
        .then((st) => {
          if (!alive) return;
          setJob(st);
          if (st.phase === "done" || st.phase === "failed" || st.phase === "cancelled") {
            setJobId("");
            setBusy(false);
          }
        })
        .catch((err) => {
          if (!alive) return;
          setError(errorText(err));
          setJobId("");
          setBusy(false);
        });
    };
    const timer = window.setInterval(tick, 400);
    tick();
    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [jobId]);

  const req = (): TimelineRequest => ({
    root,
    target,
    bucket,
    action,
    conflict,
    includeHidden,
    confirmed: false,
  });

  async function pickRoot() {
    try {
      const p = await api.pickDirectory();
      if (p) setRoot(p);
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function pickTarget() {
    try {
      const p = await api.pickDirectory();
      if (p) setTarget(p);
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function preview() {
    setError("");
    setNote("");
    setPlan(undefined);
    setBusy(true);
    try {
      const p = await api.previewTimeline(req());
      setPlan(p);
      setNote(
        t("共 {total} 个文件，可整理 {ready} 个，冲突 {conflicts} 个，跳过 {skipped} 个。", {
          total: p.total,
          ready: p.ready,
          conflicts: p.conflicts,
          skipped: p.skipped,
        }),
      );
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  async function start() {
    setError("");
    setBusy(true);
    try {
      const id = await api.startTimeline({ ...req(), confirmed: true });
      setJobId(id);
      setJob(undefined);
    } catch (err) {
      setError(errorText(err));
      setBusy(false);
    }
  }

  async function cancel() {
    if (!jobId) return;
    try {
      await api.cancelTimeline(jobId);
    } catch (err) {
      setError(errorText(err));
    }
  }

  const progress = job && job.total > 0 ? Math.min(1, job.done / job.total) : 0;
  const sample = useMemo(() => (plan ? plan.items.slice(0, 800) : []), [plan]);

  return (
    <section className="card fill">
      <h2>{t("按时间整理")}</h2>
      <p className="hint">
        {t("按文件的拍摄时间（JPEG/TIFF 的 EXIF 优先，取不到时用修改时间）分到日、周、月、年目录，支持移动或复制；同名冲突按所选策略处理。预演只读元数据，不会改动任何文件。")}
      </p>

      <div className="formgrid">
        <label className="fullrow">
          <span>{t("源目录")}</span>
          <div className="dirrow dropzone">
            <input
              className="path"
              readOnly
              value={root}
              placeholder={t("尚未选择源目录")}
              aria-label={t("按时间整理源目录")}
            />
            <button className="secondary" onClick={pickRoot}>
              {t("选择目录")}
            </button>
          </div>
        </label>
        <label className="fullrow">
          <span>{t("目标目录（容器目录，程序会在其中建日/周/月/年子目录）")}</span>
          <div className="dirrow">
            <input
              className="path"
              readOnly
              value={target}
              placeholder={t("尚未选择目标目录")}
              aria-label={t("按时间整理目标目录")}
            />
            <button className="secondary" onClick={pickTarget}>
              {t("选择目录")}
            </button>
          </div>
        </label>
        <label>
          <span>{t("分桶粒度")}</span>
          <select value={bucket} onChange={(e) => setBucket(e.target.value)}>
            {meta.buckets.map((b) => (
              <option key={b.value} value={b.value}>
                {t(b.label)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>{t("处理方式")}</span>
          <select value={action} onChange={(e) => setAction(e.target.value)}>
            {meta.actions.map((a) => (
              <option key={a.value} value={a.value}>
                {t(a.label)}
              </option>
            ))}
          </select>
        </label>
        <label>
          <span>{t("同名冲突")}</span>
          <select value={conflict} onChange={(e) => setConflict(e.target.value)}>
            {meta.conflicts.map((c) => (
              <option key={c.value} value={c.value}>
                {t(c.label)}
              </option>
            ))}
          </select>
        </label>
        <label className="checkline">
          <input type="checkbox" checked={includeHidden} onChange={(e) => setIncludeHidden(e.target.checked)} />
          <span>{t("包含隐藏/系统文件（默认跳过）")}</span>
        </label>
      </div>

      <div className="actions">
        <button className="secondary" disabled={!root || !target || busy} onClick={preview}>
          {t("预演计划")}
        </button>
        <button className="primary" disabled={!plan || plan.ready === 0 || busy || !props.canRun || !confirmed} onClick={start}>
          {t("开始整理")}
        </button>
        {jobId && (
          <button className="secondary" onClick={cancel}>
            {t("取消")}
          </button>
        )}
      </div>

      {!props.canRun && <p className="hint warn">{props.licenseHint}</p>}
      {error && <p className="banner error">{error}</p>}
      {note && <p className="hint">{note}</p>}

      {plan && plan.notes.length > 0 && (
        <ul className="hint">
          {plan.notes.map((n, i) => (
            <li key={i}>{n}</li>
          ))}
        </ul>
      )}

      {job && (
        <div>
          <div className={`progress ${job.phase === "running" ? "" : "done"}`}>
            <div className="bar" style={{ width: `${Math.round(progress * 100)}%` }} />
          </div>
          <p className="hint">
            {job.phase === "running" ? (
              <>
                {t("整理中：{done} / {total}", { done: job.done, total: job.total })}
                {job.current ? t("（{current}）", { current: job.current }) : ""}
              </>
            ) : job.phase === "done" ? (
              t("已完成：移动 {moved}，复制 {copied}，覆盖 {overwrote}，失败 {failed}", {
                moved: job.result.moved,
                copied: job.result.copied,
                overwrote: job.result.overwrote,
                failed: job.result.failed,
              })
            ) : (
              <>
                {job.phase === "cancelled" ? t("已取消") : t("执行失败")}
                {job.error ? t("：{error}", { error: job.error }) : ""}
              </>
            )}
          </p>
          {job.result.errors.length > 0 && (
            <div className="banner error">
              {t("失败明细（前 10 条）：")}
              <ul>
                {job.result.errors.slice(0, 10).map((e, i) => (
                  <li key={i}>{e}</li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}

      {!jobId && plan && plan.ready > 0 && (
        <label className="checkline">
          <input type="checkbox" checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} />
          <span>
            {t("我确认按上面的计划")}
            {action === "move" ? t("移动") : t("复制")}{" "}
            {t("{count} 个文件（{size}），冲突策略为「{policy}」。", {
              count: plan.ready,
              size: formatBytes(plan.totalSize),
              policy: (() => {
                const label = meta.conflicts.find((c) => c.value === conflict)?.label;
                return label ? t(label) : conflict;
              })(),
            })}
          </span>
        </label>
      )}

      {sample.length > 0 && (
        <div className="tlwrap">
          <table className="tl-table">
            <thead>
              <tr>
                <th>{t("时间")}</th>
                <th>{t("来源")}</th>
                <th>{t("目标")}</th>
                <th>{t("大小")}</th>
                <th>{t("状态")}</th>
              </tr>
            </thead>
            <tbody>
              {sample.map((it, i) => (
                <tr key={`${it.source}-${i}`} className={it.status === "skipped" ? "skipped" : ""}>
                  <td>
                    {it.when}
                    <span className="tag">{it.whenFrom === "exif" ? "EXIF" : t("修改时间")}</span>
                  </td>
                  <td title={it.source}>{it.relPath}</td>
                  <td title={it.dest}>{it.bucket}</td>
                  <td className="num">{formatBytes(it.size)}</td>
                  <td>
                    {it.status === "ready"
                      ? t("就绪")
                      : it.status === "conflict"
                        ? it.note || t("冲突")
                        : it.status === "skipped"
                          ? t("跳过")
                          : t("错误")}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {plan?.truncated && (
            <p className="hint">{t("预览条目过多，仅显示前 {n} 条；执行时会处理全部文件。", { n: sample.length })}</p>
          )}
        </div>
      )}
    </section>
  );
}
