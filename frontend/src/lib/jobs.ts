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
  busy?: boolean;
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

export function filterJobs(jobs: Job[], filters: JobFilters): Job[] {
  const q = filters.query.trim().toLowerCase();
  return jobs.filter((job) => {
    if (filters.state && filters.state !== "all" && job.state !== filters.state) return false;
    if (filters.project && filters.project !== "all" && job.project !== filters.project) return false;
    if (filters.view && filters.view !== "all" && job.view_mode !== filters.view) return false;
    if (!q) return true;
    return [job.title, job.project, job.last_action, job.grok_session_id]
      .filter(Boolean)
      .some((v) => String(v).toLowerCase().includes(q));
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
