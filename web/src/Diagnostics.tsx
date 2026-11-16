import { useEffect, useState } from "react";
import { api } from "./api";
import type { ClearLogsResult, EnvCheckItem, LogOverview } from "./types";

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

export function DiagnosticsDialog({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<"logs" | "env">("logs");
  const [overview, setOverview] = useState<LogOverview>();
  const [selected, setSelected] = useState("");
  const [text, setText] = useState("");
  const [checks, setChecks] = useState<EnvCheckItem[]>([]);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);

  async function loadLogs() {
    setError("");
    try {
      const ov = await api.getLogOverview();
      setOverview(ov);
      if (ov.files.length > 0) {
        const first = ov.files[0].name;
        if (!selected || !ov.files.some((f) => f.name === selected)) {
          setSelected(first);
          setText(await api.readLogFile(first));
        }
      } else {
        setSelected("");
        setText("");
      }
    } catch (err) {
      setError(errorText(err));
    }
  }

  async function runChecks() {
    setError("");
    setBusy(true);
    try {
      setChecks(await api.runEnvironmentCheck());
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void loadLogs();
    void runChecks();
    // 只在打开时加载一次；刷新由按钮触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function openFile(name: string) {
    setSelected(name);
    setError("");
    try {
      setText(await api.readLogFile(name));
    } catch (err) {
      setText("");
      setError(errorText(err));
    }
  }

  async function clearLogs() {
    setError("");
    setNote("");
    setBusy(true);
    try {
      const res: ClearLogsResult = await api.clearLogs();
      setNote(
        `已删除 ${res.deleted} 个文件，释放 ${formatBytes(res.freed)}。` +
          (res.errors.length > 0 ? ` ${res.errors.length} 个文件被占用未删除。` : ""),
      );
      setConfirmClear(false);
      await loadLogs();
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  async function openDir() {
    if (!overview?.dir) return;
    try {
      await api.openPath(overview.dir);
    } catch (err) {
      setError(errorText(err));
    }
  }

  return (
    <div className="modal" role="dialog" aria-modal="true" aria-label="诊断">
      <div className="modalbodysmall diagdialog">
        <div className="dialoghead">
          <h3>诊断</h3>
          <div className="dialogactions">
            <button className={tab === "logs" ? "primary" : "secondary"} onClick={() => setTab("logs")}>
              日志
            </button>
            <button className={tab === "env" ? "primary" : "secondary"} onClick={() => setTab("env")}>
              环境检测
            </button>
          </div>
        </div>

        {error && <p className="banner error">{error}</p>}
        {note && <p className="hint">{note}</p>}

        {tab === "logs" ? (
          <div className="diagbody">
            <aside className="diaglist">
              <p className="hint">日志目录：{overview?.dir ?? "—"}</p>
              {(overview?.files.length ?? 0) === 0 && <p className="hint">暂无日志文件。</p>}
              {overview?.files.map((f) => (
                <button
                  key={f.name}
                  className={`diagfile ${selected === f.name ? "active" : ""}`}
                  onClick={() => openFile(f.name)}
                  title={f.path}
                >
                  <span className="diagfilename">{f.name}</span>
                  <span className="diaghint">
                    {formatBytes(f.size)} · {f.modTime}
                  </span>
                </button>
              ))}
            </aside>
            <main className="diagpreview">
              <pre>{text || "选择左侧日志文件查看内容（只显示尾部 512 KB）。"}</pre>
            </main>
          </div>
        ) : (
          <div className="diagbody single">
            <div className="envtable">
              {checks.map((c) => (
                <div key={c.name} className={`envrow ${c.status}`}>
                  <span className={`envdot ${c.status}`} aria-hidden="true" />
                  <span className="envname">{c.name}</span>
                  <span className="envdetail">
                    {c.detail}
                    {c.fix && <span className="envfix">{c.fix}</span>}
                  </span>
                </div>
              ))}
              {checks.length === 0 && <p className="hint">尚未运行检测。</p>}
            </div>
          </div>
        )}

        <div className="dialogfoot">
          {tab === "logs" ? (
            <>
              <button className="secondary" disabled={busy} onClick={() => void loadLogs()}>
                刷新列表
              </button>
              <button className="secondary" onClick={() => void openDir()} disabled={!overview?.dir}>
                打开日志目录
              </button>
              {confirmClear ? (
                <>
                  <span className="hint">清除会删除日志目录下全部 *.log / *.jsonl / *.txt，不可恢复。</span>
                  <button className="primary danger" disabled={busy} onClick={() => void clearLogs()}>
                    确认清除
                  </button>
                  <button className="secondary" onClick={() => setConfirmClear(false)}>
                    取消
                  </button>
                </>
              ) : (
                <button className="secondary" disabled={busy || !overview?.files.length} onClick={() => setConfirmClear(true)}>
                  清除日志
                </button>
              )}
            </>
          ) : (
            <button className="secondary" disabled={busy} onClick={() => void runChecks()}>
              重新检测
            </button>
          )}
          <button className="primary" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}
