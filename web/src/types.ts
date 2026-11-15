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

// ---------- 设置持久化（对应 cmd/filededup/settings.go） ----------

export interface ScanSettings {
  root: string;
  keepPolicy: KeepPolicy;
  deleteMode: DeleteMode;
  similarImage: boolean;
  imageThreshold: number;
  similarAudio: boolean;
  audioTolerance: number;
  minSize: number;
  includeHidden: boolean;
  excludes: string[];
}

export interface OrganizeSteps {
  renamePrefix: boolean;
  renameSuffix: boolean;
  specialDelete: boolean;
  readonly: boolean;
  contentDedup: boolean;
  imageDedup: boolean;
  audioDedup: boolean;
  organize: boolean;
  emptyDirs: boolean;
}

// 整理器比扫描多一个“保留最大文件”，引擎 ParseKeepRule 与设置默认值都用 largest。
export type KeepRule = KeepPolicy | "largest";

export interface OrganizeSettings {
  root: string;
  target: string;
  usePreviewPlan: boolean;
  steps: OrganizeSteps;
  mode: string;
  keyword: string;
  regex: string;
  fixedDir: string;
  conflict: string;
  deleteMode: DeleteMode;
  keepPolicy: KeepRule;
  imageKeepPolicy: KeepRule;
  imageThreshold: number;
  audioTolerance: number;
  strength: number;
  minSize: number;
  includeHidden: boolean;
  excludes: string[];
  protectWords: string;
  hardlink: boolean;
  continueOnError: boolean;
}

export interface UISettings {
  schemaVersion: number;
  scan: ScanSettings;
  organize: OrganizeSettings;
}

export interface SettingsResult {
  settings: UISettings;
  path: string;
  warning?: string;
}

// ---------- 授权（对应 internal/auth） ----------

export type LicenseStatus = "licensed" | "trial" | "trial-expired" | "unlicensed";

export interface LicenseState {
  status: LicenseStatus;
  licensed: boolean;
  source: string;
  edition?: string;
  expiry?: string;
  forever: boolean;
  machineCode: string;
  features: string[];
  canApply: boolean;
  canOrganize: boolean;
  trialActive: boolean;
  trialStart?: string;
  trialEnd?: string;
  trialDaysLeft: number;
  licensePath?: string;
  limits: string;
  warning?: string;
}

// ---------- 整理器（对应 cmd/filededup/organize.go） ----------

export interface OrganizeRequest extends Omit<OrganizeSettings, "root"> {
  root: string;
  dryRun: boolean;
  // 只扫描与规划，不创建目录、不改文件；用于先收集存疑项。
  planOnly?: boolean;
  // 用户逐条确认要删除的存疑项路径；未列出的一律保留。
  confirmedSuspects?: string[];
}

export interface OrganizeStepMeta {
  id: string;
  title: string;
}

export interface OrganizeMeta {
  standalone: OrganizeStepMeta[];
  preview: OrganizeStepMeta[];
}

export interface Counter {
  renamed: number;
  specialDeleted: number;
  moved: number;
  conflictSkip: number;
  readonly: number;
  organizeSkip: number;
  contentDeleted: number;
  imageDeleted: number;
  audioDeleted: number;
  emptyDirs: number;
  excluded: number;
  hiddenSkipped: number;
  suspectsKept: number;
  hardlinkMerged: number;
}

export interface OrganizeStepView {
  id: string;
  title: string;
  order: number;
  enabled: boolean;
  skipped: boolean;
  skipReason?: string;
  actionNum: number;
  counters: Counter;
}

export interface OrganizeActionView {
  kind: string;
  step: string;
  from?: string;
  to?: string;
  reason?: string;
  rule?: string;
}

export interface OrganizeExecView {
  total: number;
  applied: number;
  failed: number;
  skipped: number;
  errors: string[];
}

export interface OrganizeSuspect {
  key: string;
  path: string;
  reason: string;
  suggestion: string;
  normalized?: string;
  score: number;
}

export interface OrganizeState {
  id: string;
  phase: "running" | "done" | "failed";
  stage: string;
  dryRun: boolean;
  order: "standalone" | "preview";
  usePreviewPlan: boolean;
  elapsedMs: number;
  error?: string;
  steps: OrganizeStepView[];
  counters: Counter;
  exec: OrganizeExecView;
  actionTotal: number;
  actions: OrganizeActionView[];
  suspects: OrganizeSuspect[];
}

// ---------- 整理规则导入导出 / 撤销（对应 cmd/filededup/organize_rules.go、organize_undo.go） ----------

export interface OrganizeRulesExport {
  canceled: boolean;
  path: string;
  bytes: number;
}

export interface OrganizeRulesImport {
  canceled: boolean;
  path: string;
  rules: OrganizeSettings;
}

export interface OrganizeUndoInfo {
  available: boolean;
  id?: string;
  root?: string;
  startedAt?: string;
  records: number;
}

export interface OrganizeUndoResult {
  id: string;
  root: string;
  total: number;
  restored: number;
  failed: number;
  skipped: number;
  unrestorable: number;
  errors: string[];
}
