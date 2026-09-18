package terminal

import "context"

type Handle interface {
	Wait() error
	PID() int
	Close() error
}

type Launcher interface {
	OpenResume(ctx context.Context, grokPath, sessionID, cwd string) (Handle, error)
	FocusResume(ctx context.Context, sessionID string) (bool, error)
	OpenDashboard(ctx context.Context, grokPath, cwd string) error
	OpenDirectory(ctx context.Context, cwd string) error
	TestTemplate(ctx context.Context, template, command string) error
}

type exited struct{}

func (exited) Wait() error  { return nil }
func (exited) PID() int     { return 0 }
func (exited) Close() error { return nil }
