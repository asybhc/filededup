// 浏览器预览用的合成后端。仅在 window.go 不存在时启用；
// 界面顶部会显示"浏览器预览（合成数据）"横幅，不会被当成真实扫描结果。

import type {
  Counter,
  DeleteMode,
  ExecuteState,
  GroupView,
  GroupsPage,
  LicenseState,
  OrganizeRequest,
  OrganizeMeta,
  OrganizeState,
  OrganizeStepView,
  PlanPreview,
  ScanRequest,
  ScanState,
  SettingsResult,
  SkippedPage,
  UISettings,
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

function previewOf(id: string, gs: GroupView[], mode: DeleteMode): PlanPreview {
  const kinds: Record<string, number> = {};
  let total = 0;
  const samples = [] as PlanPreview["samples"];
  for (const g of gs) {
    for (const m of g.members) {
      if (!m.deletable) continue;
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
    scan: {
      root: SAMPLE_ROOT,
      keepPolicy: "pathShortest",
      deleteMode: "trash",
      similarImage: true,
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
    usePreviewPlan: false, elapsedMs: 0, steps: [], counters: zeroCounter(),
    exec: { total: 0, applied: 0, failed: 0, skipped: 0, errors: [] }, actionTotal: 0, actions: [],
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
  return {
    id,
    phase: "done",
    stage: "",
    dryRun: req.dryRun,
    order: req.usePreviewPlan ? "preview" : "standalone",
    usePreviewPlan: req.usePreviewPlan,
    elapsedMs: elapsed,
    steps,
    counters,
    exec: req.dryRun
      ? { total, applied: 0, failed: 0, skipped: total, errors: [] }
      : { total, applied: total, failed: 0, skipped: 0, errors: [] },
    actionTotal: total,
    actions,
  };
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
  previewPlan(id: string, mode: DeleteMode): PlanPreview {
    return previewOf(id, groups, mode);
  },
  startExecute(id: string, mode: DeleteMode, confirmed: boolean): void {
    if (!confirmed) throw new Error("未确认，拒绝执行");
    const p = previewOf(id, groups, mode);
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
    return { settings: readSettings(), path: "localStorage:" + SETTINGS_KEY, warning: undefined };
  },
  saveSettings(s: UISettings): void {
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
    return orgJob.id;
  },
  getOrganizeState(id: string): OrganizeState {
    if (!orgJob || orgJob.id !== id) {
      return { ...emptyOrgState(id), phase: "failed", error: "整理任务不存在" };
    }
    return buildOrgState(id, orgJob.req, Date.now() - orgJob.startedAt);
  },
  // ---- 授权：浏览器预览没有真实授权，按“试用中”展示，导入操作如实拒绝 ----
  getLicenseState(): LicenseState {
    return {
      status: "trial",
      licensed: false,
      source: "trial",
      forever: false,
      machineCode: "DEMO-0000-0000-0000",
      features: ["scan", "apply", "organize"],
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
};

export const mockSampleRoot = SAMPLE_ROOT;
export { MB };
