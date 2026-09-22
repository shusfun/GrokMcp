package agent

import (
	"context"
	"errors"
	"time"

	"grokmcp/internal/protocol"
)

type PromptResult struct {
	Text       string
	StopReason string
	PlanReady  bool
	LastAction string
}

var ErrNoPlanPermission = errors.New("no pending plan permission")
var ErrDisconnected = errors.New("Grok 连接已断开，请手动继续")

type ConnectionState struct {
	LeaderRunning bool
	ACPOK         bool
	Connecting    bool
}

type Agent interface {
	ConnectionState() ConnectionState
	SessionLoaded(sessionID string) bool
	Release() error
	Diagnose(ctx context.Context) protocol.DiagnoseResult
	EnsureLeader(ctx context.Context) error
	NewSession(ctx context.Context, cwd string, worktree bool) (sessionID, effectiveCwd string, err error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	LoadConnectedSession(ctx context.Context, sessionID, cwd string) error
	InvalidateSession(sessionID string)
	PlanSession(ctx context.Context, sessionID string) error
	Prompt(ctx context.Context, sessionID, text string) (PromptResult, error)
	Cancel(ctx context.Context, sessionID string) error
	PendingApproval(id string) (PlanRequest, error)
	ResolveApproval(ctx context.Context, req PlanDecision) error
	SetPlanHandler(func(PlanRequest) error)
	SetActivityHandler(func(Activity))
	SetDisconnectListener(func())
	AttachMode() string
	Close() error
}

type PlanRequest struct {
	ID, ConnectionID, SessionID, RequestID, TurnID, Content string
	SupportsNotes                                           bool
	CreatedAt                                               time.Time
}
type PlanDecision struct {
	PlanRequest
	Decide protocol.PlanDecision
	Notes  string
}
type Activity struct {
	ConnectionID, SessionID, RequestID, TurnID, Kind string
	At                                               time.Time
}
