import type {
  AppInfo,
  DedupUndoInfo,
  DedupUndoResult,
  DeleteMode,
  ExecuteState,
  ExportFormat,
  ExportResult,
  DropPathResult,
  FullDiskPlan,
  ClearLogsResult,
  EnvCheckItem,
  GroupsPage,
  ImagePreview,
  LogOverview,
  LanzouListing,
  LanzouOptions,
  LanzouPlatform,
  LanzouPrepareResult,
  LanzouStatus,
  LanzouUploadState,
  LicenseState,
  OrganizeMeta,
  OrganizePreset,
  OrganizePresetsResult,
  OrganizeRequest,
  OrganizeRulesExport,
  OrganizeRulesImport,
  OrganizeState,
  OrganizeSettings,
  OrganizeTestRequest,
  OrganizeTestResult,
  OrganizeUndoInfo,
  OrganizeUndoResult,
  PlanPreview,
  ScanRequest,
  ScanState,
  ScheduleRunState,
  SearchBackends,
  SearchRequest,
  SearchResultPage,
  SearchState,
  SettingsResult,
  SkippedPage,
  TimelineMeta,
  TimelinePlan,
  TimelineRequest,
  TimelineState,
  UISettings,
  UpdateCheckView,
  UpdateInstallView,
} from "./types";
import { mock } from "./mock";

// onAppEvent 订阅 Wails 后端事件（托盘、定时任务）；浏览器预览下是空实现。
export function onAppEvent(name: string, cb: (payload: any) => void): () => void {
  const rt = (window as any).runtime;
  if (!rt || typeof rt.EventsOn !== "function") return () => undefined;
  const off = rt.EventsOn(name, cb);
  return typeof off === "function" ? off : () => rt.EventsOff?.(name);
}

interface WailsApp {
  GetAppInfo(): Promise<AppInfo>;
  PickDirectory(): Promise<string>;
  InspectDroppedPath(path: string): Promise<DropPathResult>;
  StartScan(req: ScanRequest): Promise<string>;
  GetFullDiskPlan(): Promise<FullDiskPlan>;
  GetScanState(id: string): Promise<ScanState>;
  CancelScan(id: string): Promise<void>;
  GetGroups(id: string, offset: number, limit: number): Promise<GroupsPage>;
  GetSkipped(id: string, offset: number, limit: number): Promise<SkippedPage>;
  PreviewPlan(id: string, deleteMode: DeleteMode, deletePaths: string[]): Promise<PlanPreview>;
  StartExecute(id: string, deleteMode: DeleteMode, deletePaths: string[], confirmed: boolean): Promise<void>;
  GetExecuteState(id: string): Promise<ExecuteState>;
  ExportResults(id: string, format: ExportFormat): Promise<ExportResult>;
  GetSettings(): Promise<SettingsResult>;
  SaveSettings(s: UISettings): Promise<void>;
  SaveAIKey(key: string): Promise<void>;
  GetScheduleState(): Promise<ScheduleRunState>;
  StartSearch(req: SearchRequest): Promise<string>;
  GetSearchBackends(): Promise<SearchBackends>;
  GetSearchState(id: string): Promise<SearchState>;
  CancelSearch(id: string): Promise<void>;
  GetSearchResults(id: string, sortKey: string, desc: boolean, offset: number, limit: number): Promise<SearchResultPage>;
  RevealPath(path: string): Promise<void>;
  OpenPath(path: string): Promise<void>;
  GetOrganizeMeta(): Promise<OrganizeMeta>;
  StartOrganize(req: OrganizeRequest): Promise<string>;
  GetOrganizeState(id: string): Promise<OrganizeState>;
  ExportOrganizeRules(rules: OrganizeSettings): Promise<OrganizeRulesExport>;
  ImportOrganizeRules(): Promise<OrganizeRulesImport>;
  ListOrganizePresets(): Promise<OrganizePresetsResult>;
  SaveOrganizePreset(name: string, rules: OrganizeSettings): Promise<OrganizePreset>;
  DeleteOrganizePreset(id: string): Promise<void>;
  TestOrganizeRules(req: OrganizeTestRequest): Promise<OrganizeTestResult>;
  GetOrganizeUndoInfo(): Promise<OrganizeUndoInfo>;
  UndoLastOrganize(): Promise<OrganizeUndoResult>;
  GetDedupUndoInfo(): Promise<DedupUndoInfo>;
  UndoLastDedup(): Promise<DedupUndoResult>;
  CheckForUpdate(manual: boolean): Promise<UpdateCheckView>;
  InstallUpdate(): Promise<UpdateInstallView>;
  SkipUpdateVersion(version: string): Promise<void>;
  ClearSkippedUpdateVersion(): Promise<void>;
  GetLicenseState(): Promise<LicenseState>;
  GetMachineCode(): Promise<string>;
  ImportLicenseFile(): Promise<LicenseState>;
  ImportLicenseText(text: string): Promise<LicenseState>;
  LanzouStatus(platform: LanzouPlatform): Promise<LanzouStatus>;
  LanzouLogin(platform: LanzouPlatform, username: string, password: string): Promise<LanzouStatus>;
  LanzouUseCookie(platform: LanzouPlatform, raw: string): Promise<LanzouStatus>;
  LanzouImportCSharp(platform: LanzouPlatform): Promise<LanzouStatus>;
  LanzouLogout(platform: LanzouPlatform): Promise<LanzouStatus>;
  LanzouBrowse(platform: LanzouPlatform, folderID: string): Promise<LanzouListing>;
  LanzouPrepare(opts: LanzouOptions): Promise<LanzouPrepareResult>;
  LanzouStartUpload(batchId: string, confirmed: boolean): Promise<void>;
  LanzouUploadState(batchId: string): Promise<LanzouUploadState>;
  LanzouCancelUpload(batchId: string): Promise<void>;
  LanzouOpenWeb(platform: LanzouPlatform): Promise<void>;
  LanzouOpenReport(batchId: string): Promise<void>;
  LanzouPickDirectory(): Promise<string>;
  LanzouAcceptDisclaimer(): Promise<void>;
  GetTimelineMeta(): Promise<TimelineMeta>;
  PreviewTimeline(req: TimelineRequest): Promise<TimelinePlan>;
  StartTimeline(req: TimelineRequest): Promise<string>;
  GetTimelineState(id: string): Promise<TimelineState>;
  CancelTimeline(id: string): Promise<void>;
  GetLogOverview(): Promise<LogOverview>;
  ReadLogFile(name: string): Promise<string>;
  ClearLogs(): Promise<ClearLogsResult>;
  RunEnvironmentCheck(): Promise<EnvCheckItem[]>;
  ReadImagePreview(path: string, maxEdge: number): Promise<ImagePreview>;
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
  }
}

function bridge(): WailsApp | undefined {
  return window.go?.main?.App;
}

export const isDesktop = Boolean(bridge());

export const api = {
  async getAppInfo(): Promise<AppInfo> {
    const b = bridge();
    if (b) return b.GetAppInfo();
    return { version: "0.8.0 (浏览器预览)", platform: "browser" };
  },
  async pickDirectory(): Promise<string> {
    const b = bridge();
    if (b) return b.PickDirectory();
    return mock.pickDirectory();
  },
  async inspectDroppedPath(path: string): Promise<DropPathResult> {
    const b = bridge();
    if (b) return b.InspectDroppedPath(path);
    return { path, isDir: true, fromFile: false, message: "浏览器预览：已采用 " + path };
  },
  async startScan(req: ScanRequest): Promise<string> {
    const b = bridge();
    if (b) return b.StartScan(req);
    return mock.startScan(req);
  },
  async getFullDiskPlan(): Promise<FullDiskPlan> {
    const b = bridge();
    if (b) return b.GetFullDiskPlan();
    return { supported: false, volumes: [], hardExcludes: [], message: "浏览器预览不支持全盘查重" };
  },
  async getScanState(id: string): Promise<ScanState> {
    const b = bridge();
    if (b) return b.GetScanState(id);
    return mock.getScanState(id);
  },
  async cancelScan(id: string): Promise<void> {
    const b = bridge();
    if (b) return b.CancelScan(id);
    return mock.cancelScan(id);
  },
  async getGroups(id: string, offset: number, limit: number): Promise<GroupsPage> {
    const b = bridge();
    if (b) return b.GetGroups(id, offset, limit);
    return mock.getGroups(id, offset, limit);
  },
  async getSkipped(id: string, offset: number, limit: number): Promise<SkippedPage> {
    const b = bridge();
    if (b) return b.GetSkipped(id, offset, limit);
    return mock.getSkipped(id, offset, limit);
  },
  async previewPlan(id: string, deleteMode: DeleteMode, deletePaths: string[]): Promise<PlanPreview> {
    const b = bridge();
    if (b) return b.PreviewPlan(id, deleteMode, deletePaths);
    return mock.previewPlan(id, deleteMode, deletePaths);
  },
  async startExecute(id: string, deleteMode: DeleteMode, deletePaths: string[], confirmed: boolean): Promise<void> {
    const b = bridge();
    if (b) return b.StartExecute(id, deleteMode, deletePaths, confirmed);
    return mock.startExecute(id, deleteMode, deletePaths, confirmed);
  },
  async getExecuteState(id: string): Promise<ExecuteState> {
    const b = bridge();
    if (b) return b.GetExecuteState(id);
    return mock.getExecuteState();
  },
  async exportResults(id: string, format: ExportFormat): Promise<ExportResult> {
    const b = bridge();
    if (b) return b.ExportResults(id, format);
    return mock.exportResults(format);
  },
  async getSettings(): Promise<SettingsResult> {
    const b = bridge();
    if (b) return b.GetSettings();
    return mock.getSettings();
  },
  async saveSettings(s: UISettings): Promise<void> {
    const b = bridge();
    if (b) return b.SaveSettings(s);
    mock.saveSettings(s);
  },
  async saveAIKey(key: string): Promise<void> {
    const b = bridge();
    if (b) return b.SaveAIKey(key);
    mock.saveAIKey(key);
  },
  async getScheduleState(): Promise<ScheduleRunState> {
    const b = bridge();
    if (b) return b.GetScheduleState();
    return mock.getScheduleState();
  },
  async startSearch(req: SearchRequest): Promise<string> {
    const b = bridge();
    if (b) return b.StartSearch(req);
    return mock.startSearch(req);
  },
  async getSearchBackends(): Promise<SearchBackends> {
    const b = bridge();
    if (b) return b.GetSearchBackends();
    return {
      backends: [
        { key: "local", name: "本地文件系统", available: true, message: "浏览器预览使用本地模拟数据。" },
        { key: "everything", name: "Everything（es.exe）", available: false, message: "浏览器预览不支持 Everything 后端。" },
      ],
    };
  },
  async getSearchState(id: string): Promise<SearchState> {
    const b = bridge();
    if (b) return b.GetSearchState(id);
    return mock.getSearchState(id);
  },
  async cancelSearch(id: string): Promise<void> {
    const b = bridge();
    if (b) return b.CancelSearch(id);
    mock.cancelSearch(id);
  },
  async getSearchResults(
    id: string,
    sortKey: string,
    desc: boolean,
    offset: number,
    limit: number,
  ): Promise<SearchResultPage> {
    const b = bridge();
    if (b) return b.GetSearchResults(id, sortKey, desc, offset, limit);
    return mock.getSearchResults(id, sortKey, desc, offset, limit);
  },
  async revealPath(path: string): Promise<void> {
    const b = bridge();
    if (b) return b.RevealPath(path);
    mock.revealPath(path);
  },
  async openPath(path: string): Promise<void> {
    const b = bridge();
    if (b) return b.OpenPath(path);
    mock.openPath(path);
  },
  async getOrganizeMeta(): Promise<OrganizeMeta> {
    const b = bridge();
    if (b) return b.GetOrganizeMeta();
    return mock.getOrganizeMeta();
  },
  async startOrganize(req: OrganizeRequest): Promise<string> {
    const b = bridge();
    if (b) return b.StartOrganize(req);
    return mock.startOrganize(req);
  },
  async getOrganizeState(id: string): Promise<OrganizeState> {
    const b = bridge();
    if (b) return b.GetOrganizeState(id);
    return mock.getOrganizeState(id);
  },
  async exportOrganizeRules(rules: OrganizeSettings): Promise<OrganizeRulesExport> {
    const b = bridge();
    if (b) return b.ExportOrganizeRules(rules);
    return mock.exportOrganizeRules(rules);
  },
  async importOrganizeRules(): Promise<OrganizeRulesImport> {
    const b = bridge();
    if (b) return b.ImportOrganizeRules();
    return mock.importOrganizeRules();
  },
  async listOrganizePresets(): Promise<OrganizePresetsResult> {
    const b = bridge();
    if (b) return b.ListOrganizePresets();
    return mock.listOrganizePresets();
  },
  async saveOrganizePreset(name: string, rules: OrganizeSettings): Promise<OrganizePreset> {
    const b = bridge();
    if (b) return b.SaveOrganizePreset(name, rules);
    return mock.saveOrganizePreset(name, rules);
  },
  async deleteOrganizePreset(id: string): Promise<void> {
    const b = bridge();
    if (b) return b.DeleteOrganizePreset(id);
    return mock.deleteOrganizePreset(id);
  },
  async testOrganizeRules(req: OrganizeTestRequest): Promise<OrganizeTestResult> {
    const b = bridge();
    if (b) return b.TestOrganizeRules(req);
    return mock.testOrganizeRules(req);
  },
  async getOrganizeUndoInfo(): Promise<OrganizeUndoInfo> {
    const b = bridge();
    if (b) return b.GetOrganizeUndoInfo();
    return mock.getOrganizeUndoInfo();
  },
  async undoLastOrganize(): Promise<OrganizeUndoResult> {
    const b = bridge();
    if (b) return b.UndoLastOrganize();
    return mock.undoLastOrganize();
  },
  async getDedupUndoInfo(): Promise<DedupUndoInfo> {
    const b = bridge();
    if (b) return b.GetDedupUndoInfo();
    return mock.getDedupUndoInfo();
  },
  async undoLastDedup(): Promise<DedupUndoResult> {
    const b = bridge();
    if (b) return b.UndoLastDedup();
    return mock.undoLastDedup();
  },
  async checkForUpdate(manual: boolean): Promise<UpdateCheckView> {
    const b = bridge();
    if (b) return b.CheckForUpdate(manual);
    return mock.checkForUpdate(manual);
  },
  async installUpdate(): Promise<UpdateInstallView> {
    const b = bridge();
    if (b) return b.InstallUpdate();
    return mock.installUpdate();
  },
  async skipUpdateVersion(version: string): Promise<void> {
    const b = bridge();
    if (b) return b.SkipUpdateVersion(version);
    mock.skipUpdateVersion(version);
  },
  async clearSkippedUpdateVersion(): Promise<void> {
    const b = bridge();
    if (b) return b.ClearSkippedUpdateVersion();
    mock.clearSkippedUpdateVersion();
  },
  async getLicenseState(): Promise<LicenseState> {
    const b = bridge();
    if (b) return b.GetLicenseState();
    return mock.getLicenseState();
  },
  async getMachineCode(): Promise<string> {
    const b = bridge();
    if (b) return b.GetMachineCode();
    return mock.getLicenseState().machineCode;
  },
  async importLicenseFile(): Promise<LicenseState> {
    const b = bridge();
    if (b) return b.ImportLicenseFile();
    return mock.importLicenseFile();
  },
  async importLicenseText(text: string): Promise<LicenseState> {
    const b = bridge();
    if (b) return b.ImportLicenseText(text);
    return mock.importLicenseText(text);
  },

  // ---- 蓝奏云/蓝奏优享上传：编译可用的门面，真实列表/上传需账号联调 ----
  async lanzouStatus(platform: LanzouPlatform): Promise<LanzouStatus> {
    const b = bridge();
    if (b) return b.LanzouStatus(platform);
    return mock.lanzouStatus(platform);
  },
  async lanzouLogin(platform: LanzouPlatform, username: string, password: string): Promise<LanzouStatus> {
    const b = bridge();
    if (b) return b.LanzouLogin(platform, username, password);
    return mock.lanzouLogin(platform, username, password);
  },
  async lanzouUseCookie(platform: LanzouPlatform, raw: string): Promise<LanzouStatus> {
    const b = bridge();
    if (b) return b.LanzouUseCookie(platform, raw);
    return mock.lanzouUseCookie(platform, raw);
  },
  async lanzouImportCSharp(platform: LanzouPlatform): Promise<LanzouStatus> {
    const b = bridge();
    if (b) return b.LanzouImportCSharp(platform);
    return mock.lanzouImportCSharp(platform);
  },
  async lanzouLogout(platform: LanzouPlatform): Promise<LanzouStatus> {
    const b = bridge();
    if (b) return b.LanzouLogout(platform);
    return mock.lanzouLogout(platform);
  },
  async lanzouBrowse(platform: LanzouPlatform, folderID: string): Promise<LanzouListing> {
    const b = bridge();
    if (b) return b.LanzouBrowse(platform, folderID);
    return mock.lanzouBrowse(platform, folderID);
  },
  async lanzouPrepare(opts: LanzouOptions): Promise<LanzouPrepareResult> {
    const b = bridge();
    if (b) return b.LanzouPrepare(opts);
    return mock.lanzouPrepare(opts);
  },
  async lanzouStartUpload(batchId: string, confirmed: boolean): Promise<void> {
    const b = bridge();
    if (b) return b.LanzouStartUpload(batchId, confirmed);
    mock.lanzouStartUpload(batchId, confirmed);
  },
  async lanzouUploadState(batchId: string): Promise<LanzouUploadState> {
    const b = bridge();
    if (b) return b.LanzouUploadState(batchId);
    return mock.lanzouUploadState(batchId);
  },
  async lanzouCancelUpload(batchId: string): Promise<void> {
    const b = bridge();
    if (b) return b.LanzouCancelUpload(batchId);
    mock.lanzouCancelUpload(batchId);
  },
  async lanzouOpenWeb(platform: LanzouPlatform): Promise<void> {
    const b = bridge();
    if (b) return b.LanzouOpenWeb(platform);
    mock.lanzouOpenWeb(platform);
  },
  async lanzouOpenReport(batchId: string): Promise<void> {
    const b = bridge();
    if (b) return b.LanzouOpenReport(batchId);
    mock.lanzouOpenReport(batchId);
  },
  async lanzouPickDirectory(): Promise<string> {
    const b = bridge();
    if (b) return b.LanzouPickDirectory();
    return mock.lanzouPickDirectory();
  },
  async lanzouAcceptDisclaimer(): Promise<void> {
    const b = bridge();
    if (b) return b.LanzouAcceptDisclaimer();
    mock.lanzouAcceptDisclaimer();
  },
  async getTimelineMeta(): Promise<TimelineMeta> {
    const b = bridge();
    if (b) return b.GetTimelineMeta();
    return {
      buckets: [
        { value: "day", label: "按日（2024-03-08）" },
        { value: "week", label: "按周（2024-W10）" },
        { value: "month", label: "按月（2024-03）" },
        { value: "year", label: "按年（2024）" },
      ],
      actions: [
        { value: "move", label: "移动（源目录不再保留）" },
        { value: "copy", label: "复制（源目录保留）" },
      ],
      conflicts: [
        { value: "rename", label: "改名保留（默认）" },
        { value: "skip", label: "跳过同名文件" },
        { value: "overwrite", label: "覆盖同名文件" },
      ],
    };
  },
  async previewTimeline(req: TimelineRequest): Promise<TimelinePlan> {
    const b = bridge();
    if (b) return b.PreviewTimeline(req);
    // 浏览器预览：给出一份结构完整的合成计划，不做任何落盘。
    const now = new Date();
    const bucket =
      req.bucket === "year"
        ? String(now.getFullYear())
        : req.bucket === "month"
          ? `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`
          : `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
    const items = ["样例照片.jpg", "样例文档.txt"].map((name, i) => ({
      source: `${req.root || "C:/预览"}${"\\"}${name}`,
      dest: `${req.target || "C:/预览输出"}${"\\"}${bucket}${"\\"}${name}`,
      relPath: name,
      bucket,
      when: now.toISOString().slice(0, 19).replace("T", " "),
      whenFrom: i === 0 ? "exif" : "mtime",
      size: 1024 * (i + 1),
      status: "ready",
      note: "",
    }));
    return {
      root: req.root,
      target: req.target,
      items,
      total: items.length,
      ready: items.length,
      conflicts: 0,
      skipped: 0,
      errored: 0,
      totalSize: 3072,
      truncated: false,
      notes: ["浏览器预览：合成数据，不会落盘。"],
      plannedFor: now.toISOString(),
    };
  },
  async startTimeline(req: TimelineRequest): Promise<string> {
    const b = bridge();
    if (b) return b.StartTimeline(req);
    throw new Error("浏览器预览不支持真实执行");
  },
  async getTimelineState(id: string): Promise<TimelineState> {
    const b = bridge();
    if (b) return b.GetTimelineState(id);
    throw new Error("浏览器预览不支持真实执行");
  },
  async cancelTimeline(id: string): Promise<void> {
    const b = bridge();
    if (b) return b.CancelTimeline(id);
  },
  async getLogOverview(): Promise<LogOverview> {
    const b = bridge();
    if (b) return b.GetLogOverview();
    return { dir: "浏览器预览无日志目录", files: [], totalSize: 0 };
  },
  async readLogFile(name: string): Promise<string> {
    const b = bridge();
    if (b) return b.ReadLogFile(name);
    return `浏览器预览不读取本地日志：${name}`;
  },
  async clearLogs(): Promise<ClearLogsResult> {
    const b = bridge();
    if (b) return b.ClearLogs();
    return { deleted: 0, freed: 0, errors: ["浏览器预览不支持清除日志"] };
  },
  async runEnvironmentCheck(): Promise<EnvCheckItem[]> {
    const b = bridge();
    if (b) return b.RunEnvironmentCheck();
    return [
      { name: "运行环境", status: "warn", detail: "浏览器预览：仅展示界面，未连接桌面后端。", fix: "" },
    ];
  },
  async readImagePreview(path: string, maxEdge: number): Promise<ImagePreview> {
    const b = bridge();
    if (b) return b.ReadImagePreview(path, maxEdge);
    const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1280" height="800"><rect width="100%" height="100%" fill="#1f2937"/><text x="50%" y="46%" fill="#e5e7eb" font-size="28" text-anchor="middle">浏览器预览占位</text><text x="50%" y="56%" fill="#9ca3af" font-size="18" text-anchor="middle">${path}</text></svg>`;
    return {
      dataUrl: "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg),
      width: 1280,
      height: 800,
      format: "svg",
    };
  },
};
