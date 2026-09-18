import type { Job, JobPage } from "../lib/jobs";
import { sortJobs } from "../lib/jobs";
import type { BoundaryEvent, Client, DebugSnapshot, Diagnose, InstallResult, Settings, StatusBar, TraceEvent } from "./client";

const jobs: Job[] = [
  {
    job_id: "job-runtime",
    grok_session_id: "11111111-1111-1111-1111-111111111111",
    cwd: "/tmp/suiyuan",
    project: "suiyuan",
    title: "Runtime 底座",
    state: "executing",
    view_mode: "headless",
    input_owner: "supervisor",
    last_action: "Running tests",
    last_summary: "tests in progress",
    elapsed_seconds: 18 * 60,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    job_id: "job-auth",
    grok_session_id: "22222222-2222-2222-2222-222222222222",
    cwd: "/tmp/auth",
    project: "auth",
    title: "登录重构",
    state: "plan_ready",
    view_mode: "headed",
    input_owner: "tui",
    last_action: "Plan ready",
    plan_summary: "登录改成会话 cookie，并补测试。",
    elapsed_seconds: 4 * 60,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    job_id: "job-ui",
    grok_session_id: "33333333-3333-3333-3333-333333333333",
    cwd: "/tmp/admin-web",
    project: "admin-web",
    title: "UI 测试",
    state: "needs_input",
    view_mode: "headless",
    input_owner: "supervisor",
    last_action: "Tests failed",
    last_summary: "需要修复失败用例",
    elapsed_seconds: 11 * 60,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    job_id: "job-cancelled",
    grok_session_id: "44444444-4444-4444-4444-444444444444",
    cwd: "/tmp/dash",
    project: "GrokMcp",
    title: "Dashboard 可视化测试",
    state: "cancelled",
    view_mode: "headless",
    input_owner: "supervisor",
    last_action: "Cancelled",
    last_summary: "plan cancelled",
    user_cancelled: true,
    elapsed_seconds: 18 * 60,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
];

let settings: Settings = {
  grok_binary_path: "",
  terminal_provider: "default",
  terminal_command_template: "",
  default_view_mode: "headless",
  debug_enabled: false,
  debug_payloads: false,
};

let grokInstalled = false;
const traces = new Map<string, TraceEvent[]>();
let seq = 0;
pushTrace("job-runtime", "queue.enqueued", { kind: "plan" });
pushTrace("job-runtime", "pump.skipped", { reason: "tui_owns" });
pushTrace("job-auth", "plan.ready");

function pushTrace(jobId: string, event: string, fields?: Record<string, unknown>) {
  seq += 1;
  const ev: TraceEvent = {
    seq,
    time: new Date().toISOString(),
    level: event.includes("stalled") ? "warn" : "info",
    source: "supervisor",
    event,
    job_id: jobId,
    message: event,
    fields,
  };
  const list = traces.get(jobId) ?? [];
  list.push(ev);
  traces.set(jobId, list);
}

const listeners = new Set<() => void>();
function emit() {
  listeners.forEach((fn) => fn());
}

function find(id: string): Job {
  const job = jobs.find((j) => j.job_id === id);
  if (!job) throw new Error("job not found");
  return job;
}

export const mockClient: Client = {
  async listJobs() {
    return jobs.map((j) => ({ ...j }));
  },
  async listJobsPage(q): Promise<JobPage> {
    const includeArchived = Boolean(q.include_archived);
    let list = jobs.filter((j) => includeArchived ? Boolean(j.archived_at) : !j.archived_at);
    const query = (q.query ?? "").trim().toLowerCase();
    if (q.state && q.state !== "all") {
      if (q.state === "attention") list = list.filter((j) => j.state === "plan_ready" || j.state === "needs_input");
      else if (q.state === "working") list = list.filter((j) => ["planning", "executing", "starting", "recovering"].includes(j.state));
      else list = list.filter((j) => j.state === q.state);
    }
    if (q.project && q.project !== "all") list = list.filter((j) => j.project === q.project);
    if (q.view && q.view !== "all") list = list.filter((j) => j.view_mode === q.view);
    if (query) {
      list = list.filter((j) => [j.title, j.project, j.last_action, j.grok_session_id].filter(Boolean).some((v) => String(v).toLowerCase().includes(query)));
    }
    list = sortJobs(list);
    const limit = q.limit && q.limit > 0 ? q.limit : 40;
    const start = q.cursor ? list.findIndex((j) => j.job_id === q.cursor) + 1 : 0;
    const slice = list.slice(Math.max(0, start), Math.max(0, start) + limit);
    const hasMore = Math.max(0, start) + slice.length < list.length;
    return { jobs: slice.map((j) => ({ ...j })), next_cursor: hasMore ? slice.at(-1)?.job_id : "", has_more: hasMore };
  },
  async listProjects() {
    return [...new Set(jobs.map((j) => j.project).filter(Boolean))].sort();
  },
  async archiveJob(jobId) {
    const job = find(jobId);
    if (["starting", "planning", "executing", "recovering", "plan_ready", "needs_input", "disconnected"].includes(job.state)) {
      throw new Error("cannot archive active job");
    }
    job.archived_at = new Date().toISOString();
    emit();
    return { ...job };
  },
  async unarchiveJob(jobId) {
    const job = find(jobId);
    job.archived_at = undefined;
    emit();
    return { ...job };
  },
  async deleteJob(jobId) {
    const idx = jobs.findIndex((j) => j.job_id === jobId);
    if (idx < 0) return;
    const job = jobs[idx];
    if (["starting", "planning", "executing", "recovering", "plan_ready", "needs_input", "disconnected"].includes(job.state)) {
      throw new Error("cannot delete active job");
    }
    jobs.splice(idx, 1);
    emit();
  },
  async status(jobId) {
    return { ...find(jobId) };
  },
  async events(jobId) {
    const job = find(jobId);
    return [{ id: 1, job_id: jobId, event_type: job.state, summary: job.last_action ?? "", created_at: job.updated_at }] satisfies BoundaryEvent[];
  },
  async statusBar(): Promise<StatusBar> {
    return { leader_ok: true, acp_ok: true, mcp_ok: true, db_ok: true, debug_enabled: Boolean(settings.debug_enabled), stalled: jobs.some((j) => j.stalled), working: 1, needs_input: 1 };
  },
  async setView(jobId, view) {
    const job = find(jobId);
    job.view_mode = view as Job["view_mode"];
    job.input_owner = view === "headed" ? "tui" : "supervisor";
    emit();
    return { ...job };
  },
  async cancelTurn(jobId) {
    const job = find(jobId);
    job.state = "needs_input";
    job.last_action = "Turn cancelled";
    emit();
    return { ...job };
  },
  async planDecide(jobId, decide, notes) {
    const job = find(jobId);
    if (decide === "cancel") {
      job.state = "cancelled";
      job.last_action = "Cancelled";
    } else if (decide === "revise") {
      job.state = "planning";
      job.last_action = notes || "Revising plan";
    } else {
      job.state = "executing";
      job.last_action = "Implementing";
    }
    emit();
    return { ...job };
  },
  async continueJob(jobId) {
    const job = find(jobId);
    job.state = "executing";
    job.last_action = "Working";
    emit();
    return { ...job };
  },
  async openTerminal() {},
  async openProject() {},
  async settings() {
    return { ...settings };
  },
  async saveSettings(s) {
    settings = { ...s };
  },
  async diagnose(): Promise<Diagnose> {
    if (!grokInstalled) {
      return {
        grok_path: "",
        grok_version: "",
        logged_in: false,
        leader_running: false,
        leader_socket: "~/.grok/leader.sock",
        compatible: false,
        attach_mode: "",
        error: "Grok 二进制未找到。安装 Grok Build 并登录后再试。",
      };
    }
    return {
      grok_path: "/Users/shus/.grok/bin/grok",
      grok_version: "1.0.34",
      logged_in: true,
      leader_running: true,
      leader_socket: "~/.grok/leader.sock",
      compatible: true,
      attach_mode: "boundary",
    };
  },
  async installGrok(): Promise<InstallResult> {
    grokInstalled = true;
    return { ok: true, grok_path: "/Users/shus/.grok/bin/grok", log: "mock: installed to ~/.grok/bin/grok" };
  },
  async testTerminal() {},
  async debugSet(jobId, enabled, payloads) {
    if (!jobId) {
      settings = { ...settings, debug_enabled: enabled, debug_payloads: Boolean(payloads) };
      emit();
      return { job_id: "", cwd: "", project: "", title: "", state: "created", view_mode: "headless", input_owner: "supervisor", elapsed_seconds: 0, created_at: "", updated_at: "", debug_enabled: enabled };
    }
    const job = find(jobId);
    job.debug_enabled = enabled;
    pushTrace(jobId, enabled ? "debug.enabled" : "debug.disabled", { payloads: Boolean(payloads) });
    emit();
    return { ...job };
  },
  async debugSnapshot(jobId, cursor = 0, limit = 200): Promise<DebugSnapshot> {
    const list = (traces.get(jobId) ?? []).filter((ev) => ev.seq > cursor).slice(0, limit);
    return { job_id: jobId, cursor: list.at(-1)?.seq ?? cursor, events: list };
  },
  async debugExport(jobId) {
    return { path: `/tmp/${jobId}.jsonl` };
  },
  async appVersion() {
    return "dev";
  },
  async checkUpdate() {
    return null;
  },
  async downloadUpdate() {},
  async restartUpdate() {},
  subscribe(fn) {
    listeners.add(fn);
    return () => listeners.delete(fn);
  },
};
