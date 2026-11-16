// 设置对话框：外观 / 窗口与托盘 / 定时任务 / AI / 日志。
// 所有选项沿用 ui-settings.json 的兼容策略，由后端负责缺键补默认与坏值回退。

import { useEffect, useState } from "react";
import { api } from "./api";
import { useT } from "./i18n";
import type { ScheduleRunState, SettingsResult, UISettings } from "./types";

interface Props {
  onClose: () => void;
  onSaved: (s: UISettings) => void;
}

export function SettingsDialog({ onClose, onSaved }: Props) {
  const t = useT();
  const [res, setRes] = useState<SettingsResult | null>(null);
  const [draft, setDraft] = useState<UISettings | null>(null);
  const [sched, setSched] = useState<ScheduleRunState | null>(null);
  const [keyInput, setKeyInput] = useState("");
  const [clearKey, setClearKey] = useState(false);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const r = await api.getSettings();
        if (!alive) return;
        setRes(r);
        setDraft(r.settings);
        setSched(await api.getScheduleState());
      } catch (e) {
        if (alive) setErr(String(e));
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  function patch<K extends keyof UISettings>(key: K, value: UISettings[K]) {
    setDraft((d) => (d ? { ...d, [key]: value } : d));
  }

  async function save() {
    if (!draft) return;
    setBusy(true);
    setErr("");
    setMsg("");
    try {
      if (clearKey) {
        await api.saveAIKey("");
      } else if (keyInput.trim()) {
        await api.saveAIKey(keyInput.trim());
      }
      await api.saveSettings(draft);
      const fresh = await api.getSettings();
      setRes(fresh);
      setDraft(fresh.settings);
      setKeyInput("");
      setClearKey(false);
      setSched(await api.getScheduleState());
      setMsg(t("已保存"));
      onSaved(fresh.settings);
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal" role="dialog" aria-modal="true" aria-label={t("设置")}>
      <div className="modalbodysmall settingsdialog">
        <div className="dialoghead">
          <h3>{t("设置")}</h3>
          <button className="ghost" onClick={onClose}>
            {t("关闭")}
          </button>
        </div>
        {!draft ? (
          <p className="hint">{err || t("正在读取设置…")}</p>
        ) : (
          <>
            <section>
              <h4>{t("外观")}</h4>
              <label className="fieldrow">
                <span>{t("主题")}</span>
                <select
                  value={draft.window.theme}
                  onChange={(e) =>
                    patch("window", { ...draft.window, theme: e.target.value as typeof draft.window.theme })
                  }
                >
                  <option value="system">{t("跟随系统")}</option>
                  <option value="light">{t("日间")}</option>
                  <option value="dark">{t("夜间")}</option>
                </select>
              </label>
              <label className="fieldrow">
                <span>{t("语言")}</span>
                <select
                  value={draft.lang}
                  onChange={(e) => patch("lang", e.target.value as UISettings["lang"])}
                >
                  <option value="zh">{t("中文")}</option>
                  <option value="en">English</option>
                </select>
              </label>
              <p className="hint">{t("语言切换会立即生效并保存到设置文件；重新打开后保持一致。")}</p>
            </section>

            <section>
              <h4>{t("窗口与托盘")}</h4>
              <label className="fieldrow">
                <span>{t("关闭窗口时")}</span>
                <select
                  value={draft.window.closeAction}
                  onChange={(e) =>
                    patch("window", {
                      ...draft.window,
                      closeAction: e.target.value as typeof draft.window.closeAction,
                    })
                  }
                >
                  <option value="exit">{t("退出应用")}</option>
                  <option value="tray">{t("最小化到托盘")}</option>
                </select>
              </label>
              <label className="checkrow">
                <input
                  type="checkbox"
                  checked={draft.window.trayEnabled}
                  onChange={(e) => patch("window", { ...draft.window, trayEnabled: e.target.checked })}
                />
                <span>{t("显示托盘图标（右键菜单：打开、扫描、退出）")}</span>
              </label>
              <p className="hint">{t("托盘只在 Windows 上可用；关闭托盘后“最小化到托盘”会退化为退出。")}</p>
            </section>

            <section>
              <h4>{t("定时任务")}</h4>
              <label className="checkrow">
                <input
                  type="checkbox"
                  checked={draft.schedule.enabled}
                  onChange={(e) => patch("schedule", { ...draft.schedule, enabled: e.target.checked })}
                />
                <span>{t("启用定时任务（只做只读扫描，不删除、不整理）")}</span>
              </label>
              <label className="fieldrow">
                <span>{t("执行方式")}</span>
                <select
                  value={draft.schedule.mode}
                  onChange={(e) =>
                    patch("schedule", { ...draft.schedule, mode: e.target.value as "interval" | "daily" })
                  }
                >
                  <option value="interval">{t("按间隔")}</option>
                  <option value="daily">{t("每日定时")}</option>
                </select>
              </label>
              {draft.schedule.mode === "interval" ? (
                <label className="fieldrow">
                  <span>{t("间隔（分钟，10–10080）")}</span>
                  <input
                    type="number"
                    min={10}
                    max={10080}
                    value={draft.schedule.intervalMinutes}
                    onChange={(e) =>
                      patch("schedule", { ...draft.schedule, intervalMinutes: Number(e.target.value) })
                    }
                  />
                </label>
              ) : (
                <label className="fieldrow">
                  <span>{t("每天（HH:MM）")}</span>
                  <input
                    type="time"
                    value={draft.schedule.dailyTime}
                    onChange={(e) => patch("schedule", { ...draft.schedule, dailyTime: e.target.value })}
                  />
                </label>
              )}
              <label className="fieldrow">
                <span>{t("扫描目录（留空用查重页的根目录）")}</span>
                <input
                  value={draft.schedule.root}
                  placeholder="D:\\资料"
                  onChange={(e) => patch("schedule", { ...draft.schedule, root: e.target.value })}
                />
              </label>
              <p className="hint">
                {sched?.finishedAt
                  ? t("上次：{time} · {status} · {msg}", {
                      time: sched.finishedAt,
                      status: sched.status,
                      msg: sched.message ?? "",
                    })
                  : t("还没有执行记录。")}
              </p>
            </section>

            <section>
              <h4>{t("AI 配置")}</h4>
              <label className="checkrow">
                <input
                  type="checkbox"
                  checked={draft.ai.enabled}
                  onChange={(e) => patch("ai", { ...draft.ai, enabled: e.target.checked })}
                />
                <span>{t("启用 AI 分析（关闭时不发起任何网络请求）")}</span>
              </label>
              <label className="fieldrow">
                <span>{t("服务端点")}</span>
                <input
                  value={draft.ai.endpoint}
                  placeholder="https://example.com/v1/chat/completions"
                  onChange={(e) => patch("ai", { ...draft.ai, endpoint: e.target.value })}
                />
              </label>
              <label className="fieldrow">
                <span>{t("模型名")}</span>
                <input
                  value={draft.ai.model}
                  placeholder="gpt-4o-mini"
                  onChange={(e) => patch("ai", { ...draft.ai, model: e.target.value })}
                />
              </label>
              <label className="fieldrow">
                <span>{t("超时（秒，5–300）")}</span>
                <input
                  type="number"
                  min={5}
                  max={300}
                  value={draft.ai.timeoutSeconds}
                  onChange={(e) => patch("ai", { ...draft.ai, timeoutSeconds: Number(e.target.value) })}
                />
              </label>
              <label className="fieldrow">
                <span>{t("密钥")}</span>
                <input
                  type="password"
                  value={keyInput}
                  placeholder={draft.ai.keyHint ? t("已保存 {hint}", { hint: draft.ai.keyHint }) : t("未保存")}
                  onChange={(e) => {
                    setKeyInput(e.target.value);
                    setClearKey(false);
                  }}
                />
              </label>
              <label className="checkrow">
                <input
                  type="checkbox"
                  checked={clearKey}
                  onChange={(e) => {
                    setClearKey(e.target.checked);
                    if (e.target.checked) setKeyInput("");
                  }}
                />
                <span>{t("清除已保存的密钥")}</span>
              </label>
              <p className="hint">
                {res?.keyProtectionOk
                  ? t("密钥用 DPAPI 加密保存在 {path}，界面只显示后四位。", {
                      path: res?.keyStoragePath ?? "",
                    })
                  : t("当前平台不支持 DPAPI，密钥无法加密保存。")}
              </p>
            </section>

            <section>
              <h4>{t("运行日志")}</h4>
              <label className="fieldrow">
                <span>{t("保留天数（1–365）")}</span>
                <input
                  type="number"
                  min={1}
                  max={365}
                  value={draft.logs.retentionDays}
                  onChange={(e) => patch("logs", { ...draft.logs, retentionDays: Number(e.target.value) })}
                />
              </label>
              <p className="hint">
                {t("日志目录：{dir}", { dir: res?.path ? res.path.replace(/[^\\/]+$/, "logs") : "—" })}
              </p>
            </section>

            {res?.warning && <p className="hint warn">{res.warning}</p>}
            {err && <p className="hint warn">{err}</p>}
            {msg && <p className="hint ok">{msg}</p>}
            <div className="dialogfoot">
              <button onClick={onClose}>{t("取消")}</button>
              <button className="primary" disabled={busy} onClick={save}>
                {busy ? t("保存中…") : t("保存")}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
