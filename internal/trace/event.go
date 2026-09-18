package trace

import "time"

const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"

	SourceSupervisor = "supervisor"
	SourceFIFO       = "fifo"
	SourceACP        = "acp"
	SourceTerminal   = "terminal"
	SourceMCP        = "mcp"
	SourceLeader     = "leader"
	SourceStore      = "store"

	UntilAny      = "any"
	UntilWarning  = "warning"
	UntilError    = "error"
	UntilBoundary = "boundary"
)

const (
	MaxSnapshotBytes = 256 * 1024
	DefaultLimit     = 200
	MaxLimit         = 1000
)

type Event struct {
	Seq         int64          `json:"seq"`
	Time        time.Time      `json:"time"`
	Level       string         `json:"level"`
	Source      string         `json:"source"`
	Name        string         `json:"event"`
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

func (e Event) Boundary() bool {
	switch e.Name {
	case "plan.ready", "acp.prompt.completed", "acp.prompt.cancelled",
		"state.transition", "watchdog.stalled", "session.load.failed",
		"view.attached", "view.detached":
		return true
	default:
		return false
	}
}

func Bool(v bool) *bool { return &v }

func Int(v int) *int { return &v }
