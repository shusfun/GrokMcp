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
	ListJobsPage(ctx context.Context, q protocol.ListJobsQuery) (protocol.JobPage, error)
	ImportProject(ctx context.Context, path string, installSkill bool) (protocol.Project, error)
	ListProjects(ctx context.Context) ([]protocol.Project, error)
	GetProject(ctx context.Context, projectID string) (protocol.Project, error)
	RemoveProject(ctx context.Context, projectID string) (protocol.Project, error)
	SkillStatus(ctx context.Context, projectID string) (protocol.Project, error)
	SkillInstall(ctx context.Context, projectID string) (protocol.Project, error)
	SkillUpdate(ctx context.Context, projectID string) (protocol.Project, error)
	SkillRemove(ctx context.Context, projectID string) (protocol.Project, error)
	GeneratePrompt(ctx context.Context, req protocol.GeneratePromptRequest) (protocol.PromptResult, error)
	SavePrompt(ctx context.Context, req protocol.SavePromptRequest) (protocol.Project, error)
	OpenProjectDir(ctx context.Context, projectID string) error
	ArchiveJob(ctx context.Context, jobID string) (protocol.Job, error)
	UnarchiveJob(ctx context.Context, jobID string) (protocol.Job, error)
	DeleteJob(ctx context.Context, jobID string) error
	OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error
	OpenProject(ctx context.Context, jobID string) error
	Continue(ctx context.Context, jobID string) (protocol.Job, error)
	DetachView(ctx context.Context, jobID string) (protocol.Job, error)
	Settings(ctx context.Context) (protocol.Settings, error)
	SaveSettings(ctx context.Context, s protocol.Settings) error
	Diagnose(ctx context.Context) (protocol.DiagnoseResult, error)
	MCPConfig(ctx context.Context) (protocol.MCPConfigBundle, error)
	MCPStatus(ctx context.Context) (protocol.MCPInstallStatus, error)
	OpenCCSwitchMCPImport(ctx context.Context) (protocol.MCPApplyResult, error)
	OpenCCSwitchApp(ctx context.Context) (protocol.MCPApplyResult, error)
	AddMCPToCodex(ctx context.Context) (protocol.MCPApplyResult, error)
	StatusBar(ctx context.Context) (protocol.StatusBar, error)
	Events(ctx context.Context, jobID string) ([]protocol.BoundaryEvent, error)
	TestTerminal(ctx context.Context, template string) error
	DebugSet(ctx context.Context, req protocol.DebugSetRequest) (protocol.Job, error)
	DebugSnapshot(ctx context.Context, req protocol.DebugSnapshotRequest) (protocol.DebugSnapshot, error)
	DebugWait(ctx context.Context, req protocol.DebugWaitRequest) (protocol.DebugSnapshot, error)
	DebugExport(ctx context.Context, jobID string) (protocol.DebugExportResult, error)
	Subscribe(fn func(protocol.Event)) (unsubscribe func())
	Close() error
}
