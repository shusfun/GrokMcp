import type { Job } from "../lib/jobs";
import type { BoundaryEvent, Client, Diagnose, InstallResult, Settings, StatusBar } from "./client";

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
};

let grokInstalled = false;

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
  async status(jobId) {
    return { ...find(jobId) };
  },
  async events(jobId) {
    const job = find(jobId);
    return [{ id: 1, job_id: jobId, event_type: job.state, summary: job.last_action ?? "", created_at: job.updated_at }] satisfies BoundaryEvent[];
  },
  async statusBar(): Promise<StatusBar> {
    return { leader_ok: true, mcp_ok: true, working: 1, needs_input: 1 };
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
