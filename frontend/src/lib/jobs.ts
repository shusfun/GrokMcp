export type JobState =
  | "created"
  | "starting"
  | "planning"
  | "plan_ready"
  | "executing"
  | "completed"
  | "needs_input"
  | "disconnected"
  | "recovering"
  | "cancelled"
  | "blocked"
  | "failed";

export type ViewMode = "headless" | "attaching" | "headed" | "detaching";

export type Job = {
  job_id: string;
  grok_session_id?: string;
  cwd: string;
  project_id?: string;
  project: string;
  title: string;
  state: JobState;
  view_mode: ViewMode;
  input_owner: string;
  last_action?: string;
  last_summary?: string;
  plan_summary?: string;
  desired_view_mode?: ViewMode;
  busy?: boolean;
  user_cancelled?: boolean;
  debug_enabled?: boolean;
  debug_cursor?: number;
  queue_length?: number;
  active_turn_id?: string;
  approval_id?: string;
  approval_delivery?: string;
  approval_supports_notes?: boolean;
  pause_reason?: string;
  result_turn_id?: string;
  result?: { request_id: string; turn_id: string; text: string; offset: number; next_offset: number; total: number; has_more: boolean };
  request_phase?: string;
  request_id?: string;
  accepted_request_id?: string;
  queued_request_ids?: string[];
  approved?: boolean;
  plan_version?: number;
  plan_turn_id?: string;
  event_cursor?: number;
  wait_reason?: string;
  last_activity_at?: string;
  activity_kind?: string;
  terminal_pid?: number;
  terminal_window_id?: string;
  stalled?: boolean;
  stalled_reason?: string;
  elapsed_seconds: number;
  created_at: string;
  updated_at: string;
  archived_at?: string;
};

export type JobFilters = {
  query: string;
  state: string;
  project: string;
  view: string;
  include_archived: boolean;
};

export type ListJobsQuery = {
  cursor?: string;
  limit?: number;
  include_archived?: boolean;
  query?: string;
  state?: string;
  project?: string;
  view?: string;
};

export type JobPage = {
  jobs: Job[];
  next_cursor?: string;
  has_more: boolean;
};

const STAGE: Record<JobState, string> = {
  created: "已创建",
  starting: "启动中",
  planning: "规划中",
  plan_ready: "Plan 就绪",
  executing: "实施中",
  completed: "已完成",
  needs_input: "需要输入",
  disconnected: "待手动恢复",
  recovering: "恢复中",
  cancelled: "已取消",
  blocked: "阻塞",
  failed: "失败",
};

const VIEW: Record<ViewMode, string> = {
  headless: "无头",
  attaching: "附着中",
  headed: "有头",
  detaching: "分离中",
};

export function stageLabel(state: string): string {
  return STAGE[state as JobState] ?? state;
}

export function viewLabel(view: string): string {
  return VIEW[view as ViewMode] ?? view;
}

export function stageViewLabel(state: string, view: string): string {
  return `${stageLabel(state)} · ${viewLabel(view)}`;
}

export function formatElapsed(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 60) return "1m";
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return m === 0 ? `${h}h` : `${h}h${m}m`;
}

const WORKING: JobState[] = ["planning", "executing", "starting", "recovering"];
const ATTENTION: JobState[] = ["needs_input", "plan_ready"];
const ACTIVE: JobState[] = ["starting", "planning", "executing", "recovering", "plan_ready", "needs_input", "disconnected"];
const HISTORY: JobState[] = ["completed", "cancelled", "failed", "blocked"];

function matchesState(job: Job, state: string): boolean {
  if (!state || state === "all") return true;
  if (state === "attention") return ATTENTION.includes(job.state);
  if (state === "working") return WORKING.includes(job.state);
  return job.state === state;
}

export function filterJobs(jobs: Job[], filters: JobFilters): Job[] {
  const q = filters.query.trim().toLowerCase();
  return jobs.filter((job) => {
    if (!matchesState(job, filters.state)) return false;
    if (filters.project && filters.project !== "all" && job.project !== filters.project) return false;
    if (filters.view && filters.view !== "all" && job.view_mode !== filters.view) return false;
    if (!q) return true;
    return [job.title, job.project, job.last_action, job.grok_session_id]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(q));
  });
}

export function dashboardActive(state: string): boolean {
  return ACTIVE.includes(state as JobState);
}

export function dashboardHistory(state: string): boolean {
  return HISTORY.includes(state as JobState);
}

export function activeGroup(state: string): number {
  return dashboardActive(state) ? 1 : 0;
}

function unixSeconds(iso: string): number {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return 0;
  return Math.floor(t / 1000);
}

export function compareJobs(a: Job, b: Job): number {
  const group = activeGroup(b.state) - activeGroup(a.state);
  if (group !== 0) return group;
  const updated = unixSeconds(b.updated_at) - unixSeconds(a.updated_at);
  if (updated !== 0) return updated;
  const created = unixSeconds(b.created_at) - unixSeconds(a.created_at);
  if (created !== 0) return created;
  if (b.job_id === a.job_id) return 0;
  return b.job_id > a.job_id ? 1 : -1;
}

export function sortJobs(jobs: Job[]): Job[] {
  return [...jobs].sort(compareJobs);
}

export function mergeJobs(loaded: Job[], incoming: Job[]): Job[] {
  const map = new Map<string, Job>();
  for (const job of loaded) map.set(job.job_id, job);
  for (const job of incoming) map.set(job.job_id, job);
  return sortJobs([...map.values()]);
}

export function canArchive(job: Job): boolean {
  return !job.archived_at && !dashboardActive(job.state);
}

export function canDelete(job: Job): boolean {
  return !dashboardActive(job.state);
}

export function summarizeStatus(jobs: Job[]) {
  let working = 0;
  let needsInput = 0;
  let disconnected = 0;
  let failed = 0;
  for (const job of jobs) {
    if (job.state === "planning" || job.state === "executing" || job.state === "starting" || job.state === "recovering") {
      working += 1;
    } else if (job.state === "needs_input" || job.state === "plan_ready") {
      needsInput += 1;
    } else if (job.state === "disconnected") {
      disconnected += 1;
    } else if (job.state === "failed") {
      failed += 1;
    }
  }
  return { working, needsInput, disconnected, failed };
}

export function projectOptions(jobs: Job[]): string[] {
  return [...new Set(jobs.map((j) => j.project).filter(Boolean))].sort();
}

const TURN_CONTROL: JobState[] = [
  "needs_input",
  "blocked",
  "executing",
  "planning",
  "starting",
  "recovering",
  "disconnected",
];

export function canControlTurn(job: Job): boolean {
  if (job.user_cancelled) return false;
  return TURN_CONTROL.includes(job.state) || job.pause_reason === "approval_expired";
}

export function waitReasonLabel(reason?: string): string {
 const labels: Record<string, string> = {
   request_not_sent: "请求尚未发送，等待手动恢复", execution_unknown: "原调用结果未知，请在原会话核对后继续", approval: "等待方案审批", approval_delivery: "审批已提交，等待 Grok 确认", approval_delivery_unknown: "审批交付未确认，不会自动重发", approval_expired: "原审批已失效，请继续原会话重新提交", plan_content_missing: "已收到审批请求，但本次方案正文缺失", review_required: "最终回答待验收", tui_active: "TUI 正在控制交互会话", handoff: "正在连接交互会话",
   queued: "等待队列执行", running: "后台执行中", no_recent_activity: "暂未收到活动（不代表卡死）",
   disconnected: "连接已断开，等待手动恢复", queue_nonempty_but_pump_idle: "队列未被调度",
   stale_tui_owner: "交互进程已退出，但输入控制权尚未归还", detach_timeout: "等待输入控制权交接",
   executing_without_turn: "执行阶段没有活动调用或排队请求",
 };
 return reason ? labels[reason] ?? reason : "—";
}
