package protocol

import "time"

type JobState string

const (
	StateCreated      JobState = "created"
	StateStarting     JobState = "starting"
	StatePlanning     JobState = "planning"
	StatePlanReady    JobState = "plan_ready"
	StateExecuting    JobState = "executing"
	StateCompleted    JobState = "completed"
	StateNeedsInput   JobState = "needs_input"
	StateDisconnected JobState = "disconnected"
	StateRecovering   JobState = "recovering"
	StateCancelled    JobState = "cancelled"
	StateBlocked      JobState = "blocked"
	StateFailed       JobState = "failed"
)

func (s JobState) IsBoundary() bool {
	switch s {
	case StatePlanReady, StateNeedsInput, StateCompleted, StateBlocked, StateFailed, StateDisconnected, StateCancelled:
		return true
	default:
		return false
	}
}

func (s JobState) IsActive() bool {
	switch s {
	case StateStarting, StatePlanning, StateExecuting, StateRecovering:
		return true
	default:
		return false
	}
}

type ViewMode string

const (
	ViewHeadless  ViewMode = "headless"
	ViewAttaching ViewMode = "attaching"
	ViewHeaded    ViewMode = "headed"
	ViewDetaching ViewMode = "detaching"
)

type InputOwner string

const (
	OwnerSupervisor InputOwner = "supervisor"
	OwnerTUI        InputOwner = "tui"
)

type TaskMarker string

const (
	MarkerWorking    TaskMarker = "working"
	MarkerNeedsInput TaskMarker = "needs_input"
	MarkerCompleted  TaskMarker = "completed"
	MarkerBlocked    TaskMarker = "blocked"
)

type Job struct {
	JobID            string     `json:"job_id"`
	CodexThreadID    string     `json:"codex_thread_id,omitempty"`
	GrokSessionID    string     `json:"grok_session_id,omitempty"`
	Cwd              string     `json:"cwd"`
	Project          string     `json:"project"`
	Title            string     `json:"title"`
	State            JobState   `json:"state"`
	ViewMode         ViewMode   `json:"view_mode"`
	DesiredViewMode  ViewMode   `json:"desired_view_mode,omitempty"`
	InputOwner       InputOwner `json:"input_owner"`
	PlanDigest       string     `json:"plan_digest,omitempty"`
	PlanSummary      string     `json:"plan_summary,omitempty"`
	LastAction       string     `json:"last_action,omitempty"`
	LastSummary      string     `json:"last_summary,omitempty"`
	Busy             bool       `json:"busy"`
	UserCancelled    bool       `json:"user_cancelled"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ArchivedAt       time.Time  `json:"archived_at,omitempty"`
	ElapsedSeconds   int64      `json:"elapsed_seconds"`
	DebugEnabled     bool       `json:"debug_enabled,omitempty"`
	DebugCursor      int64      `json:"debug_cursor,omitempty"`
	QueueLength      int        `json:"queue_length,omitempty"`
	ActiveTurnID     string     `json:"active_turn_id,omitempty"`
	TerminalPID      int        `json:"terminal_pid,omitempty"`
	TerminalWindowID string     `json:"terminal_window_id,omitempty"`
	Stalled          bool       `json:"stalled,omitempty"`
	StalledReason    string     `json:"stalled_reason,omitempty"`
}

type BoundaryEvent struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"job_id"`
	EventType string    `json:"event_type"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

type Settings struct {
	GrokBinaryPath          string `json:"grok_binary_path"`
	TerminalProvider        string `json:"terminal_provider"`
	TerminalCommandTemplate string `json:"terminal_command_template"`
	DefaultViewMode         string `json:"default_view_mode"`
	DebugEnabled            bool   `json:"debug_enabled"`
	DebugPayloads           bool   `json:"debug_payloads"`
}

type DispatchTask struct {
	Title         string `json:"title,omitempty"`
	Prompt        string `json:"prompt"`
	Cwd           string `json:"cwd,omitempty"`
	Worktree      bool   `json:"worktree,omitempty"`
	CodexThreadID string `json:"codex_thread_id,omitempty"`
}

type DispatchRequest struct {
	Tasks []DispatchTask `json:"tasks"`
	Cwd   string         `json:"cwd,omitempty"`
}

type DispatchResult struct {
	Jobs []Job `json:"jobs"`
}

type WaitRequest struct {
	JobIDs     []string `json:"job_ids"`
	Mode       string   `json:"mode,omitempty"`
	TimeoutSec int      `json:"timeout_sec,omitempty"`
}

type WaitResult struct {
	Jobs []Job `json:"jobs"`
}

type PlanDecision string

const (
	PlanApprove PlanDecision = "approve"
	PlanRevise  PlanDecision = "revise"
	PlanCancel  PlanDecision = "cancel"
)

type PlanDecideRequest struct {
	JobID  string       `json:"job_id"`
	Decide PlanDecision `json:"decide"`
	Notes  string       `json:"notes,omitempty"`
}

type FollowupRequest struct {
	JobID  string `json:"job_id"`
	Prompt string `json:"prompt"`
}

type SetViewRequest struct {
	JobID string   `json:"job_id"`
	View  ViewMode `json:"view"`
}

type OpenTerminalRequest struct {
	JobID     string `json:"job_id,omitempty"`
	Dashboard bool   `json:"dashboard,omitempty"`
}

type DebugSetRequest struct {
	JobID    string `json:"job_id,omitempty" jsonschema:"optional job id; empty sets global debug mode"`
	Enabled  bool   `json:"enabled"`
	Payloads bool   `json:"payloads,omitempty"`
}

type DebugSnapshotRequest struct {
	JobID   string   `json:"job_id" jsonschema:"job id"`
	Cursor  int64    `json:"cursor,omitempty"`
	Limit   int      `json:"limit,omitempty"`
	Levels  []string `json:"levels,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

type DebugWaitRequest struct {
	JobID      string `json:"job_id" jsonschema:"job id"`
	Cursor     int64  `json:"cursor"`
	Until      string `json:"until,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type TraceEvent struct {
	Seq         int64          `json:"seq"`
	Time        time.Time      `json:"time"`
	Level       string         `json:"level"`
	Source      string         `json:"source"`
	Event       string         `json:"event"`
	JobID       string         `json:"job_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	TurnID      string         `json:"turn_id,omitempty"`
	State       string         `json:"state,omitempty"`
	ViewMode    string         `json:"view_mode,omitempty"`
	InputOwner  string         `json:"input_owner,omitempty"`
	Busy        *bool          `json:"busy,omitempty"`
	QueueLength *int           `json:"queue_length,omitempty"`
	Message     string         `json:"message,omitempty"`
	Fields      map[string]any `json:"fields,omitempty"`
}

type DebugSnapshot struct {
	JobID  string       `json:"job_id"`
	Cursor int64        `json:"cursor"`
	Events []TraceEvent `json:"events"`
}

type DebugExportResult struct {
	Path string `json:"path"`
}

type DiagnoseResult struct {
	GrokPath      string `json:"grok_path"`
	GrokVersion   string `json:"grok_version"`
	LoggedIn      bool   `json:"logged_in"`
	LeaderRunning bool   `json:"leader_running"`
	LeaderSocket  string `json:"leader_socket"`
	Compatible    bool   `json:"compatible"`
	AttachMode    string `json:"attach_mode"`
	ACPOK         bool   `json:"acp_ok"`
	Error         string `json:"error,omitempty"`
}

type InstallResult struct {
	OK       bool   `json:"ok"`
	GrokPath string `json:"grok_path"`
	Log      string `json:"log"`
}

type StatusBar struct {
	LeaderOK     bool `json:"leader_ok"`
	ACPOK        bool `json:"acp_ok"`
	MCPOK        bool `json:"mcp_ok"`
	DBOK         bool `json:"db_ok"`
	DebugEnabled bool `json:"debug_enabled"`
	Stalled      bool `json:"stalled"`
	Working      int  `json:"working"`
	NeedsInput   int  `json:"needs_input"`
	Disconnected int  `json:"disconnected"`
	Failed       int  `json:"failed"`
}

type Event struct {
	Type  string `json:"type"`
	Job   *Job   `json:"job,omitempty"`
	JobID string `json:"job_id,omitempty"`
}

type ListJobsQuery struct {
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Query           string `json:"query,omitempty"`
	State           string `json:"state,omitempty"`
	Project         string `json:"project,omitempty"`
	View            string `json:"view,omitempty"`
}

type JobPage struct {
	Jobs       []Job  `json:"jobs"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
