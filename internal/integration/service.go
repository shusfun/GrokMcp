package integration

import (
	"context"
	goruntime "runtime"

	"grokmcp/internal/protocol"
)

type Options struct {
	Executable func() (string, error)
	CCSwitchDB string
	GOOS       string
	LookPath   func(string) (string, error)
	Run        func(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
	OpenURL    func(ctx context.Context, url string) error
	OpenApp    func(ctx context.Context) error
}

type Service struct {
	opts Options
}

func New(opts Options) *Service {
	return &Service{opts: opts}
}

func (s *Service) goos() string {
	if s.opts.GOOS != "" {
		return s.opts.GOOS
	}
	return goruntime.GOOS
}

func (s *Service) exe() (string, error) {
	if s.opts.Executable != nil {
		return s.opts.Executable()
	}
	return "", errNoExecutable
}

var errNoExecutable = errString("cannot resolve supervisor executable")

type errString string

func (e errString) Error() string { return string(e) }

func (s *Service) Config(context.Context) (protocol.MCPConfigBundle, error) {
	exe, err := s.exe()
	if err != nil {
		return protocol.MCPConfigBundle{}, err
	}
	return BuildBundle(exe, s.goos(), protocol.MCPServerID)
}

func (s *Service) Status(ctx context.Context) (protocol.MCPInstallStatus, error) {
	bundle, err := s.Config(ctx)
	if err != nil {
		return protocol.MCPInstallStatus{}, err
	}
	cc := s.probeCCSwitch(ctx, bundle.Exe)
	return protocol.MCPInstallStatus{
		Generated: bundle,
		CCSwitch:  cc,
		Codex:     s.probeCodex(ctx, bundle.Exe),
	}, nil
}
