import type { Client } from "./client";

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
  listJobs: () => call("ListJobs"),
  status: (id) => call("Status", id),
  events: (id) => call("Events", id),
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
  testTerminal: (t) => call("TestTerminal", t),
  subscribe(fn) {
    let off = () => undefined as void;
    void import("@wailsio/runtime").then(({ Events }) => {
      off = Events.On("jobs:changed", () => fn());
    });
    return () => off();
  },
};

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
