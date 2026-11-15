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

export interface UpdateSettings {
  manifestUrl: string;
  checkMode: "auto" | "manual";
  intervalHours: number;
  skippedVersion: string;
}

// 蓝奏云上传页的持久化选项。凭据不在这里：后端单独用 DPAPI 加密存 lanzou.bin。
export interface LanzouSettings {
  workers: number;
  intervalMs: number;
  forceReupload: boolean;
  disclaimerAccepted: boolean;
  lastLocalDir: string;
  lastFolderId: string;
  lastFolderName: string;
}

export interface AISettings {
  enabled: boolean;
  endpoint: string;
  model: string;
  timeoutSeconds: number;
  keyHint: string;
}

export interface WindowSettings {
  closeAction: "exit" | "tray";
  trayEnabled: boolean;
  theme: "system" | "light" | "dark";
}

export interface ScheduleSettings {
  enabled: boolean;
  mode: "interval" | "daily";
  intervalMinutes: number;
  dailyTime: string;
  action: "scan";
  root: string;
}

export interface LogSettings {
  retentionDays: number;
}

export interface UISettings {
  schemaVersion: number;
  scan: ScanSettings;
  organize: OrganizeSettings;
  update: UpdateSettings;
  lanzou: LanzouSettings;
  ai: AISettings;
  window: WindowSettings;
  schedule: ScheduleSettings;
  logs: LogSettings;
}

export interface SettingsResult {
  settings: UISettings;
  path: string;
  warning?: string;
  keyStoragePath: string;
  keyProtectionOk: boolean;
}

export interface ScheduleRunState {
  startedAt: string;
  finishedAt: string;
  status: "ok" | "failed" | "skipped" | "cancelled";
  message?: string;
  root: string;
  files: number;
  groups: number;
  reclaimable: number;
  elapsedMs: number;
}

// ---------- 本地搜索（对应 internal/engine/search 与 cmd/filededup/search.go） ----------

export type SearchMode = "substring" | "wildcard";

export interface SearchRequest {
  roots: string[];
  query: string;
  mode: SearchMode;
  exts: string[];
  minSize: number;
  maxSize: number;
  includeHidden: boolean;
  excludes: string[];
  limit: number;
}

export interface SearchState {
  id: string;
  running: boolean;
  done: boolean;
  cancelled: boolean;
  error?: string;
  visited: number;
  dirs: number;
  matched: number;
  truncated: boolean;
  elapsedMs: number;
  current?: string;
}

export interface SearchMatch {
  path: string;
  name: string;
  dir: string;
  size: number;
  modTime: string;
  root: string;
}

export interface SearchResultPage {
  total: number;
  offset: number;
  limit: number;
  rows: SearchMatch[];
  truncated: boolean;
  sortKey: string;
  sortDesc: boolean;
}

// ---------- 授权（对应 internal/auth） ----------

export type LicenseStatus = "licensed" | "trial" | "trial-expired" | "unlicensed";

export interface LicenseFeature {
  key: string;
  name: string;
  unlocked: boolean;
  edition: string;
  reason?: string;
}

export interface LicenseState {
  status: LicenseStatus;
  licensed: boolean;
  source: string;
  edition?: string;
  editionCode?: string;
  featureList: LicenseFeature[];
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

// ---------- 自动更新（对应 cmd/filededup/update.go） ----------

export type UpdateCheckStatus =
  | "disabled"
  | "manual"
  | "throttled"
  | "uptodate"
  | "available"
  | "skipped";

export interface UpdateCheckView {
  status: UpdateCheckStatus;
  message?: string;
  currentVersion: string;
  latestVersion?: string;
  notes?: string;
  releasedAt?: string;
  assetName?: string;
  assetSize?: number;
  sourceUrl?: string;
  checkedAt?: string;
  skippedVersion?: string;
}

export interface UpdateInstallView {
  version: string;
  exePath: string;
  backupPath: string;
  size: number;
  sha256: string;
  restarting: boolean;
}

// ---------- 蓝奏云上传（对应 cmd/filededup/lanzou.go、internal/cloud/lanzou） ----------
// 注意：登录成功后的真实列表、上传与去重命中都需要真实账号联调，界面必须如实标注。

export type LanzouSource = "login" | "cookie" | "import" | "local" | "none";

export interface LanzouStatus {
  loggedIn: boolean;
  account: string;
  platform: string;
  source: LanzouSource;
  credentialPath: string;
  supportsDpapi: boolean;
  message?: string;
  savedAt?: string;
}

export interface LanzouFolder {
  id: string;
  name: string;
}

// size = -1 表示服务端给的大小无法解析。
export interface LanzouRemoteFile {
  id: string;
  name: string;
  size: number;
  sizeText?: string;
  time?: string;
}

export interface LanzouListing {
  folderId: string;
  folders: LanzouFolder[];
  files: LanzouRemoteFile[];
}

export interface LanzouOptions {
  localDir: string;
  folderId: string;
  folderName: string;
  workers: number;
  intervalMs: number;
  forceReupload: boolean;
}

export type LanzouItemKind =
  | "pending"
  | "success"
  | "instant"
  | "cached"
  | "skipped-large"
  | "duplicate"
  | "canceled"
  | "failed";

export interface LanzouItem {
  localPath: string;
  name: string;
  size: number;
  kind: LanzouItemKind;
  remoteId?: string;
  duplicateOf?: string;
  movedTo?: string;
  error?: string;
}

export interface LanzouSummary {
  total: number;
  success: number;
  instant: number;
  cached: number;
  skippedLarge: number;
  duplicate: number;
  canceled: number;
  failed: number;
}

export interface LanzouPrepareResult {
  batchId: string;
  folderId: string;
  folderName: string;
  localDir: string;
  total: number;
  uploadable: number;
  duplicates: number;
  skippedLarge: number;
  cached: number;
  items: LanzouItem[];
  message?: string;
}

export type LanzouPhase = "prepared" | "running" | "cancelling" | "done" | "failed";

export interface LanzouUploadState {
  batchId: string;
  phase: LanzouPhase;
  folderId: string;
  folderName: string;
  localDir: string;
  workers: number;
  intervalMs: number;
  total: number;
  done: number;
  bytesSent: number;
  bytesTotal: number;
  active: string[];
  summary: LanzouSummary;
  items: LanzouItem[];
  reportJson?: string;
  reportCsv?: string;
  error?: string;
  startedAt?: string;
  finishedAt?: string;
}
