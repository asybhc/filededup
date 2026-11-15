import type {
  AppInfo,
  DeleteMode,
  ExecuteState,
  GroupsPage,
  PlanPreview,
  ScanRequest,
  ScanState,
  SkippedPage,
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
    return { version: "0.6.0-ui (浏览器预览)", platform: "browser" };
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
};
