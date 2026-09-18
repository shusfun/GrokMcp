import type { Job, JobPage } from "../lib/jobs";
import type { Project, PromptResult } from "../lib/projects";
import type { BoundaryEvent, Client, DebugSnapshot } from "./client";

const service = "grokmcp/internal/app.Service";

type WailsWindow = Window & {
  wails?: { invoke?: unknown; invokeAsync?: unknown };
  _wails?: { environment?: { OS?: unknown } };
};

export function isWails(): boolean {
  const w = window as WailsWindow;
  return typeof w.wails?.invoke === "function"
    || typeof w.wails?.invokeAsync === "function"
    || typeof w._wails?.environment?.OS === "string";
}

async function call<T>(method: string, ...args: unknown[]): Promise<T> {
  const { Call } = await import("@wailsio/runtime");
  return Call.ByName(`${service}.${method}`, ...args) as Promise<T>;
}

export const wailsClient: Client = {
  listJobs: async () => (await call<Job[] | null>("ListJobs")) ?? [],
  listJobsPage: async (q) =>
    (await call<JobPage | null>("ListJobsPage", q.cursor ?? "", q.limit ?? 40, Boolean(q.include_archived), q.query ?? "", q.state ?? "all", q.project ?? "all", q.view ?? "all"))
    ?? { jobs: [], has_more: false },
  importProject: (path, installSkill = true) => call("ImportProject", path, Boolean(installSkill)),
  listProjects: async () => (await call<Project[] | null>("ListProjects")) ?? [],
  getProject: (id) => call("GetProject", id),
  removeProject: (id) => call("RemoveProject", id),
  skillStatus: (id) => call("SkillStatus", id),
  skillInstall: (id) => call("SkillInstall", id),
  skillUpdate: (id) => call("SkillUpdate", id),
  skillRemove: (id) => call("SkillRemove", id),
  generatePrompt: async (id, goal, constraints, acceptance) =>
    (await call<PromptResult>("GeneratePrompt", id, goal ?? "", constraints ?? "", acceptance ?? "")),
  savePrompt: (id, template) => call("SavePrompt", id, template),
  openProjectDir: (id) => call("OpenProjectDir", id),
  archiveJob: (id) => call("ArchiveJob", id),
  unarchiveJob: (id) => call("UnarchiveJob", id),
  deleteJob: (id) => call("DeleteJob", id),
  status: (id) => call("Status", id),
  events: async (id) => (await call<BoundaryEvent[] | null>("Events", id)) ?? [],
  statusBar: () => call("StatusBar"),
  setView: (id, view) => call("SetView", id, view),
  cancelTurn: (id) => call("CancelTurn", id),
  continueJob: (id) => call("Continue", id),
  planDecide: (id, decide, notes) => call("PlanDecide", id, decide, notes ?? ""),
  openTerminal: (id, dashboard) => call("OpenTerminal", id, Boolean(dashboard)),
  openProject: (id) => call("OpenProject", id),
  settings: () => call("Settings"),
  saveSettings: (s) => call("SaveSettings", s),
  diagnose: () => call("Diagnose"),
  installGrok: () => call("InstallGrok"),
  testTerminal: (t) => call("TestTerminal", t),
  debugSet: (id, enabled, payloads) => call("DebugSet", id, enabled, Boolean(payloads)),
  debugSnapshot: async (id, cursor, limit, levels, sources) =>
    (await call<DebugSnapshot | null>("DebugSnapshot", id, cursor ?? 0, limit ?? 200, levels ?? [], sources ?? []))
    ?? { job_id: id, cursor: cursor ?? 0, events: [] },
  debugExport: (id) => call("DebugExport", id),
  appVersion: () => call("AppVersion"),
  checkUpdate: () => call("CheckUpdate"),
  downloadUpdate: () => call("DownloadUpdate"),
  restartUpdate: () => call("RestartUpdate"),
  subscribe(fn) {
    let off = () => undefined as void;
    void import("@wailsio/runtime").then(({ Events }) => {
      off = Events.On("jobs:changed", () => fn());
    });
    return () => off();
  },
};

export type UpdaterEvents = {
  checkStarted?: () => void;
  updateAvailable?: (version: string) => void;
  noUpdate?: () => void;
  downloadStarted?: () => void;
  downloadProgress?: (written: number, total: number) => void;
  verifying?: () => void;
  installing?: () => void;
  updateReady?: () => void;
  error?: (message: string) => void;
};

function eventData(ev: { data?: unknown }): unknown {
  const d = ev?.data;
  if (Array.isArray(d)) return d[0];
  return d;
}

export async function listenUpdater(handlers: UpdaterEvents) {
  const { Events } = await import("@wailsio/runtime");
  const offs = [
    Events.On("wails:updater:check-started", () => handlers.checkStarted?.()),
    Events.On("wails:updater:update-available", (ev: { data?: unknown }) => {
      const d = eventData(ev) as { version?: string } | undefined;
      handlers.updateAvailable?.(typeof d?.version === "string" ? d.version : "");
    }),
    Events.On("wails:updater:no-update", () => handlers.noUpdate?.()),
    Events.On("wails:updater:download-started", () => handlers.downloadStarted?.()),
    Events.On("wails:updater:download-progress", (ev: { data?: unknown }) => {
      const d = eventData(ev) as { written?: number; total?: number } | undefined;
      handlers.downloadProgress?.(Number(d?.written ?? 0), Number(d?.total ?? 0));
    }),
    Events.On("wails:updater:verifying", () => handlers.verifying?.()),
    Events.On("wails:updater:installing", () => handlers.installing?.()),
    Events.On("wails:updater:update-ready", () => handlers.updateReady?.()),
    Events.On("wails:updater:error", (ev: { data?: unknown }) => {
      const d = eventData(ev) as { message?: string } | undefined;
      handlers.error?.(typeof d?.message === "string" ? d.message : "更新失败");
    }),
  ];
  return () => offs.forEach((fn) => fn());
}

export async function listenDesktop(handlers: { navigate?: (hash: string) => void; quitWarn?: () => void }) {
  const { Events } = await import("@wailsio/runtime");
  const offs = [
    Events.On("navigate", (ev: { data?: string }) => {
      if (typeof ev?.data === "string") handlers.navigate?.(ev.data);
    }),
    Events.On("quit:warn", () => handlers.quitWarn?.()),
  ];
  return () => offs.forEach((fn) => fn());
}

export async function quitApp() {
  await call("Quit");
}

export function hostOS(): string {
  const w = window as WailsWindow;
  return typeof w._wails?.environment?.OS === "string" ? w._wails.environment.OS : "";
}

async function currentWindow() {
  const { Window } = await import("@wailsio/runtime");
  return Window;
}

export async function hideWindow() {
  await (await currentWindow()).Hide();
}

export async function minimiseWindow() {
  await (await currentWindow()).Minimise();
}

export async function toggleMaximiseWindow() {
  await (await currentWindow()).ToggleMaximise();
}
