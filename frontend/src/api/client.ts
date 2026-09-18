import type { Job } from "../lib/jobs";

export type Settings = {
  grok_binary_path: string;
  terminal_provider: string;
  terminal_command_template: string;
  default_view_mode: string;
};

export type Diagnose = {
  grok_path: string;
  grok_version: string;
  logged_in: boolean;
  leader_running: boolean;
  leader_socket: string;
  compatible: boolean;
  attach_mode: string;
  error?: string;
};

export type InstallResult = {
  ok: boolean;
  grok_path: string;
  log: string;
};

export type StatusBar = {
  leader_ok: boolean;
  mcp_ok: boolean;
  working: number;
  needs_input: number;
};

export type BoundaryEvent = {
  id: number;
  job_id: string;
  event_type: string;
  summary: string;
  created_at: string;
};

export type UpdateRelease = {
  version: string;
  name?: string;
  notes?: string;
};

export type Client = {
  listJobs(): Promise<Job[]>;
  status(jobId: string): Promise<Job>;
  events(jobId: string): Promise<BoundaryEvent[]>;
  statusBar(): Promise<StatusBar>;
  setView(jobId: string, view: string): Promise<Job>;
  cancelTurn(jobId: string): Promise<Job>;
  continueJob(jobId: string): Promise<Job>;
  planDecide(jobId: string, decide: "approve" | "revise" | "cancel", notes?: string): Promise<Job>;
  openTerminal(jobId: string, dashboard?: boolean): Promise<void>;
  openProject(jobId: string): Promise<void>;
  settings(): Promise<Settings>;
  saveSettings(s: Settings): Promise<void>;
  diagnose(): Promise<Diagnose>;
  installGrok(): Promise<InstallResult>;
  testTerminal(template: string): Promise<void>;
  appVersion(): Promise<string>;
  checkUpdate(): Promise<UpdateRelease | null>;
  downloadUpdate(): Promise<void>;
  restartUpdate(): Promise<void>;
  subscribe(fn: () => void): () => void;
};
