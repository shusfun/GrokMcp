package core

import (
	"context"

	"grokmcp/internal/protocol"
)

type Backend interface {
	Dispatch(ctx context.Context, req protocol.DispatchRequest) (protocol.DispatchResult, error)
	Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error)
	PlanDecide(ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error)
	Followup(ctx context.Context, req protocol.FollowupRequest) (protocol.Job, error)
	CancelTurn(ctx context.Context, jobID string) (protocol.Job, error)
	SetView(ctx context.Context, req protocol.SetViewRequest) (protocol.Job, error)
	Status(ctx context.Context, jobID string) (protocol.Job, error)
	ListJobs(ctx context.Context) ([]protocol.Job, error)
	OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error
	OpenProject(ctx context.Context, jobID string) error
	Continue(ctx context.Context, jobID string) (protocol.Job, error)
	DetachView(ctx context.Context, jobID string) (protocol.Job, error)
	Settings(ctx context.Context) (protocol.Settings, error)
	SaveSettings(ctx context.Context, s protocol.Settings) error
	Diagnose(ctx context.Context) (protocol.DiagnoseResult, error)
	StatusBar(ctx context.Context) (protocol.StatusBar, error)
	Events(ctx context.Context, jobID string) ([]protocol.BoundaryEvent, error)
	TestTerminal(ctx context.Context, template string) error
	Subscribe(fn func(protocol.Event)) (unsubscribe func())
	Close() error
}
