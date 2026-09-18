package app

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"grokmcp/internal/core"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/protocol"
	"grokmcp/internal/version"
)

type Service struct {
	Backend core.Backend
	quit    func()
	updater *updater.Updater
}

func (s *Service) ListJobs(ctx context.Context) ([]protocol.Job, error) {
	return s.Backend.ListJobs(ctx)
}

func (s *Service) ListJobsPage(ctx context.Context, cursor string, limit int, includeArchived bool, query, state, project, view string) (protocol.JobPage, error) {
	return s.Backend.ListJobsPage(ctx, protocol.ListJobsQuery{
		Cursor: cursor, Limit: limit, IncludeArchived: includeArchived,
		Query: query, State: state, Project: project, View: view,
	})
}

func (s *Service) ListProjects(ctx context.Context, includeArchived bool) ([]string, error) {
	return s.Backend.ListProjects(ctx, includeArchived)
}

func (s *Service) ArchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.ArchiveJob(ctx, jobID)
}

func (s *Service) UnarchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.UnarchiveJob(ctx, jobID)
}

func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	return s.Backend.DeleteJob(ctx, jobID)
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

func (s *Service) InstallGrok(ctx context.Context) (protocol.InstallResult, error) {
	log, err := (grokbin.Installer{}).Install(ctx)
	res := protocol.InstallResult{Log: log}
	if err != nil {
		if res.Log != "" {
			res.Log += "\n"
		}
		res.Log += err.Error()
		return res, nil
	}
	d, dErr := s.Backend.Diagnose(ctx)
	if dErr != nil {
		if res.Log != "" {
			res.Log += "\n"
		}
		res.Log += dErr.Error()
	}
	res.GrokPath = d.GrokPath
	res.OK = d.GrokPath != ""
	if !res.OK {
		if res.Log != "" {
			res.Log += "\n"
		}
		res.Log += "安装结束但仍未找到 grok。"
	}
	return res, nil
}

func (s *Service) TestTerminal(ctx context.Context, template string) error {
	return s.Backend.TestTerminal(ctx, template)
}

func (s *Service) DebugSet(ctx context.Context, jobID string, enabled, payloads bool) (protocol.Job, error) {
	return s.Backend.DebugSet(ctx, protocol.DebugSetRequest{JobID: jobID, Enabled: enabled, Payloads: payloads})
}

func (s *Service) DebugSnapshot(ctx context.Context, jobID string, cursor int64, limit int, levels, sources []string) (protocol.DebugSnapshot, error) {
	return s.Backend.DebugSnapshot(ctx, protocol.DebugSnapshotRequest{
		JobID: jobID, Cursor: cursor, Limit: limit, Levels: levels, Sources: sources,
	})
}

func (s *Service) DebugExport(ctx context.Context, jobID string) (protocol.DebugExportResult, error) {
	return s.Backend.DebugExport(ctx, jobID)
}

func (s *Service) DetachView(ctx context.Context, jobID string) (protocol.Job, error) {
	return s.Backend.DetachView(ctx, jobID)
}

func (s *Service) Quit(context.Context) {
	if s.quit != nil {
		s.quit()
	}
}

func (s *Service) AppVersion(context.Context) string {
	return version.Version
}

func (s *Service) CheckUpdate(ctx context.Context) (*updater.Release, error) {
	if s.updater == nil {
		return nil, fmt.Errorf("updater not configured")
	}
	return s.updater.Check(ctx)
}

func (s *Service) DownloadUpdate(context.Context) error {
	if s.updater == nil {
		return fmt.Errorf("updater not configured")
	}
	go func() {
		_ = s.updater.DownloadAndInstall(context.Background())
	}()
	return nil
}

func (s *Service) RestartUpdate(ctx context.Context) error {
	if s.updater == nil {
		return fmt.Errorf("updater not configured")
	}
	return s.updater.Restart(ctx)
}
