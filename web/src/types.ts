// 与 cmd/filededup/app.go 的 DTO 一一对应。字段名固定，改后端必须同步改这里。

export type KeepPolicy = "pathShortest" | "newest" | "oldest" | "nameShortest";
export type DeleteMode = "trash" | "permanent";

export interface ScanRequest {
  root: string;
  keepPolicy: KeepPolicy;
  similarImage: boolean;
  imageThreshold: number;
  similarAudio: boolean;
  audioTolerance: number;
  minSize: number;
  includeHidden: boolean;
  excludes: string[];
}

export interface ScanState {
  id: string;
  phase: "running" | "cancelling" | "done" | "failed" | "cancelled";
  files: number;
  bytes: number;
  hashTargets: number;
  hashDone: number;
  hashBytes: number;
  hashTotalBytes: number;
  enumerationDone: boolean;
  groups: number;
  duplicateFiles: number;
  reclaimable: number;
  skipped: number;
  error?: string;
  elapsedMs: number;
}

export interface MemberView {
  path: string;
  name: string;
  size: number;
  modTime: string;
  keep: boolean;
  deletable: boolean;
}

export interface GroupView {
  id: string;
  kind: string;
  keep: string;
  reclaimable: number;
  members: MemberView[];
}

export interface GroupsPage {
  total: number;
  offset: number;
  items: GroupView[];
}

export interface SkipView {
  path: string;
  code: string;
  message: string;
}

export interface SkippedPage {
  total: number;
  offset: number;
  items: SkipView[];
}

export interface ActionView {
  seq: number;
  kind: string;
  src: string;
  reason: string;
  size: number;
}

export interface PlanPreview {
  requestId: string;
  deleteMode: string;
  total: number;
  groups: number;
  reclaimableBytes: number;
  kindCounts: Record<string, number>;
  samples: ActionView[];
}

export interface ExecuteState {
  running: boolean;
  done: boolean;
  total: number;
  applied: number;
  failed: number;
  skipped: number;
  rolledBack: number;
  reclaimed: number;
  error?: string;
  finishedAt?: string;
}

export interface AppInfo {
  version: string;
  platform: string;
}

export type Row =
  | { type: "group"; key: string; group: GroupView }
  | { type: "member"; key: string; groupId: string; member: MemberView };
