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
	RequestPhase          string      `json:"request_phase,omitempty"`
	ApprovalID            string      `json:"approval_id,omitempty"`
	ApprovalConnectionID  string      `json:"approval_connection_id,omitempty"`
	ApprovalSupportsNotes bool        `json:"approval_supports_notes"`
	ApprovalDelivery      string      `json:"approval_delivery,omitempty"`
	PauseReason           string      `json:"pause_reason,omitempty"`
	ResultTurnID          string      `json:"result_turn_id,omitempty"`
	Result                *ResultPage `json:"result,omitempty"`

	AcceptedRequestID string   `json:"accepted_request_id,omitempty"`
	QueuedRequestIDs  []string `json:"queued_request_ids,omitempty"`

	RequestID      string    `json:"request_id,omitempty"`
	Approved       bool      `json:"approved"`
	PlanVersion    int64     `json:"plan_version,omitempty"`
	PlanTurnID     string    `json:"plan_turn_id,omitempty"`
	EventCursor    int64     `json:"event_cursor"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	ActivityKind   string    `json:"activity_kind,omitempty"`
	WaitReason     string    `json:"wait_reason,omitempty"`

	JobID            string     `json:"job_id"`
	CodexThreadID    string     `json:"codex_thread_id,omitempty"`
	GrokSessionID    string     `json:"grok_session_id,omitempty"`
	Cwd              string     `json:"cwd"`
	ProjectID        string     `json:"project_id,omitempty"`
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
	ProjectID     string `json:"project_id,omitempty"`
	Worktree      bool   `json:"worktree,omitempty"`
	CodexThreadID string `json:"codex_thread_id,omitempty"`
}

type DispatchRequest struct {
	Tasks     []DispatchTask `json:"tasks"`
	Cwd       string         `json:"cwd,omitempty"`
	ProjectID string         `json:"project_id,omitempty"`
}

type DispatchResult struct {
	Jobs []Job `json:"jobs"`
}

type WaitRequest struct {
	Cursors map[string]int64 `json:"cursors,omitempty" jsonschema:"per-job cursors returned by the previous wait; omit on first wait"`

	JobIDs     []string `json:"job_ids"`
	Mode       string   `json:"mode,omitempty"`
	TimeoutSec int      `json:"timeout_sec,omitempty"`
}

type WaitResult struct {
	Boundaries []Job            `json:"boundaries,omitempty"`
	Reason     string           `json:"reason"`
	Cursors    map[string]int64 `json:"cursors"`

	Jobs []Job `json:"jobs"`
}

type PlanDecision string

const (
	PlanApprove PlanDecision = "approve"
	PlanRevise  PlanDecision = "revise"
	PlanCancel  PlanDecision = "cancel"
)

type PlanDecideRequest struct {
	ApprovalID  string `json:"approval_id"`
	RequestID   string `json:"request_id"`
	TurnID      string `json:"turn_id"`
	PlanVersion int64  `json:"plan_version"`

	JobID  string       `json:"job_id"`
	Decide PlanDecision `json:"decide"`
	Notes  string       `json:"notes,omitempty"`
}

type FollowupRequest struct {
	Replan bool `json:"replan,omitempty" jsonschema:"explicitly request a new plan in the original session"`

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

type SkillStatus string

const (
	SkillMissing   SkillStatus = "missing"
	SkillInstalled SkillStatus = "installed"
	SkillOutdated  SkillStatus = "outdated"
	SkillConflict  SkillStatus = "conflict"
	SkillError     SkillStatus = "error"
)

type Project struct {
	ProjectID       string      `json:"project_id"`
	Name            string      `json:"name"`
	Root            string      `json:"root"`
	CanonicalPath   string      `json:"canonical_path"`
	GitRoot         string      `json:"git_root,omitempty"`
	Imported        bool        `json:"imported"`
	SkillStatus     SkillStatus `json:"skill_status"`
	SkillVersion    string      `json:"skill_version,omitempty"`
	SkillMessage    string      `json:"skill_message,omitempty"`
	PromptTemplate  string      `json:"prompt_template,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	LastUsedAt      time.Time   `json:"last_used_at"`
	ActiveCount     int         `json:"active_count"`
	NeedsInputCount int         `json:"needs_input_count"`
}

type ImportProjectRequest struct {
	Path         string `json:"path"`
	InstallSkill *bool  `json:"install_skill,omitempty" jsonschema:"optional; default true"`
}

type ProjectIDRequest struct {
	ProjectID string `json:"project_id"`
}

type GeneratePromptRequest struct {
	ProjectID   string `json:"project_id"`
	Goal        string `json:"goal,omitempty"`
	Constraints string `json:"constraints,omitempty"`
	Acceptance  string `json:"acceptance,omitempty"`
}

type SavePromptRequest struct {
	ProjectID string `json:"project_id"`
	Template  string `json:"template"`
}

type PromptResult struct {
	ProjectID   string `json:"project_id"`
	Text        string `json:"text"`
	Template    string `json:"template"`
	Builtin     string `json:"builtin"`
	Goal        string `json:"goal,omitempty"`
	Constraints string `json:"constraints,omitempty"`
	Acceptance  string `json:"acceptance,omitempty"`
}

type ListJobsQuery struct {
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
	Query           string `json:"query,omitempty"`
	State           string `json:"state,omitempty"`
	Project         string `json:"project,omitempty" jsonschema:"optional project_id"`
	View            string `json:"view,omitempty"`
}

type JobPage struct {
	Jobs       []Job  `json:"jobs"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

const (
	MCPServerID       = "grok_supervisor"
	MCPServerLegacyID = "Grok Supervisor"

	MCPStartupTimeoutSec = 30
	MCPToolTimeoutSec    = 21600

	MCPDetectDetected          = "detected"
	MCPDetectUnsupportedSchema = "unsupported_schema"
	MCPDetectLocked            = "locked"
	MCPDetectMissing           = "missing"
	MCPDetectReadError         = "read_error"

	MCPActionPendingUser = "pending_user_confirmation"
	MCPActionUnchanged   = "unchanged"
	MCPActionCreated     = "created"
	MCPActionUpdated     = "updated"
	MCPActionFailed      = "failed"
	MCPActionNeedsManual = "needs_manual"

	MCPTargetCCSwitch = "ccswitch"
	MCPTargetCodex    = "codex"
)

type MCPConfigBundle struct {
	ServerID          string   `json:"server_id"`
	Exe               string   `json:"exe"`
	Args              []string `json:"args"`
	StartupTimeoutSec int      `json:"startup_timeout_sec"`
	ToolTimeoutSec    int      `json:"tool_timeout_sec"`
	JSON              string   `json:"json"`
	UpdateJSON        string   `json:"update_json"`
	DeepLink          string   `json:"deep_link"`
	CodexAddCommand   string   `json:"codex_add_command"`
	TOML              string   `json:"toml"`
	Platform          string   `json:"platform,omitempty"`
	DeepLinkSupported bool     `json:"deep_link_supported"`
}

type MCPCCSwitchStatus struct {
	Detected     string `json:"detected"`
	Registered   bool   `json:"registered"`
	EnabledCodex bool   `json:"enabled_codex"`
	NeedsUpdate  bool   `json:"needs_update"`
	LegacyID     string `json:"legacy_id,omitempty"`
	MatchedID    string `json:"matched_id,omitempty"`
	NextStep     string `json:"next_step,omitempty"`
	Message      string `json:"message,omitempty"`
}

type MCPCodexStatus struct {
	CLIFound        bool   `json:"cli_found"`
	LiveName        string `json:"live_name,omitempty"`
	LiveVisible     bool   `json:"live_visible"`
	Enabled         bool   `json:"enabled"`
	CommandMatch    bool   `json:"command_match"`
	TimeoutsPresent bool   `json:"timeouts_present"`
	NeedsUpdate     bool   `json:"needs_update"`
	Error           string `json:"error,omitempty"`
}

type MCPInstallStatus struct {
	Generated MCPConfigBundle   `json:"generated"`
	CCSwitch  MCPCCSwitchStatus `json:"ccswitch"`
	Codex     MCPCodexStatus    `json:"codex"`
}

type MCPApplyResult struct {
	OK            bool   `json:"ok"`
	Action        string `json:"action"`
	Target        string `json:"target"`
	LiveEffective bool   `json:"live_effective"`
	Message       string `json:"message"`
	NextStep      string `json:"next_step,omitempty"`
}

// Offset/Limit 按 Unicode 字符计数，分页不切开 UTF-8 字符。
type ResultQuery struct {
	IncludeResult bool   `json:"include_result,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	TurnID        string `json:"turn_id,omitempty"`
	Offset        int    `json:"offset,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}
type ResultPage struct {
	RequestID  string `json:"request_id"`
	TurnID     string `json:"turn_id"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	NextOffset int    `json:"next_offset"`
	Total      int    `json:"total"`
	HasMore    bool   `json:"has_more"`
}
