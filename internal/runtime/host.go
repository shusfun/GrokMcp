package runtime

import (
	"context"

	"grokmcp/internal/integration"
	"grokmcp/internal/protocol"
	"grokmcp/internal/supervisor"
)

type host struct {
	*supervisor.Service
	integ *integration.Service
}

func (h *host) MCPConfig(ctx context.Context) (protocol.MCPConfigBundle, error) {
	return h.integ.Config(ctx)
}

func (h *host) MCPStatus(ctx context.Context) (protocol.MCPInstallStatus, error) {
	return h.integ.Status(ctx)
}

func (h *host) OpenCCSwitchMCPImport(ctx context.Context) (protocol.MCPApplyResult, error) {
	return h.integ.OpenCCSwitchMCPImport(ctx)
}

func (h *host) OpenCCSwitchApp(ctx context.Context) (protocol.MCPApplyResult, error) {
	return h.integ.OpenCCSwitchApp(ctx)
}

func (h *host) AddMCPToCodex(ctx context.Context) (protocol.MCPApplyResult, error) {
	return h.integ.AddMCPToCodex(ctx)
}
