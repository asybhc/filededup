import type {
  AppInfo,
  DeleteMode,
  ExecuteState,
  GroupsPage,
  LicenseState,
  OrganizeMeta,
  OrganizeRequest,
  OrganizeRulesExport,
  OrganizeRulesImport,
  OrganizeState,
  OrganizeSettings,
  OrganizeUndoInfo,
  OrganizeUndoResult,
  PlanPreview,
  ScanRequest,
  ScanState,
  SettingsResult,
  SkippedPage,
  UISettings,
  UpdateCheckView,
  UpdateInstallView,
} from "./types";
import { mock } from "./mock";

interface WailsApp {
  GetAppInfo(): Promise<AppInfo>;
  PickDirectory(): Promise<string>;
  StartScan(req: ScanRequest): Promise<string>;
  GetScanState(id: string): Promise<ScanState>;
  CancelScan(id: string): Promise<void>;
  GetGroups(id: string, offset: number, limit: number): Promise<GroupsPage>;
  GetSkipped(id: string, offset: number, limit: number): Promise<SkippedPage>;
  PreviewPlan(id: string, deleteMode: DeleteMode): Promise<PlanPreview>;
  StartExecute(id: string, deleteMode: DeleteMode, confirmed: boolean): Promise<void>;
  GetExecuteState(id: string): Promise<ExecuteState>;
  GetSettings(): Promise<SettingsResult>;
  SaveSettings(s: UISettings): Promise<void>;
  GetOrganizeMeta(): Promise<OrganizeMeta>;
  StartOrganize(req: OrganizeRequest): Promise<string>;
  GetOrganizeState(id: string): Promise<OrganizeState>;
  ExportOrganizeRules(rules: OrganizeSettings): Promise<OrganizeRulesExport>;
  ImportOrganizeRules(): Promise<OrganizeRulesImport>;
  GetOrganizeUndoInfo(): Promise<OrganizeUndoInfo>;
  UndoLastOrganize(): Promise<OrganizeUndoResult>;
  CheckForUpdate(manual: boolean): Promise<UpdateCheckView>;
  InstallUpdate(): Promise<UpdateInstallView>;
  SkipUpdateVersion(version: string): Promise<void>;
  ClearSkippedUpdateVersion(): Promise<void>;
  GetLicenseState(): Promise<LicenseState>;
  GetMachineCode(): Promise<string>;
  ImportLicenseFile(): Promise<LicenseState>;
  ImportLicenseText(text: string): Promise<LicenseState>;
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
  async startScan(req: ScanRequest): Promise<string> {
    const b = bridge();
    if (b) return b.StartScan(req);
    return mock.startScan(req);
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
  async previewPlan(id: string, deleteMode: DeleteMode): Promise<PlanPreview> {
    const b = bridge();
    if (b) return b.PreviewPlan(id, deleteMode);
    return mock.previewPlan(id, deleteMode);
  },
  async startExecute(id: string, deleteMode: DeleteMode, confirmed: boolean): Promise<void> {
    const b = bridge();
    if (b) return b.StartExecute(id, deleteMode, confirmed);
    return mock.startExecute(id, deleteMode, confirmed);
  },
  async getExecuteState(id: string): Promise<ExecuteState> {
    const b = bridge();
    if (b) return b.GetExecuteState(id);
    return mock.getExecuteState();
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
};
