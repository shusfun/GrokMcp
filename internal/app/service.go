package app

import (
	"context"

	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
)

type Service struct {
	Backend core.Backend
	quit    func()
}

func (s *Service) ListJobs(ctx context.Context) ([]protocol.Job, error) {
	return s.Backend.ListJobs(ctx)
}

func (s *Service) Status(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.Status(ctx, jobID)
}

func (s *Service) Events(ctx context.Context, jobID string) ([]protocol.BoundaryEvent, error) {
	return s.Backend.Events(ctx, jobID)
}

func (s *Service) StatusBar(ctx context.Context) (protocol.StatusBar, error) {
	return s.Backend.StatusBar(ctx)
}

func (s *Service) SetView(ctx context.Context, jobID, view string) (protocol.Job, error) {
	return s.Backend.SetView(ctx, protocol.SetViewRequest{JobID: jobID, View: protocol.ViewMode(view)})
}

func (s *Service) CancelTurn(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.CancelTurn(ctx, jobID)
}

func (s *Service) Continue(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.Continue(ctx, jobID)
}

func (s *Service) PlanDecide(ctx context.Context, jobID, decide, notes string) (protocol.Job, error) {
	return s.Backend.PlanDecide(ctx, protocol.PlanDecideRequest{
		JobID: jobID, Decide: protocol.PlanDecision(decide), Notes: notes,
	})
}

func (s *Service) OpenTerminal(ctx context.Context, jobID string, dashboard bool) error {
	return s.Backend.OpenTerminal(ctx, protocol.OpenTerminalRequest{JobID: jobID, Dashboard: dashboard})
}

func (s *Service) OpenProject(ctx context.Context, jobID string) error {
	return s.Backend.OpenProject(ctx, jobID)
}

func (s *Service) Settings(ctx context.Context) (protocol.Settings, error) {
	return s.Backend.Settings(ctx)
}

func (s *Service) SaveSettings(ctx context.Context, st protocol.Settings) error {
	return s.Backend.SaveSettings(ctx, st)
}

func (s *Service) Diagnose(ctx context.Context) (protocol.DiagnoseResult, error) {
	return s.Backend.Diagnose(ctx)
}

func (s *Service) TestTerminal(ctx context.Context, template string) error {
	return s.Backend.TestTerminal(ctx, template)
}

func (s *Service) DetachView(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.DetachView(ctx, jobID)
}

func (s *Service) Quit(context.Context) {
	if s.quit != nil {
		s.quit()
	}
}
