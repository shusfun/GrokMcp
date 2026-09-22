package agent

import (
	"context"
	"errors"

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
	Prompt(ctx context.Context, sessionID, text string) (PromptResult, error)
	Cancel(ctx context.Context, sessionID string) error
	ResolvePlan(ctx context.Context, sessionID string, decide protocol.PlanDecision, notes string) error
	SetPlanListener(fn func(sessionID, excerpt string))
	AttachMode() string
	Close() error
}
