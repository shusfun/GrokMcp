import type { Job, JobPage, ListJobsQuery } from "../lib/jobs";
import type { Project, PromptResult } from "../lib/projects";

export type Settings = {
  grok_binary_path: string;
  terminal_provider: string;
  terminal_command_template: string;
  default_view_mode: string;
  debug_enabled?: boolean;
  debug_payloads?: boolean;
};

export type MCPConfigBundle = {
  server_id: string;
  exe: string;
  args: string[];
  startup_timeout_sec: number;
  tool_timeout_sec: number;
  json: string;
  update_json: string;
  deep_link: string;
  codex_add_command: string;
  toml: string;
  platform?: string;
  deep_link_supported?: boolean;
};

export type MCPCCSwitchStatus = {
  detected: string;
  registered: boolean;
  enabled_codex: boolean;
  needs_update: boolean;
  legacy_id?: string;
  matched_id?: string;
  next_step?: string;
  message?: string;
};

export type MCPCodexStatus = {
  cli_found: boolean;
  live_name?: string;
  live_visible: boolean;
  enabled: boolean;
  command_match: boolean;
  timeouts_present: boolean;
  needs_update: boolean;
  error?: string;
};

export type MCPInstallStatus = {
  generated: MCPConfigBundle;
  ccswitch: MCPCCSwitchStatus;
  codex: MCPCodexStatus;
};

export type MCPApplyResult = {
  ok: boolean;
  action: string;
  target: string;
  live_effective: boolean;
  message: string;
  next_step?: string;
};

export type Diagnose = {
  grok_path: string;
  grok_version: string;
  logged_in: boolean;
  leader_running: boolean;
  leader_socket: string;
  compatible: boolean;
  attach_mode: string;
  acp_ok?: boolean;
  error?: string;
};

export type InstallResult = {
  ok: boolean;
  grok_path: string;
  log: string;
};

export type StatusBar = {
  leader_ok: boolean;
  acp_ok?: boolean;
  mcp_ok: boolean;
  db_ok?: boolean;
  debug_enabled?: boolean;
  stalled?: boolean;
  working: number;
  needs_input: number;
};

export type TraceEvent = {
  seq: number;
  time: string;
  level: string;
  source: string;
  event: string;
  job_id?: string;
  session_id?: string;
  turn_id?: string;
  state?: string;
  view_mode?: string;
  input_owner?: string;
  busy?: boolean;
  queue_length?: number;
  message?: string;
  fields?: Record<string, unknown>;
};

export type DebugSnapshot = {
  job_id: string;
  cursor: number;
  events: TraceEvent[];
};

export type DebugExport = {
  path: string;
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
  listJobsPage(q: ListJobsQuery): Promise<JobPage>;
  importProject(path: string, installSkill?: boolean): Promise<Project>;
  listProjects(): Promise<Project[]>;
  getProject(projectId: string): Promise<Project>;
  removeProject(projectId: string): Promise<Project>;
  skillStatus(projectId: string): Promise<Project>;
  skillInstall(projectId: string): Promise<Project>;
  skillUpdate(projectId: string): Promise<Project>;
  skillRemove(projectId: string): Promise<Project>;
  generatePrompt(projectId: string, goal?: string, constraints?: string, acceptance?: string): Promise<PromptResult>;
  savePrompt(projectId: string, template: string): Promise<Project>;
  openProjectDir(projectId: string): Promise<void>;
  archiveJob(jobId: string): Promise<Job>;
  unarchiveJob(jobId: string): Promise<Job>;
  deleteJob(jobId: string): Promise<void>;
  status(jobId: string): Promise<Job>;
  readResult(jobId: string, requestId: string, turnId?: string, offset?: number): Promise<Job>;
  events(jobId: string): Promise<BoundaryEvent[]>;
  statusBar(): Promise<StatusBar>;
  setView(jobId: string, view: string): Promise<Job>;
  cancelTurn(jobId: string, turnId?: string): Promise<Job>;
  continueJob(jobId: string): Promise<Job>;
  planDecide(jobId: string, decide: "approve" | "revise" | "cancel", notes?: string, binding?: Pick<Job, "approval_id" | "request_id" | "plan_turn_id" | "plan_version">): Promise<Job>;
  openTerminal(jobId: string, dashboard?: boolean): Promise<void>;
  openProject(jobId: string): Promise<void>;
  settings(): Promise<Settings>;
  saveSettings(s: Settings): Promise<void>;
  diagnose(): Promise<Diagnose>;
  mcpConfig(): Promise<MCPConfigBundle>;
  mcpStatus(): Promise<MCPInstallStatus>;
  openCCSwitchMCPImport(): Promise<MCPApplyResult>;
  openCCSwitchApp(): Promise<MCPApplyResult>;
  addMCPToCodex(): Promise<MCPApplyResult>;
  installGrok(): Promise<InstallResult>;
  testTerminal(template: string): Promise<void>;
  debugSet(jobId: string, enabled: boolean, payloads?: boolean): Promise<Job>;
  debugSnapshot(jobId: string, cursor?: number, limit?: number, levels?: string[], sources?: string[]): Promise<DebugSnapshot>;
  debugExport(jobId: string): Promise<DebugExport>;
  appVersion(): Promise<string>;
  checkUpdate(): Promise<UpdateRelease | null>;
  downloadUpdate(): Promise<void>;
  restartUpdate(): Promise<void>;
  subscribe(fn: () => void): () => void;
};
