// 浏览器预览用的合成后端。仅在 window.go 不存在时启用；
// 界面顶部会显示"浏览器预览（合成数据）"横幅，不会被当成真实扫描结果。

import type {
  Counter,
  DeleteMode,
  ExecuteState,
  GroupView,
  GroupsPage,
  LanzouItem,
  LanzouListing,
  LanzouOptions,
  LanzouPlatform,
  LanzouPrepareResult,
  DedupUndoInfo,
  DedupUndoResult,
  ExportFormat,
  ExportResult,
  LanzouStatus,
  LanzouSummary,
  LanzouUploadState,
  LicenseState,
  OrganizeRequest,
  OrganizeMeta,
  OrganizePreset,
  OrganizePresetsResult,
  OrganizeRulesExport,
  OrganizeRulesImport,
  OrganizeSettings,
  OrganizeState,
  OrganizeStepView,
  OrganizeSuspect,
  OrganizeTestRequest,
  OrganizeTestResult,
  OrganizeUndoInfo,
  OrganizeUndoResult,
  PlanPreview,
  ScanRequest,
  ScanState,
  ScheduleRunState,
  SearchMatch,
  SearchRequest,
  SearchResultPage,
  SearchState,
  SettingsResult,
  SkippedPage,
  UISettings,
  UpdateCheckView,
  UpdateInstallView,
} from "./types";

const SAMPLE_ROOT = "D:\\示例素材\\照片与视频";
const MB = 1024 * 1024;

let seq = 0;
let sessionId = "";
let startedAt = 0;
let cancelled = false;
let groups: GroupView[] = [];
let execState: ExecuteState = emptyExec();
let execTimer: number | undefined;

function emptyExec(): ExecuteState {
  return { running: false, done: false, total: 0, applied: 0, failed: 0, skipped: 0, rolledBack: 0, reclaimed: 0 };
}

const NAMES = [
  "海边日落.jpg",
  "山顶合影.png",
  "会议纪要.txt",
  "产品报价单.pdf",
  "项目复盘-终稿.docx",
  "城市夜景-调色版.jpg",
  "现场录音.mp3",
  "年会视频.mp4",
];

const FOLDERS = ["2024\\旅行\\青岛", "2024\\旅行\\厦门", "工作\\2025 规划", "客户资料\\华东区", "手机备份\\DCIM"];

interface MockFile {
  path: string;
  name: string;
  size: number;
  modTime: string;
}

// 搜索页的合成文件表：覆盖多种扩展名、大小与目录，便于预览过滤与排序。
const MOCK_FILES: MockFile[] = (() => {
  const out: MockFile[] = [];
  for (let i = 0; i < 96; i++) {
    const sample = NAMES[i % NAMES.length];
    const ext = sample.slice(sample.lastIndexOf("."));
    const base = sample.slice(0, sample.lastIndexOf("."));
    const suffix = i < NAMES.length ? "" : `-${i}`;
    const folder = FOLDERS[i % FOLDERS.length];
    out.push({
      path: `${SAMPLE_ROOT}\\${folder}\\${base}${suffix}${ext}`,
      name: `${base}${suffix}${ext}`,
      size: (1 + (i % 40)) * 1024 * 512,
      modTime: new Date(Date.UTC(2024, i % 12, (i % 27) + 1, 9, 0)).toISOString(),
    });
  }
  return out;
})();

let searchSeq = 0;
let search: { id: string; rows: SearchMatch[]; done: boolean; truncated: boolean; startedAt: number } | null = null;

function makeGroups(count: number, root = SAMPLE_ROOT): GroupView[] {
  const out: GroupView[] = [];
  for (let i = 0; i < count; i++) {
    const name = NAMES[i % NAMES.length];
    const size = (2 + (i % 17)) * 1024 * 512;
    const memberCount = 2 + (i % 3);
    const folder = FOLDERS[i % FOLDERS.length];
    const members = [];
    for (let k = 0; k < memberCount; k++) {
      const suffix = k === 0 ? "" : ` (副本 ${k})`;
      const ext = name.slice(name.lastIndexOf("."));
      const baseName = name.slice(0, name.lastIndexOf("."));
      members.push({
        path: `${root}\\${folder}\\${baseName}${suffix}${ext}`,
        name: `${baseName}${suffix}${ext}`,
        size,
        modTime: new Date(Date.UTC(2024, (i + k) % 12, ((i * 3 + k) % 27) + 1, 8, 30)).toISOString(),
        keep: k === 0,
        deletable: k !== 0,
      });
    }
    const keep = members[0].path;
    out.push({
      id: `exact:${(i + 1).toString(16).padStart(16, "0")}`,
      kind: i % 23 === 0 ? "image" : i % 31 === 0 ? "audio" : "exact",
      keep,
      reclaimable: size * (memberCount - 1),
      members,
    });
  }
  return out;
}

function statsOf(gs: GroupView[]) {
  let duplicates = 0;
  let reclaimable = 0;
  for (const g of gs) {
    duplicates += g.members.length - 1;
    reclaimable += g.reclaimable;
  }
  return { duplicates, reclaimable };
}

function doneState(id: string, gs: GroupView[], elapsed: number): ScanState {
  const s = statsOf(gs);
  return {
    id,
    phase: "done",
    files: gs.reduce((n, g) => n + g.members.length, 0) + 64,
    bytes: gs.reduce((n, g) => n + g.members.length * g.members[0].size, 0),
    hashTargets: gs.reduce((n, g) => n + g.members.length, 0),
    hashDone: gs.reduce((n, g) => n + g.members.length, 0),
    hashBytes: gs.reduce((n, g) => n + g.members.length * g.members[0].size, 0),
    hashTotalBytes: gs.reduce((n, g) => n + g.members.length * g.members[0].size, 0),
    enumerationDone: true,
    groups: gs.length,
    duplicateFiles: s.duplicates,
    reclaimable: s.reclaimable,
    skipped: 3,
    elapsedMs: elapsed,
  };
}

function validateSelection(gs: GroupView[], deletePaths: string[]): void {
  const chosen = new Set(deletePaths);
  const known = new Set<string>();
  for (const g of gs) {
    for (const m of g.members) known.add(m.path);
    if (g.members.length > 0 && g.members.every((m) => chosen.has(m.path))) {
      throw new Error(`勾选会删除整组文件：${g.id} 共 ${g.members.length} 个成员，至少保留一个`);
    }
  }
  for (const p of chosen) {
    if (!known.has(p)) throw new Error(`勾选了不属于任何重复组的文件：${p}`);
  }
}

function previewOf(id: string, gs: GroupView[], mode: DeleteMode, deletePaths?: string[]): PlanPreview {
  if (deletePaths !== undefined) validateSelection(gs, deletePaths);
  const chosen = deletePaths === undefined ? undefined : new Set(deletePaths);
  const kinds: Record<string, number> = {};
  let total = 0;
  const samples = [] as PlanPreview["samples"];
  for (const g of gs) {
    for (const m of g.members) {
      const selected = chosen ? chosen.has(m.path) : m.deletable;
      if (!selected) continue;
      const kind = mode === "permanent" ? "delete_permanent" : "delete_trash";
      total++;
      kinds[kind] = (kinds[kind] || 0) + 1;
      if (samples.length < 200) {
        samples.push({ seq: total, kind, src: m.path, reason: `${g.kind} duplicate; keep=${g.keep}`, size: m.size });
      }
    }
  }
  return {
    requestId: id,
    deleteMode: mode,
    total,
    groups: gs.length,
    reclaimableBytes: gs.reduce((n, g) => n + g.reclaimable, 0),
    kindCounts: kinds,
    samples,
  };
}

const SETTINGS_KEY = "filededup.ui-settings";

function zeroCounter(): Counter {
  return {
    renamed: 0, specialDeleted: 0, moved: 0, conflictSkip: 0, readonly: 0, organizeSkip: 0,
    contentDeleted: 0, imageDeleted: 0, audioDeleted: 0, emptyDirs: 0, excluded: 0,
    hiddenSkipped: 0, suspectsKept: 0, hardlinkMerged: 0,
  };
}

function mockDefaultSettings(): UISettings {
  return {
    schemaVersion: 1,
    lang: "zh",
    scan: {
      root: SAMPLE_ROOT,
      keepPolicy: "pathShortest",
      deleteMode: "trash",
      similarImage: false,
      imageThreshold: 8,
      similarAudio: true,
      audioTolerance: 2,
      minSize: 0,
      includeHidden: false,
      excludes: [],
    },
    organize: {
      root: SAMPLE_ROOT,
      target: "整理结果",
      usePreviewPlan: false,
      steps: {
        renamePrefix: true, renameSuffix: true, specialDelete: false, readonly: true,
        contentDedup: false, imageDedup: false, audioDedup: false, organize: true, emptyDirs: false,
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
    },
    update: {
      manifestUrl: "",
      checkMode: "auto",
      intervalHours: 18,
      skippedVersion: "",
    },
    lanzou: {
      workers: 3,
      intervalMs: 1500,
      forceReupload: false,
      disclaimerAccepted: false,
      lastLocalDir: "",
      lastFolderId: "0",
      lastFolderName: "",
    },
    ai: { enabled: false, endpoint: "", model: "", timeoutSeconds: 45, keyHint: "" },
    window: { closeAction: "exit", trayEnabled: true, theme: "system" },
    schedule: {
      enabled: false,
      mode: "interval",
      intervalMinutes: 60,
      dailyTime: "09:00",
      action: "scan",
      root: "",
    },
    logs: { retentionDays: 14 },
  };
}

function readSettings(): UISettings {
  const def = mockDefaultSettings();
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return def;
    const parsed = JSON.parse(raw) as Partial<UISettings>;
    return {
      schemaVersion: parsed.schemaVersion ?? def.schemaVersion,
      scan: { ...def.scan, ...(parsed.scan ?? {}) },
      organize: {
        ...def.organize,
        ...(parsed.organize ?? {}),
        steps: { ...def.organize.steps, ...(parsed.organize?.steps ?? {}) },
      },
      update: { ...def.update, ...(parsed.update ?? {}) },
      lanzou: { ...def.lanzou, ...(parsed.lanzou ?? {}) },
      ai: { ...def.ai, ...(parsed.ai ?? {}) },
      window: { ...def.window, ...(parsed.window ?? {}) },
      schedule: { ...def.schedule, ...(parsed.schedule ?? {}) },
      logs: { ...def.logs, ...(parsed.logs ?? {}) },
      lang: parsed.lang === "en" ? "en" : def.lang,
    };
  } catch {
    return def;
  }
}

// 两套顺序与 internal/engine/organize/types.go 的 standaloneOrder / previewOrder 一致。
const STANDALONE_ORDER = [
  "rename_prefix", "rename_suffix", "special_delete", "readonly",
  "content_dedup", "image_dedup", "audio_dedup", "organize", "empty_dirs",
];
const PREVIEW_ORDER = [
  "content_dedup", "image_dedup", "audio_dedup",
  "rename_prefix", "rename_suffix", "special_delete", "readonly", "organize", "empty_dirs",
];

const STEP_TITLES: Record<string, string> = {
  rename_prefix: "第一步：去除文件名前缀序列号（含中间序列号）",
  rename_suffix: "第二步：去除文件名后缀序列号",
  special_delete: "第三步：删除特殊文件（确定垃圾自动删；存疑项需人工确认，未确认一律保留）",
  readonly: "第四步：去除所有文件只读属性",
  content_dedup: "内容去重",
  image_dedup: "相似图片去重",
  audio_dedup: "音频相似去重（实验性）",
  organize: "第五步：文件整理",
  empty_dirs: "清理空文件夹",
};

const STEP_ACTIONS: Record<string, number> = {
  rename_prefix: 12, rename_suffix: 8, special_delete: 3, readonly: 15,
  content_dedup: 26, image_dedup: 9, audio_dedup: 2, organize: 84, empty_dirs: 6,
};

const STEP_COUNTER: Record<string, Partial<Counter>> = {
  rename_prefix: { renamed: 12 },
  rename_suffix: { renamed: 8 },
  special_delete: { specialDeleted: 3 },
  readonly: { readonly: 15 },
  content_dedup: { contentDeleted: 26 },
  image_dedup: { imageDeleted: 9 },
  audio_dedup: { audioDeleted: 2 },
  organize: { moved: 84, organizeSkip: 3, conflictSkip: 2 },
  empty_dirs: { emptyDirs: 6 },
};

const STEP_KIND: Record<string, string> = {
  rename_prefix: "rename", rename_suffix: "rename", special_delete: "delete",
  readonly: "clear_readonly", content_dedup: "delete", image_dedup: "delete",
  audio_dedup: "delete", organize: "move", empty_dirs: "remove_dir",
};

let orgSeq = 0;
let orgJob: { id: string; startedAt: number; req: OrganizeRequest } | null = null;

// 浏览器预览用固定存疑项，路径与 key（小写）都保持稳定，便于确认/驳回演示。
const MOCK_SUSPECTS: OrganizeSuspect[] = [
  {
    key: "2024\\旅行\\a8f3k2x9.jpg",
    path: "2024\\旅行\\a8f3k2x9.jpg",
    reason: "疑似随机字符（需人工确认）",
    suggestion: "RENAME",
    normalized: "a8f3k2x9",
    score: 7,
  },
  {
    key: "2024\\旅行\\q7zm2v4b.png",
    path: "2024\\旅行\\q7zm2v4b.png",
    reason: "疑似随机字符（需人工确认）",
    suggestion: "RENAME",
    normalized: "q7zm2v4b",
    score: 6,
  },
  {
    key: "下载\\tmp-9x2k.dat",
    path: "下载\\tmp-9x2k.dat",
    reason: "疑似临时文件（需人工确认）",
    suggestion: "KEEP",
    normalized: "tmp-9x2k",
    score: 5,
  },
];

let mockUndo: { id: string; root: string; at: string; records: number } | null = null;

function stepEnabled(sid: string, s: OrganizeRequest["steps"]): boolean {
  switch (sid) {
    case "rename_prefix": return s.renamePrefix;
    case "rename_suffix": return s.renameSuffix;
    case "special_delete": return s.specialDelete;
    case "readonly": return s.readonly;
    case "content_dedup": return s.contentDedup;
    case "image_dedup": return s.imageDedup;
    case "audio_dedup": return s.audioDedup;
    case "organize": return s.organize;
    case "empty_dirs": return s.emptyDirs;
    default: return false;
  }
}

function emptyOrgState(id: string): OrganizeState {
  return {
    id, phase: "running", stage: "枚举目录", dryRun: true, order: "standalone",
    usePreviewPlan: false, targetDir: "", targetExternal: false, moveMode: "", elapsedMs: 0,
    steps: [], counters: zeroCounter(),
    exec: { total: 0, applied: 0, failed: 0, skipped: 0, errors: [] }, actionTotal: 0, actions: [], suspects: [],
  };
}

function buildOrgState(id: string, req: OrganizeRequest, elapsed: number): OrganizeState {
  const order = req.usePreviewPlan ? PREVIEW_ORDER : STANDALONE_ORDER;
  if (elapsed < 700) {
    return { ...emptyOrgState(id), stage: "枚举目录并计算指纹", dryRun: req.dryRun, order: req.usePreviewPlan ? "preview" : "standalone", usePreviewPlan: req.usePreviewPlan, elapsedMs: elapsed };
  }
  if (elapsed < 1500) {
    return { ...emptyOrgState(id), stage: "规划动作", dryRun: req.dryRun, order: req.usePreviewPlan ? "preview" : "standalone", usePreviewPlan: req.usePreviewPlan, elapsedMs: elapsed };
  }
  const steps: OrganizeStepView[] = order.map((sid, i) => {
    const on = stepEnabled(sid, req.steps);
    return {
      id: sid,
      title: STEP_TITLES[sid],
      order: i + 1,
      enabled: on,
      skipped: !on,
      skipReason: on ? undefined : "未启用该步骤",
      actionNum: on ? STEP_ACTIONS[sid] ?? 0 : 0,
      counters: { ...zeroCounter(), ...(on ? STEP_COUNTER[sid] ?? {} : {}) },
    };
  });
  const counters = zeroCounter();
  for (const s of steps) {
    counters.renamed += s.counters.renamed;
    counters.specialDeleted += s.counters.specialDeleted;
    counters.moved += s.counters.moved;
    counters.conflictSkip += s.counters.conflictSkip;
    counters.readonly += s.counters.readonly;
    counters.organizeSkip += s.counters.organizeSkip;
    counters.contentDeleted += s.counters.contentDeleted;
    counters.imageDeleted += s.counters.imageDeleted;
    counters.audioDeleted += s.counters.audioDeleted;
    counters.emptyDirs += s.counters.emptyDirs;
  }
  const actions = [] as OrganizeState["actions"];
  let seqNo = 0;
  for (const s of steps) {
    if (!s.enabled) continue;
    for (let k = 0; k < Math.min(s.actionNum, 6); k++) {
      seqNo++;
      const kind = STEP_KIND[s.id];
      const name = `${SAMPLE_ROOT}\\2024\\旅行\\样例文件-${seqNo}.jpg`;
      actions.push({
        kind,
        step: s.title,
        from: name,
        to: kind === "move" ? `${SAMPLE_ROOT}\\整理结果\\图片\\样例文件-${seqNo}.jpg` : undefined,
        reason: kind === "delete" ? "重复内容，保留路径最短的一份" : undefined,
      });
    }
  }
  const total = steps.reduce((n, s) => n + s.actionNum, 0);
  const specialOn = stepEnabled("special_delete", req.steps);
  const confirmed = new Set(req.confirmedSuspects ?? []);
  const suspects = specialOn ? MOCK_SUSPECTS.filter((s) => !confirmed.has(s.key)) : [];
  if (specialOn) {
    counters.suspectsKept += suspects.length;
    counters.specialDeleted += MOCK_SUSPECTS.length - suspects.length;
  }
  const planOnly = Boolean(req.planOnly);
  const exec = planOnly
    ? { total, applied: 0, failed: 0, skipped: 0, errors: [] }
    : req.dryRun
      ? { total, applied: 0, failed: 0, skipped: total, errors: [] }
      : { total, applied: total, failed: 0, skipped: 0, errors: [] };
  const targetExternal = /^[A-Za-z]:[\\/]/.test(req.target) || req.target.startsWith("\\\\");
  return {
    id,
    phase: "done",
    stage: "",
    dryRun: req.dryRun,
    order: req.usePreviewPlan ? "preview" : "standalone",
    usePreviewPlan: req.usePreviewPlan,
    targetDir: req.target || "",
    targetExternal: targetExternal && req.target !== req.root,
    moveMode: req.target && req.steps.organize ? "move" : "",
    elapsedMs: elapsed,
    steps,
    counters,
    exec,
    actionTotal: total,
    actions,
    suspects,
  };
}

// ---------- 蓝奏云：浏览器预览用合成登录态、目录与上传进度 ----------
// 这里不联网、不写本机凭据；登录成功后的真实列表与上传仍需在桌面版用账号联调。

const LANZOU_ROOT_ID = "0";
const LANZOU_CREDENTIAL_PATH = "浏览器预览：不写本机凭据（合成数据）";

let lanzouSeq = 0;

const lanzouStatuses: Record<LanzouPlatform, LanzouStatus> = {
  lanzou: {
    loggedIn: false,
    account: "",
    platform: "lanzou",
    source: "none",
    credentialPath: LANZOU_CREDENTIAL_PATH,
    supportsDpapi: false,
  },
  youxiang: {
    loggedIn: false,
    account: "",
    platform: "youxiang",
    source: "none",
    credentialPath: LANZOU_CREDENTIAL_PATH,
    supportsDpapi: false,
  },
};

function lanzouLoggedStatus(platform: LanzouPlatform, source: LanzouStatus["source"], message: string): LanzouStatus {
  return {
    loggedIn: true,
    account: "****1234",
    platform,
    source,
    credentialPath: LANZOU_CREDENTIAL_PATH,
    supportsDpapi: false,
    message,
    savedAt: new Date().toISOString(),
  };
}

// 固定的假目录树：id 与名称都稳定，便于演示下钻与面包屑。
const LANZOU_LISTINGS: Record<string, LanzouListing> = {
  "0": {
    folderId: "0",
    folders: [
      { id: "f-1001", name: "照片备份" },
      { id: "f-1002", name: "工作文档" },
    ],
    files: [
      { id: "mock-file-101", name: "说明.txt", size: 1229, sizeText: "1.2 KB", time: "2026-10-02 09:12" },
      { id: "mock-file-102", name: "旧版备份.zip", size: 8 * MB, sizeText: "8.0 MB", time: "2026-08-18 21:40" },
    ],
  },
  "f-1001": {
    folderId: "f-1001",
    folders: [{ id: "f-2001", name: "2024 旅行" }],
    files: [
      { id: "mock-file-201", name: "海边日落.jpg", size: Math.round(2.4 * MB), sizeText: "2.4 MB", time: "2026-05-01 18:02" },
      { id: "mock-file-202", name: "山顶合影.png", size: Math.round(1.8 * MB), sizeText: "1.8 MB", time: "2026-05-02 10:31" },
      { id: "mock-file-203", name: "现场录音.mp3", size: Math.round(5.2 * MB), sizeText: "5.2 MB", time: "2026-06-11 20:15" },
    ],
  },
  "f-1002": {
    folderId: "f-1002",
    folders: [],
    files: [
      { id: "mock-file-301", name: "产品报价单.pdf", size: Math.round(480 * 1024), sizeText: "480 KB", time: "2026-09-09 14:25" },
      { id: "mock-file-302", name: "会议纪要.txt", size: 3174, sizeText: "3.1 KB", time: "2026-09-10 09:05" },
    ],
  },
  "f-2001": {
    folderId: "f-2001",
    folders: [],
    files: [{ id: "mock-file-401", name: "城市夜景.jpg", size: Math.round(3.6 * MB), sizeText: "3.6 MB", time: "2026-04-20 22:18" }],
  },
};

interface MockLanzouBatch {
  id: string;
  platform: LanzouPlatform;
  localDir: string;
  folderId: string;
  folderName: string;
  workers: number;
  intervalMs: number;
  baseItems: LanzouItem[];
  startedAt: number;
  canceled: boolean;
  cancelAt: number;
}

let lanzouBatch: MockLanzouBatch | null = null;

function lanzouSummaryOf(items: LanzouItem[]): LanzouSummary {
  const s: LanzouSummary = {
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

// 与后端一致：云端重复的本地文件移动到上传目录旁边的「网盘重复文件」。
function lanzouDuplicateTarget(localDir: string, name: string): string {
  const idx = Math.max(localDir.lastIndexOf("\\"), localDir.lastIndexOf("/"));
  const parent = idx > 0 ? localDir.slice(0, idx) : localDir;
  return `${parent}\\网盘重复文件\\${name}`;
}

function lanzouStamp(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`;
}

function lanzouStateOf(b: MockLanzouBatch): LanzouUploadState {
  const items = b.baseItems.map((it) => ({ ...it }));
  const pending: { item: LanzouItem; index: number }[] = [];
  items.forEach((it, index) => {
    if (it.kind === "pending") pending.push({ item: it, index });
  });

  const started = b.startedAt > 0;
  const now = Date.now();
  const elapsed = started ? (b.canceled ? b.cancelAt : now) - b.startedAt : 0;
  const slot = pending.length > 0 ? Math.max(700, Math.round(5200 / pending.length)) : 0;
  let bytesTotal = 0;
  let bytesSent = 0;
  const active: string[] = [];

  if (started) {
    pending.forEach(({ item, index }, order) => {
      const size = Math.max(0, item.size);
      bytesTotal += size;
      const from = order * slot;
      const to = from + slot;
      if (elapsed >= to) {
        items[index] = {
          ...item,
          kind: order === 0 ? "instant" : "success",
          remoteId: `mock-remote-${index + 1}`,
        };
        bytesSent += size;
      } else if (elapsed > from) {
        bytesSent += Math.round((size * (elapsed - from)) / slot);
        active.push(item.name);
      } else if (b.canceled) {
        items[index] = { ...item, kind: "canceled" };
      }
    });
    if (b.canceled) {
      items.forEach((it, i) => {
        if (it.kind === "pending") items[i] = { ...it, kind: "canceled" };
      });
      active.length = 0;
    }
    items.forEach((it, i) => {
      if (it.kind === "duplicate") {
        items[i] = { ...it, movedTo: lanzouDuplicateTarget(b.localDir, it.name) };
      }
    });
  }

  const cancelling = b.canceled && now - b.cancelAt < 700;
  const allReached = pending.every((_, order) => elapsed >= (order + 1) * slot);
  const ended = started && !cancelling && (b.canceled || allReached);
  const phase: LanzouUploadState["phase"] = !started
    ? "prepared"
    : cancelling
      ? "cancelling"
      : ended
        ? "done"
        : "running";

  const stamp = started ? lanzouStamp(b.startedAt) : "";
  const reportJson = ended ? `${b.localDir}\\upload-report-${stamp}.json` : undefined;
  const reportCsv = ended ? `${b.localDir}\\upload-report-${stamp}.csv` : undefined;

  return {
    batchId: b.id,
    platform: b.platform,
    phase,
    folderId: b.folderId,
    folderName: b.folderName,
    localDir: b.localDir,
    workers: b.workers,
    intervalMs: b.intervalMs,
    total: items.length,
    done: items.filter((it) => it.kind !== "pending").length,
    bytesSent,
    bytesTotal,
    active,
    summary: lanzouSummaryOf(items),
    items,
    reportJson,
    reportCsv,
    startedAt: started ? new Date(b.startedAt).toISOString() : undefined,
    finishedAt: ended ? new Date(now).toISOString() : undefined,
  };
}

const PRESETS_KEY = "filededup.mock.organizePresets";

// 浏览器预览的预设只存在 localStorage；桌面版的预设文件由 Go 侧管理。
function readMockPresets(): OrganizePreset[] {
  try {
    const raw = localStorage.getItem(PRESETS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as OrganizePreset[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function writeMockPresets(list: OrganizePreset[]): void {
  try {
    localStorage.setItem(PRESETS_KEY, JSON.stringify(list));
  } catch {
    // 浏览器禁用存储时仍返回本次结果，刷新页面后预设会丢失。
  }
}

export const mock = {
  pickDirectory(): string {
    return SAMPLE_ROOT;
  },
  startScan(req: ScanRequest): string {
    seq++;
    sessionId = `mock-${seq}`;
    startedAt = Date.now();
    cancelled = false;
    groups = makeGroups(180, req.root || SAMPLE_ROOT);
    execState = emptyExec();
    if (execTimer) window.clearTimeout(execTimer);
    return sessionId;
  },
  getScanState(id: string): ScanState {
    const elapsed = Date.now() - startedAt;
    if (cancelled) {
      return { ...doneState(id, groups, elapsed), phase: "cancelled" };
    }
    const totalMembers = groups.reduce((n, g) => n + g.members.length, 0);
    const totalBytes = groups.reduce((n, g) => n + g.members.length * g.members[0].size, 0);
    if (elapsed < 900) {
      const ratio = Math.min(1, elapsed / 900);
      return {
        id,
        phase: "running",
        files: Math.floor(18000 * ratio),
        bytes: Math.floor(totalBytes * ratio),
        hashTargets: 0,
        hashDone: 0,
        hashBytes: 0,
        hashTotalBytes: 0,
        enumerationDone: false,
        groups: 0,
        duplicateFiles: 0,
        reclaimable: 0,
        skipped: 0,
        elapsedMs: elapsed,
      };
    }
    if (elapsed < 2100) {
      const ratio = Math.min(1, (elapsed - 900) / 1200);
      return {
        id,
        phase: "running",
        files: 18064,
        bytes: totalBytes,
        hashTargets: totalMembers,
        hashDone: Math.floor(totalMembers * ratio),
        hashBytes: Math.floor(totalBytes * ratio),
        hashTotalBytes: totalBytes,
        enumerationDone: true,
        groups: 0,
        duplicateFiles: 0,
        reclaimable: 0,
        skipped: 0,
        elapsedMs: elapsed,
      };
    }
    return doneState(id, groups, elapsed);
  },
  cancelScan(_id: string): void {
    cancelled = true;
  },
  getGroups(_id: string, offset: number, limit: number): GroupsPage {
    const items = groups.slice(offset, offset + limit);
    return { total: groups.length, offset, items };
  },
  getSkipped(_id: string, offset: number, limit: number): SkippedPage {
    const all = [
      { path: `${SAMPLE_ROOT}\\工作\\2025 规划\\~$草案.docx`, code: "permission_denied", message: "没有读取权限" },
      { path: `${SAMPLE_ROOT}\\手机备份\\DCIM\\损坏.jpg`, code: "read_failed", message: "读取失败（文件可能已损坏）" },
      { path: `${SAMPLE_ROOT}\\客户资料\\华东区\\锁定.pdf`, code: "locked", message: "文件被其他程序占用" },
    ];
    return { total: all.length, offset, items: all.slice(offset, offset + limit) };
  },
  previewPlan(id: string, mode: DeleteMode, deletePaths: string[]): PlanPreview {
    return previewOf(id, groups, mode, deletePaths);
  },
  startExecute(id: string, mode: DeleteMode, deletePaths: string[], confirmed: boolean): void {
    if (!confirmed) throw new Error("未确认，拒绝执行");
    const p = previewOf(id, groups, mode, deletePaths);
    execState = { ...emptyExec(), running: true, total: p.total };
    if (execTimer) window.clearTimeout(execTimer);
    execTimer = window.setTimeout(() => {
      execState = {
        running: false,
        done: true,
        total: p.total,
        applied: p.total,
        failed: 0,
        skipped: 0,
        rolledBack: 0,
        reclaimed: p.reclaimableBytes,
        finishedAt: new Date().toISOString(),
      };
    }, 1200);
  },
  getExecuteState(): ExecuteState {
    return execState;
  },
  demoGroups(count: number): GroupView[] {
    return makeGroups(count);
  },
  demoScanState(gs: GroupView[]): ScanState {
    return doneState("demo", gs, 2530);
  },
  demoPreview(gs: GroupView[], mode: DeleteMode): PlanPreview {
    return previewOf("demo", gs, mode);
  },
  demoExec(gs: GroupView[], mode: DeleteMode): ExecuteState {
    const p = previewOf("demo", gs, mode);
    return {
      running: false,
      done: true,
      total: p.total,
      applied: p.total,
      failed: 0,
      skipped: 0,
      rolledBack: 0,
      reclaimed: p.reclaimableBytes,
      finishedAt: new Date().toISOString(),
    };
  },

  // ---- 设置持久化：浏览器里用 localStorage 模拟用户配置目录 ----
  getSettings(): SettingsResult {
    const s = readSettings();
    return {
      settings: s,
      path: "localStorage:" + SETTINGS_KEY,
      warning: undefined,
      keyStoragePath: "（浏览器预览不保存密钥）",
      keyProtectionOk: false,
    };
  },
  saveSettings(s: UISettings): void {
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  },
  saveAIKey(_key: string): void {
    // 浏览器预览不保存 AI 密钥。
  },
  getScheduleState(): ScheduleRunState {
    return {
      startedAt: "",
      finishedAt: "",
      status: "skipped",
      message: "浏览器预览不运行定时任务",
      root: "",
      files: 0,
      groups: 0,
      reclaimable: 0,
      elapsedMs: 0,
    };
  },
  checkForUpdate(manual: boolean): UpdateCheckView {
    const s = readSettings();
    const currentVersion = "0.8.0";
    if (!s.update.manifestUrl.trim()) {
      return {
        status: "disabled",
        message: "未配置更新源。填写清单地址后才会检查更新。",
        currentVersion,
      };
    }
    if (!manual && s.update.checkMode !== "auto") {
      return { status: "manual", message: "当前为手动检查模式。", currentVersion };
    }
    const latestVersion = "0.9.0";
    if (s.update.skippedVersion === latestVersion) {
      return {
        status: "skipped",
        message: `已跳过版本 ${latestVersion}。`,
        currentVersion,
        latestVersion,
        skippedVersion: s.update.skippedVersion,
      };
    }
    return {
      status: "available",
      message: `发现新版本 ${latestVersion}（浏览器预览为合成结果）。`,
      currentVersion,
      latestVersion,
      notes: "浏览器预览不会真的下载或替换程序。",
      assetName: "fdedup-ui.exe",
      assetSize: 12932608,
      sourceUrl: s.update.manifestUrl,
      checkedAt: new Date().toISOString(),
    };
  },
  installUpdate(): UpdateInstallView {
    throw new Error("浏览器预览不能安装更新，请在桌面版中使用。");
  },
  skipUpdateVersion(version: string): void {
    const s = readSettings();
    s.update.skippedVersion = version;
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  },
  clearSkippedUpdateVersion(): void {
    const s = readSettings();
    s.update.skippedVersion = "";
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  },
  getOrganizeMeta(): OrganizeMeta {
    return {
      standalone: STANDALONE_ORDER.map((id) => ({ id, title: STEP_TITLES[id] })),
      preview: PREVIEW_ORDER.map((id) => ({ id, title: STEP_TITLES[id] })),
    };
  },
  startOrganize(req: OrganizeRequest): string {
    orgSeq++;
    orgJob = { id: `mock-org-${orgSeq}`, startedAt: Date.now(), req };
    if (!req.dryRun && !req.planOnly) {
      mockUndo = { id: orgJob.id, root: req.root, at: new Date().toISOString(), records: 42 };
    }
    return orgJob.id;
  },
  getOrganizeState(id: string): OrganizeState {
    if (!orgJob || orgJob.id !== id) {
      return { ...emptyOrgState(id), phase: "failed", error: "整理任务不存在" };
    }
    return buildOrgState(id, orgJob.req, Date.now() - orgJob.startedAt);
  },
  exportOrganizeRules(rules: OrganizeSettings): OrganizeRulesExport {
    const payload = {
      schemaVersion: 1,
      kind: "filededup.organize.rules",
      rules: { ...rules, root: "" },
    };
    const raw = JSON.stringify(payload, null, 2) + "\n";
    try {
      localStorage.setItem("filededup.mock.organizeRules", raw);
    } catch {
      // 浏览器禁用存储时仍然返回成功，规则内容已随返回值交给调用方。
    }
    return { canceled: false, path: "浏览器预览：内存中的规则", bytes: raw.length };
  },
  importOrganizeRules(): OrganizeRulesImport {
    let raw: string | null = null;
    try {
      raw = localStorage.getItem("filededup.mock.organizeRules");
    } catch {
      raw = null;
    }
    if (!raw) {
      throw new Error("浏览器预览还没有可导入的规则；请先在预览里导出一次。");
    }
    const parsed = JSON.parse(raw) as { schemaVersion: number; kind: string; rules: OrganizeSettings };
    if (parsed.schemaVersion !== 1 || parsed.kind !== "filededup.organize.rules") {
      throw new Error("规则文件格式或版本不受支持");
    }
    return { canceled: false, path: "浏览器预览：内存中的规则", rules: parsed.rules };
  },
  listOrganizePresets(): OrganizePresetsResult {
    return {
      path: "浏览器预览：localStorage:" + PRESETS_KEY,
      presets: readMockPresets(),
    };
  },
  saveOrganizePreset(name: string, rules: OrganizeSettings): OrganizePreset {
    const clean = name.trim();
    if (!clean) throw new Error("预设名称不能为空");
    const presets = readMockPresets();
    const now = new Date().toISOString();
    const idx = presets.findIndex((p) => p.name.toLowerCase() === clean.toLowerCase());
    const entry: OrganizePreset =
      idx >= 0
        ? { ...presets[idx], name: clean, updatedAt: now, rules: { ...rules, root: "" } }
        : {
            id: `mock-preset-${Date.now()}`,
            name: clean,
            createdAt: now,
            updatedAt: now,
            rules: { ...rules, root: "" },
          };
    if (idx >= 0) presets[idx] = entry;
    else presets.push(entry);
    writeMockPresets(presets);
    return entry;
  },
  deleteOrganizePreset(id: string): void {
    const presets = readMockPresets();
    if (!presets.some((p) => p.id === id)) throw new Error("预设不存在（可能已删除）");
    writeMockPresets(presets.filter((p) => p.id !== id));
  },
  testOrganizeRules(_req: OrganizeTestRequest): OrganizeTestResult {
    throw new Error("浏览器预览没有 Go 引擎，规则测试器只在桌面版可用。");
  },
  getOrganizeUndoInfo(): OrganizeUndoInfo {
    if (!mockUndo) return { available: false, records: 0 };
    return {
      available: true,
      id: mockUndo.id,
      root: mockUndo.root,
      startedAt: mockUndo.at,
      records: mockUndo.records,
    };
  },
  undoLastOrganize(): OrganizeUndoResult {
    if (!mockUndo) throw new Error("没有可撤销的整理记录");
    const result: OrganizeUndoResult = {
      id: mockUndo.id,
      root: mockUndo.root,
      total: mockUndo.records,
      restored: mockUndo.records - 4,
      failed: 0,
      skipped: 0,
      unrestorable: 4,
      errors: ["浏览器预览：4 条删除动作无法还原（合成数据）"],
    };
    mockUndo = null;
    return result;
  },
  // 浏览器预览没有真实回收站，按“没有可撤销记录”展示；撤销操作如实拒绝。
  getDedupUndoInfo(): DedupUndoInfo {
    return { available: false, trash: 0, permanent: 0, items: [], truncated: false };
  },
  undoLastDedup(): DedupUndoResult {
    throw new Error("浏览器预览不支持从回收站撤销，请在桌面版中使用");
  },
  // 浏览器预览没有本地写盘能力，导出如实拒绝，避免伪造“已导出”。
  exportResults(_format: ExportFormat): ExportResult {
    throw new Error("浏览器预览不支持导出到本地文件，请在桌面版中使用");
  },
  // ---- 授权：浏览器预览没有真实授权，按“试用中”展示，导入操作如实拒绝 ----
  getLicenseState(): LicenseState {
    return {
      status: "trial",
      licensed: false,
      source: "trial",
      edition: "试用版",
      editionCode: "trial",
      forever: false,
      machineCode: "DEMO-0000-0000-0000",
      features: ["scan", "apply", "organize", "search", "cloud", "cloud-dedup", "ai", "schedule"],
      featureList: [
        { key: "scan", name: "重复文件扫描", unlocked: true, edition: "标准版" },
        { key: "apply", name: "删除执行", unlocked: true, edition: "标准版" },
        { key: "organize", name: "九步整理", unlocked: true, edition: "标准版" },
        { key: "search", name: "本地搜索", unlocked: true, edition: "标准版" },
        { key: "cloud", name: "网盘上传", unlocked: true, edition: "专业版" },
        { key: "cloud-dedup", name: "网盘查重", unlocked: true, edition: "专业版" },
        { key: "ai", name: "AI 分析建议", unlocked: true, edition: "专业版" },
        { key: "schedule", name: "定时任务", unlocked: true, edition: "专业版" },
      ],
      canApply: true,
      canOrganize: true,
      trialActive: true,
      trialStart: new Date().toISOString().slice(0, 10),
      trialEnd: new Date(Date.now() + 3 * 86400000).toISOString().slice(0, 10),
      trialDaysLeft: 3,
      licensePath: "浏览器预览无许可证文件",
      limits: "浏览器预览：按试用中展示，导入许可证需在桌面版操作。",
      warning: "浏览器预览使用合成数据，授权状态不是真实结果。",
    };
  },
  importLicenseFile(): LicenseState {
    throw new Error("浏览器预览不支持导入许可证，请在桌面版操作");
  },
  importLicenseText(_text: string): LicenseState {
    throw new Error("浏览器预览不支持导入许可证，请在桌面版操作");
  },

  // ---- 蓝奏云/蓝奏优享（合成数据）：登录态只存在内存里，不写本机凭据 ----
  lanzouStatus(platform: LanzouPlatform): LanzouStatus {
    return { ...lanzouStatuses[platform] };
  },
  lanzouLogin(platform: LanzouPlatform, username: string, password: string): LanzouStatus {
    if (!username.trim() || !password) throw new Error("请输入账号和密码");
    lanzouStatuses[platform] = lanzouLoggedStatus(platform, "login", "浏览器预览为合成数据，未连接网盘。");
    return { ...lanzouStatuses[platform] };
  },
  lanzouUseCookie(platform: LanzouPlatform, raw: string): LanzouStatus {
    if (platform === "youxiang") throw new Error("蓝奏优享使用 appToken 登录，不支持粘贴 Cookie");
    if (!raw.trim()) throw new Error("粘贴的 Cookie 为空");
    lanzouStatuses[platform] = lanzouLoggedStatus(platform, "cookie", "浏览器预览为合成数据：Cookie 未校验、未保存。");
    return { ...lanzouStatuses[platform] };
  },
  lanzouImportCSharp(platform: LanzouPlatform): LanzouStatus {
    if (platform === "youxiang") throw new Error("蓝奏优享暂不支持从 C# 版导入登录态");
    lanzouStatuses[platform] = lanzouLoggedStatus(platform, "import", "浏览器预览为合成数据：C# 版登录态未真正读取。");
    return { ...lanzouStatuses[platform] };
  },
  lanzouLogout(platform: LanzouPlatform): LanzouStatus {
    lanzouStatuses[platform] = {
      loggedIn: false,
      account: "",
      platform,
      source: "none",
      credentialPath: LANZOU_CREDENTIAL_PATH,
      supportsDpapi: false,
    };
    lanzouBatch = null;
    return { ...lanzouStatuses[platform] };
  },
  lanzouBrowse(platform: LanzouPlatform, folderID: string): LanzouListing {
    if (!lanzouStatuses[platform].loggedIn) throw new Error("尚未登录网盘，请先登录");
    const id = folderID.trim() || LANZOU_ROOT_ID;
    const listing = LANZOU_LISTINGS[id];
    if (!listing) return { folderId: id, folders: [], files: [] };
    return {
      folderId: id,
      folders: listing.folders.map((f) => ({ ...f })),
      files: listing.files.map((f) => ({ ...f })),
    };
  },
  lanzouPrepare(opts: LanzouOptions): LanzouPrepareResult {
    if (!lanzouStatuses[opts.platform].loggedIn) throw new Error("尚未登录网盘，请先登录");
    const localDir = opts.localDir.trim() || SAMPLE_ROOT;
    const folderId = opts.folderId.trim() || LANZOU_ROOT_ID;
    const folderName = opts.folderName.trim() || (folderId === LANZOU_ROOT_ID ? "根目录" : folderId);
    const items: LanzouItem[] = [
      { localPath: `${localDir}\\海边日落.jpg`, name: "海边日落.jpg", size: Math.round(2.4 * MB), kind: "pending" },
      { localPath: `${localDir}\\山顶合影.png`, name: "山顶合影.png", size: Math.round(1.8 * MB), kind: "pending" },
      { localPath: `${localDir}\\现场录音.mp3`, name: "现场录音.mp3", size: Math.round(5.2 * MB), kind: "pending" },
      { localPath: `${localDir}\\会议纪要.txt`, name: "会议纪要.txt", size: 3174, kind: "cached" },
      {
        localPath: `${localDir}\\产品报价单.pdf`, name: "产品报价单.pdf", size: Math.round(480 * 1024),
        kind: "duplicate", duplicateOf: "产品报价单.pdf", remoteId: "mock-file-501",
      },
      {
        localPath: `${localDir}\\旧版备份.zip`, name: "旧版备份.zip", size: 8 * MB,
        kind: "duplicate", duplicateOf: "旧版备份.zip", remoteId: "mock-file-502",
      },
      { localPath: `${localDir}\\年会视频.mp4`, name: "年会视频.mp4", size: 420 * MB, kind: "skipped-large" },
      {
        localPath: `${localDir}\\损坏文件.bin`, name: "损坏文件.bin", size: 12288,
        kind: "failed", error: "读取文件信息失败：浏览器预览为合成数据",
      },
    ];
    lanzouSeq++;
    lanzouBatch = {
      id: `mock-lanzou-${lanzouSeq}`,
      platform: opts.platform,
      localDir,
      folderId,
      folderName,
      workers: opts.workers,
      intervalMs: opts.intervalMs,
      baseItems: items.map((it) => ({ ...it })),
      startedAt: 0,
      canceled: false,
      cancelAt: 0,
    };
    let uploadable = 0;
    let duplicates = 0;
    let skippedLarge = 0;
    let cached = 0;
    for (const it of items) {
      if (it.kind === "pending") uploadable++;
      else if (it.kind === "duplicate") duplicates++;
      else if (it.kind === "skipped-large") skippedLarge++;
      else if (it.kind === "cached") cached++;
    }
    return {
      batchId: lanzouBatch.id,
      platform: opts.platform,
      folderId,
      folderName,
      localDir,
      total: items.length,
      uploadable,
      duplicates,
      skippedLarge,
      cached,
      items,
      message:
        `预检完成：待上传 ${uploadable}，云端重复 ${duplicates}，超 100 MiB ${skippedLarge}，本机缓存 ${cached}。` +
        "此步骤未改动任何本地文件（浏览器预览为合成数据）。",
    };
  },
  lanzouStartUpload(batchId: string, confirmed: boolean): void {
    if (!confirmed) throw new Error("未确认，拒绝上传");
    if (!readSettings().lanzou.disclaimerAccepted) throw new Error("请先阅读并同意免责声明");
    const b = lanzouBatch;
    if (!b || b.id !== batchId) throw new Error(`找不到上传批次：${batchId}`);
    if (lanzouStateOf(b).phase === "done") throw new Error("该批次已结束，请重新预检");
    if (b.startedAt > 0) throw new Error("该批次正在上传");
    b.startedAt = Date.now();
    b.canceled = false;
  },
  lanzouUploadState(batchId: string): LanzouUploadState {
    const b = lanzouBatch;
    if (!b || b.id !== batchId) throw new Error(`找不到上传批次：${batchId}`);
    return lanzouStateOf(b);
  },
  lanzouCancelUpload(batchId: string): void {
    const b = lanzouBatch;
    if (!b || b.id !== batchId) throw new Error(`找不到上传批次：${batchId}`);
    if (b.startedAt <= 0 || b.canceled) return;
    b.canceled = true;
    b.cancelAt = Date.now();
  },
  lanzouOpenWeb(platform: LanzouPlatform): void {
    window.open(
      platform === "youxiang" ? "https://www.ilanzou.com/" : "https://pc.woozooo.com/mydisk.php",
      "_blank",
      "noopener",
    );
  },
  lanzouOpenReport(_batchId: string): void {
    throw new Error("浏览器预览没有真实报告文件，请在桌面版中打开。");
  },
  lanzouPickDirectory(): string {
    return SAMPLE_ROOT;
  },
  lanzouAcceptDisclaimer(): void {
    const s = readSettings();
    s.lanzou.disclaimerAccepted = true;
    localStorage.setItem(SETTINGS_KEY, JSON.stringify(s));
  },

  // ---- 本地搜索：浏览器预览用合成文件表，排序与分页行为与真实后端一致 ----
  startSearch(req: SearchRequest): string {
    searchSeq += 1;
    const id = `mock-search-${searchSeq}`;
    const query = req.query.trim().toLowerCase();
    const wildcard = req.mode === "wildcard" && /[*?]/.test(query);
    const exts = req.exts.map((e) => e.replace(/^\./, "").toLowerCase()).filter(Boolean);
    const rows = MOCK_FILES.filter((f) => {
      if (exts.length > 0 && !exts.includes(f.name.split(".").pop()?.toLowerCase() ?? "")) return false;
      if (f.size < req.minSize) return false;
      if (req.maxSize > 0 && f.size > req.maxSize) return false;
      if (!query) return true;
      const lowerName = f.name.toLowerCase();
      const lowerPath = f.path.toLowerCase();
      if (wildcard) {
        const re = new RegExp("^" + query.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".") + "$");
        return re.test(lowerName) || re.test(lowerPath);
      }
      return lowerName.includes(query) || lowerPath.includes(query);
    });
    const limit = req.limit > 0 ? req.limit : 200000;
    const truncated = rows.length > limit;
    search = {
      id,
      rows: rows.slice(0, limit).map((f) => ({
        path: f.path,
        name: f.name,
        dir: f.path.slice(0, Math.max(0, f.path.lastIndexOf("\\"))),
        size: f.size,
        modTime: f.modTime,
        root: SAMPLE_ROOT,
      })),
      done: true,
      truncated,
      startedAt: Date.now(),
    };
    return id;
  },
  getSearchState(id: string): SearchState {
    if (!search || search.id !== id) throw new Error(`找不到搜索任务：${id}`);
    return {
      id,
      running: false,
      done: true,
      cancelled: false,
      visited: MOCK_FILES.length,
      dirs: 12,
      matched: search.rows.length,
      truncated: search.truncated,
      elapsedMs: Date.now() - search.startedAt,
    };
  },
  cancelSearch(_id: string): void {
    // 合成数据瞬间完成，没有可取消的工作。
  },
  getSearchResults(id: string, sortKey: string, desc: boolean, offset: number, limit: number): SearchResultPage {
    if (!search || search.id !== id) throw new Error(`找不到搜索任务：${id}`);
    const sorted = [...search.rows].sort((a, b) => {
      let r = 0;
      if (sortKey === "name") r = a.name.localeCompare(b.name, "zh-CN");
      else if (sortKey === "size") r = a.size - b.size;
      else if (sortKey === "modTime") r = a.modTime.localeCompare(b.modTime);
      else r = a.path.localeCompare(b.path, "zh-CN");
      return desc ? -r : r;
    });
    const start = Math.max(0, offset);
    const size = Math.max(1, Math.min(1000, limit));
    return {
      total: sorted.length,
      offset: start,
      limit: size,
      rows: sorted.slice(start, start + size),
      truncated: search.truncated,
      sortKey,
      sortDesc: desc,
    };
  },
  revealPath(_path: string): void {
    throw new Error("浏览器预览无法定位文件，请在桌面版中使用。");
  },
  openPath(_path: string): void {
    throw new Error("浏览器预览无法打开文件，请在桌面版中使用。");
  },
};

export const mockSampleRoot = SAMPLE_ROOT;
export { MB };
