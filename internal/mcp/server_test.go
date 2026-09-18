package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
)

type stub struct{ core.Backend }

func (stub) Dispatch(context.Context, protocol.DispatchRequest) (protocol.DispatchResult, error) {
	return protocol.DispatchResult{Jobs: []protocol.Job{{JobID: "j1", State: protocol.StateStarting, ViewMode: protocol.ViewHeadless}}}, nil
}
func (stub) Wait(context.Context, protocol.WaitRequest) (protocol.WaitResult, error) {
	return protocol.WaitResult{}, nil
}
func (stub) PlanDecide(context.Context, protocol.PlanDecideRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) Followup(context.Context, protocol.FollowupRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) CancelTurn(context.Context, string) (protocol.Job, error) { return protocol.Job{}, nil }
func (stub) SetView(context.Context, protocol.SetViewRequest) (protocol.Job, error) {
	return protocol.Job{}, nil
}
func (stub) Status(context.Context, string) (protocol.Job, error) { return protocol.Job{}, nil }
func (stub) ListJobs(context.Context) ([]protocol.Job, error)     { return nil, nil }
func (stub) OpenTerminal(context.Context, protocol.OpenTerminalRequest) error {
	return nil
}
func (stub) OpenProject(context.Context, string) error                  { return nil }
func (stub) Continue(context.Context, string) (protocol.Job, error)     { return protocol.Job{}, nil }
func (stub) DetachView(context.Context, string) (protocol.Job, error)   { return protocol.Job{}, nil }
func (stub) Settings(context.Context) (protocol.Settings, error)        { return protocol.Settings{}, nil }
func (stub) SaveSettings(context.Context, protocol.Settings) error      { return nil }
func (stub) Diagnose(context.Context) (protocol.DiagnoseResult, error) {
	return protocol.DiagnoseResult{}, nil
}
func (stub) StatusBar(context.Context) (protocol.StatusBar, error)      { return protocol.StatusBar{}, nil }
func (stub) Events(context.Context, string) ([]protocol.BoundaryEvent, error) {
	return nil, nil
}
func (stub) TestTerminal(context.Context, string) error { return nil }
func (stub) Subscribe(func(protocol.Event)) func()      { return func() {} }
func (stub) Close() error                               { return nil }

func TestAddTools(t *testing.T) {
	server := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	addTools(server, stub{})
}
