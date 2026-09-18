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

type Agent interface {
	Diagnose(ctx context.Context) protocol.DiagnoseResult
	EnsureLeader(ctx context.Context) error
	NewSession(ctx context.Context, cwd string, worktree bool) (sessionID, effectiveCwd string, err error)
	LoadSession(ctx context.Context, sessionID, cwd string) error
	Prompt(ctx context.Context, sessionID, text string) (PromptResult, error)
	Cancel(ctx context.Context, sessionID string) error
	ResolvePlan(ctx context.Context, sessionID string, decide protocol.PlanDecision, notes string) error
	SetPlanListener(fn func(sessionID, excerpt string))
	AttachMode() string
	Close() error
}
