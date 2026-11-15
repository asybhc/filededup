// 浏览器预览用的合成后端。仅在 window.go 不存在时启用；
// 界面顶部会显示"浏览器预览（合成数据）"横幅，不会被当成真实扫描结果。

import type {
  DeleteMode,
  ExecuteState,
  GroupView,
  GroupsPage,
  PlanPreview,
  ScanRequest,
  ScanState,
  SkippedPage,
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
};

export const mockSampleRoot = SAMPLE_ROOT;
export { MB };
