import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./api";
import { VirtualList } from "./components/VirtualList";
import { useT } from "./i18n";
import type { SearchBackendInfo, SearchMatch, SearchRequest, SearchState } from "./types";

const MB = 1024 * 1024;
const PAGE = 200;
const ROW_HEIGHT = 34;

type SortKey = "name" | "path" | "size" | "modTime";

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

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * 本地搜索页。结果采用虚拟列表 + 服务端分页：数组长度等于命中总数，
 * 但只有滚动窗口附近的页会被拉取与渲染，10 万条结果也不会一次性进 DOM。
 */
export function Search({ canRun, licenseHint }: { canRun: boolean; licenseHint: string }) {
  const t = useT();
  const [roots, setRoots] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  const [mode, setMode] = useState<"substring" | "wildcard">("substring");
  const [extsText, setExtsText] = useState("");
  const [minMB, setMinMB] = useState(0);
  const [maxMB, setMaxMB] = useState(0);
  const [includeHidden, setIncludeHidden] = useState(false);
  const [excludesText, setExcludesText] = useState("");
  // 检索后端：本地遍历默认；Everything 仅在检测到 es.exe 时可选。
  const [backend, setBackend] = useState<"local" | "everything">("local");
  const [backends, setBackends] = useState<SearchBackendInfo[]>([]);

  const [jobId, setJobId] = useState("");
  const jobRef = useRef("");
  const genRef = useRef(0);
  const pagesRef = useRef(new Map<number, SearchMatch[]>());
  const pendingRef = useRef(new Set<number>());
  const sortRef = useRef({ key: "path", desc: false });

  const [state, setState] = useState<SearchState>();
  const [error, setError] = useState("");
  const [rows, setRows] = useState<(SearchMatch | null)[]>([]);
  const [total, setTotal] = useState(0);
  const [truncated, setTruncated] = useState(false);
  const [sortKey, setSortKey] = useState<SortKey>("path");
  const [sortDesc, setSortDesc] = useState(false);

  const resetResult = useCallback((nextTotal: number) => {
    genRef.current += 1;
    pagesRef.current.clear();
    pendingRef.current.clear();
    setTotal(nextTotal);
    setRows(nextTotal > 0 ? new Array<SearchMatch | null>(nextTotal).fill(null) : []);
  }, []);

  const loadBackends = useCallback(async () => {
    try {
      const r = await api.getSearchBackends();
      setBackends(r.backends);
      setError("");
    } catch (err) {
      setError(t("读取搜索后端失败：{msg}", { msg: errorText(err) }));
    }
  }, []);

  useEffect(() => {
    void loadBackends();
  }, [loadBackends]);

  const loadPage = useCallback(async (pageIndex: number) => {
    const job = jobRef.current;
    if (!job) return;
    const gen = genRef.current;
    pendingRef.current.add(pageIndex);
    try {
      const page = await api.getSearchResults(job, sortRef.current.key, sortRef.current.desc, pageIndex * PAGE, PAGE);
      if (gen !== genRef.current) return;
      pagesRef.current.set(pageIndex, page.rows);
      setRows((prev) => {
        const next = prev.slice();
        page.rows.forEach((row, i) => {
          const idx = pageIndex * PAGE + i;
          if (idx < next.length) next[idx] = row;
        });
        return next;
      });
    } catch (err) {
      if (gen === genRef.current) setError(t("读取搜索结果失败：{msg}", { msg: errorText(err) }));
    } finally {
      pendingRef.current.delete(pageIndex);
    }
  }, []);

  const ensureRange = useCallback(
    (start: number, end: number) => {
      if (!jobRef.current || total === 0) return;
      const first = Math.floor(start / PAGE);
      const last = Math.floor(Math.max(0, end - 1) / PAGE);
      for (let p = first; p <= last; p++) {
        if (!pagesRef.current.has(p) && !pendingRef.current.has(p)) void loadPage(p);
      }
    },
    [total, loadPage],
  );

  // 轮询进度；完成后按当前排序拉第一页拿到总数，再按需分页。
  useEffect(() => {
    if (!jobId) return;
    let alive = true;
    let timer = 0;
    const tick = async () => {
      try {
        const st = await api.getSearchState(jobId);
        if (!alive) return;
        setState(st);
        if (!st.done) {
          timer = window.setTimeout(tick, 300);
          return;
        }
        const first = await api.getSearchResults(jobId, sortRef.current.key, sortRef.current.desc, 0, 1);
        if (!alive) return;
        setTruncated(first.truncated);
        resetResult(first.total);
      } catch (err) {
        if (alive) setError(errorText(err));
      }
    };
    timer = window.setTimeout(tick, 150);
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [jobId, resetResult]);

  useEffect(() => {
    if (jobId && jobRef.current === jobId) ensureRange(0, Math.max(PAGE, 40));
  }, [jobId, rows.length, ensureRange]);

  const start = async () => {
    setError("");
    if (!canRun) {
      setError(licenseHint);
      return;
    }
    if (roots.length === 0) {
      setError(t("请先添加至少一个搜索目录。"));
      return;
    }
    const req: SearchRequest = {
      roots,
      query: query.trim(),
      mode,
      exts: extsText
        .split(/[,，;；\s]+/)
        .map((s) => s.trim())
        .filter(Boolean),
      minSize: Math.max(0, Math.round(minMB * MB)),
      maxSize: Math.max(0, Math.round(maxMB * MB)),
      includeHidden,
      excludes: excludesText.split(/[,，;；\s]+/).filter(Boolean),
      limit: 200000,
      backend,
    };
    try {
      const id = await api.startSearch(req);
      jobRef.current = id;
      setJobId(id);
      setState({ id, running: true, done: false, cancelled: false, visited: 0, dirs: 0, matched: 0, truncated: false, elapsedMs: 0, backend });
      setTruncated(false);
      resetResult(0);
      setSortKey("path");
      setSortDesc(false);
      sortRef.current = { key: "path", desc: false };
    } catch (err) {
      setError(errorText(err));
    }
  };

  const cancel = async () => {
    if (!jobId) return;
    try {
      await api.cancelSearch(jobId);
    } catch (err) {
      setError(errorText(err));
    }
  };

  const changeSort = (key: SortKey) => {
    const desc = key === sortKey ? !sortDesc : false;
    setSortKey(key);
    setSortDesc(desc);
    sortRef.current = { key, desc };
    // 排序变了，缓存页与已填充的行全部失效；保留总长度，让虚拟列表滚动位置不跳。
    genRef.current += 1;
    pagesRef.current.clear();
    pendingRef.current.clear();
    setRows(total > 0 ? new Array<SearchMatch | null>(total).fill(null) : []);
  };

  const addRoot = async () => {
    try {
      const dir = await api.pickDirectory();
      if (!dir) return;
      setRoots((prev) => (prev.some((p) => p.toLowerCase() === dir.toLowerCase()) ? prev : [...prev, dir]));
    } catch (err) {
      setError(t("选择目录失败：{msg}", { msg: errorText(err) }));
    }
  };

  const running = Boolean(state?.running);
  const everythingInfo = backends.find((b) => b.key === "everything");
  const everythingAvailable = Boolean(everythingInfo?.available);
  const backendInfo = backends.find((b) => b.key === backend);
  const activeBackend = state?.backend ?? backend;
  const sortMark = (key: SortKey) => (sortKey === key ? (sortDesc ? " ↓" : " ↑") : "");

  // 外部工具被卸载后，已选中的 Everything 后端回落到本地，避免留下不可用的状态。
  useEffect(() => {
    if (!everythingAvailable && backend === "everything") setBackend("local");
  }, [everythingAvailable, backend]);

  return (
    <div className="searchpage">
      <div className="card">
        <h2>{t("文件搜索")}</h2>
        <p className="hint">
          {t(
            "按文件名或路径搜索一个或多个目录。不带 * ? 时按子串匹配；通配符模式可写 *.jpg、2024/*/report?.pdf。默认后端实时遍历目录，结果与磁盘一致；检测到 Everything（es.exe）时可改用它的索引作为可选后端。",
          )}
        </p>

        <div className="subhead">{t("搜索目录")}</div>
        <div className="rootlist">
          {roots.length === 0 && <p className="hint">{t("还没有添加目录。")}</p>}
          {roots.map((root) => (
            <div className="rootitem" key={root}>
              <span className="path" title={root}>
                {root}
              </span>
              <button className="ghost" onClick={() => setRoots((prev) => prev.filter((r) => r !== root))}>
                {t("移除")}
              </button>
            </div>
          ))}
          <button className="ghost" onClick={addRoot} disabled={running}>
            {t("添加目录")}
          </button>
        </div>

        <div className="grid2">
          <label className="fieldrow">
            <span>{t("检索后端")}</span>
            <select
              value={backend}
              onChange={(e) => setBackend(e.target.value as "local" | "everything")}
              disabled={running}
            >
              <option value="local">{t("本地文件系统")}</option>
              <option value="everything" disabled={!everythingAvailable}>
                {t("Everything（es.exe）")}
                {everythingAvailable ? "" : t("（未检测到）")}
              </option>
            </select>
          </label>
          <label className="fieldrow">
            <span>{t("关键词")}</span>
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t("例如 报告、*.png、2024/旅行")}
              disabled={running}
            />
          </label>
          <label className="fieldrow">
            <span>{t("匹配方式")}</span>
            <select value={mode} onChange={(e) => setMode(e.target.value as "substring" | "wildcard")} disabled={running}>
              <option value="substring">{t("子串")}</option>
              <option value="wildcard">{t("通配符")}</option>
            </select>
          </label>
          <label className="fieldrow">
            <span>{t("扩展名")}</span>
            <input
              value={extsText}
              onChange={(e) => setExtsText(e.target.value)}
              placeholder={t("jpg,png,pdf（留空不限）")}
              disabled={running}
            />
          </label>
          <label className="fieldrow">
            <span>{t("最小 MB")}</span>
            <input
              type="number"
              min={0}
              value={minMB}
              onChange={(e) => setMinMB(Number(e.target.value))}
              disabled={running}
            />
          </label>
          <label className="fieldrow">
            <span>{t("最大 MB")}</span>
            <input
              type="number"
              min={0}
              value={maxMB}
              onChange={(e) => setMaxMB(Number(e.target.value))}
              placeholder={t("0 表示不限")}
              disabled={running}
            />
          </label>
          <label className="fieldrow">
            <span>{t("排除目录")}</span>
            <input
              value={excludesText}
              onChange={(e) => setExcludesText(e.target.value)}
              placeholder={t("node_modules .git（空格分隔）")}
              disabled={running}
            />
          </label>
        </div>

        <p className="hint">
          {t("检索后端：{name}", { name: backendInfo?.name ?? t("读取中…") })}
          {backendInfo?.message ? ` · ${backendInfo.message}` : ""}
          {backend === "everything" && everythingInfo?.path
            ? ` ${t("路径：{path}", { path: everythingInfo.path })}`
            : ""}
          <button className="ghost" onClick={() => void loadBackends()} disabled={running}>
            {t("重新检测")}
          </button>
        </p>
        {backend === "everything" && (
          <p className="hint">
            {t(
              "Everything 后端按 Everything 的搜索语法匹配关键词（目录范围、扩展名、大小、隐藏/系统与排除目录仍按本页条件过滤）；统计里的“候选”是 es.exe 返回的条数，不是遍历过的文件数。",
            )}
          </p>
        )}

        <label className="checkrow">
          <input type="checkbox" checked={includeHidden} onChange={(e) => setIncludeHidden(e.target.checked)} disabled={running} />
          {t("包含隐藏 / 系统文件（不勾选 = 跳过）")}
        </label>

        <div className="actionsrow">
          <button className="primary" onClick={start} disabled={running}>
            {running ? t("搜索中…") : t("开始搜索")}
          </button>
          {running && (
            <button className="ghost" onClick={cancel}>
              {t("取消")}
            </button>
          )}
          {state?.done && (
            <span className="hint">
              {activeBackend === "everything"
                ? t("Everything 返回 {visited} 条候选，命中 {matched} 条", {
                    visited: state.visited,
                    matched: state.matched,
                  })
                : t("遍历 {visited} 个文件 / {dirs} 个目录，命中 {matched} 条", {
                    visited: state.visited,
                    dirs: state.dirs,
                    matched: state.matched,
                  })}
              {t("，用时 {sec} 秒", { sec: Math.max(1, Math.round(state.elapsedMs / 1000)) })}
            </span>
          )}
          {running && state && (
            <span className="hint">
              {activeBackend === "everything"
                ? t("已收到 {visited} 条索引候选，命中 {matched} 条", {
                    visited: state.visited,
                    matched: state.matched,
                  })
                : t("已遍历 {visited} 个文件，命中 {matched} 条", {
                    visited: state.visited,
                    matched: state.matched,
                  })}
            </span>
          )}
        </div>

        {state?.cancelled && <p className="hint warn">{t("已取消，下面只显示取消前已完成的结果。")}</p>}
        {state?.error && <p className="error">{state.error}</p>}
        {error && <p className="error">{error}</p>}
      </div>

      {total > 0 && (
        <div className="card fill searchresult">
          <div className="sectionhead">
            <h2>{t("结果 {n} 条", { n: total.toLocaleString("zh-CN") })}</h2>
            <div className="toolbar">
              {truncated && <span className="badge warn">{t("已达上限 200000 条，结果被截断")}</span>}
              <button className="ghost" onClick={() => changeSort("name")}>
                {t("名称")}
                {sortMark("name")}
              </button>
              <button className="ghost" onClick={() => changeSort("size")}>
                {t("大小")}
                {sortMark("size")}
              </button>
              <button className="ghost" onClick={() => changeSort("modTime")}>
                {t("修改时间")}
                {sortMark("modTime")}
              </button>
              <button className="ghost" onClick={() => changeSort("path")}>
                {t("路径")}
                {sortMark("path")}
              </button>
            </div>
          </div>
          <div className="tablehead searchhead">
            <span>{t("名称")}</span>
            <span className="num">{t("大小")}</span>
            <span>{t("修改时间")}</span>
            <span>{t("所在目录")}</span>
            <span>{t("操作")}</span>
          </div>
          <VirtualList
            className="tablebody searchbody"
            rows={rows}
            rowHeight={ROW_HEIGHT}
            onRangeChange={ensureRange}
            rowKey={(row, i) => row?.path ?? `pending-${i}`}
            renderRow={(row, i) =>
              row ? (
                <div className="row memberrow searchrow" title={row.path}>
                  <span className="namecell">{row.name}</span>
                  <span className="num">{formatBytes(row.size)}</span>
                  <span>{formatTime(row.modTime)}</span>
                  <span className="pathcell">{row.dir}</span>
                  <span className="rowactions">
                    <button className="ghost" onClick={() => api.revealPath(row.path).catch((e) => setError(errorText(e)))}>
                      {t("定位")}
                    </button>
                    <button className="ghost" onClick={() => api.openPath(row.path).catch((e) => setError(errorText(e)))}>
                      {t("打开")}
                    </button>
                  </span>
                </div>
              ) : (
                <div className="row memberrow searchrow pending" key={`p-${i}`}>
                  <span className="hint">{t("加载中…")}</span>
                </div>
              )
            }
          />
        </div>
      )}
    </div>
  );
}
