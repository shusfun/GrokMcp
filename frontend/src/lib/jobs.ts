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
  terminal_pid?: number;
  terminal_window_id?: string;
  stalled?: boolean;
  stalled_reason?: string;
  elapsed_seconds: number;
  created_at: string;
  updated_at: string;
};

export type JobFilters = {
  query: string;
  state: string;
  project: string;
  view: string;
};

const STAGE: Record<JobState, string> = {
  created: "已创建",
  starting: "启动中",
  planning: "规划中",
  plan_ready: "Plan 就绪",
  executing: "实施中",
  completed: "已完成",
  needs_input: "需要输入",
  disconnected: "已断连",
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

const STATE_RANK: Record<string, number> = {
  needs_input: 0,
  plan_ready: 1,
  blocked: 2,
  executing: 3,
  planning: 4,
  starting: 5,
  recovering: 6,
  disconnected: 7,
  failed: 8,
  cancelled: 9,
  completed: 10,
  created: 11,
};

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

export function sortJobs(jobs: Job[]): Job[] {
  return [...jobs].sort((a, b) => {
    const rank = (STATE_RANK[a.state] ?? 50) - (STATE_RANK[b.state] ?? 50);
    if (rank !== 0) return rank;
    return b.elapsed_seconds - a.elapsed_seconds;
  });
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
  return TURN_CONTROL.includes(job.state);
}
