package agent

import (
	"context"
	"os"
	"os/exec"
	"time"

	acp "github.com/coder/acp-go-sdk"
)

func (g *Grok) probeAttach(ctx context.Context) {
	g.mu.Lock()
	if g.attach == "live" || g.attach == "boundary" && g.conn == nil {
		g.mu.Unlock()
		return
	}
	bin := g.Bin
	primary := g.conn
	g.mu.Unlock()
	if bin == "" || primary == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	sess, err := primary.NewSession(ctx, acp.NewSessionRequest{Cwd: cwd, McpServers: []acp.McpServer{}})
	if err != nil {
		g.setAttach("boundary")
		return
	}
	cmd := exec.CommandContext(ctx, bin, "agent", "--always-approve", "--leader", "stdio")
	cmd.Stderr = nil
	stdin, err := cmd.StdinPipe()
	if err != nil {
		g.setAttach("boundary")
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		g.setAttach("boundary")
		return
	}
	if err := cmd.Start(); err != nil {
		g.setAttach("boundary")
		return
	}
	defer func() { _ = cmd.Process.Kill() }()
	second := acp.NewClientSideConnection(newACPClient(), stdin, stdout)
	if _, err := second.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion: acp.ProtocolVersionNumber,
		ClientInfo:      &acp.Implementation{Name: "Grok Supervisor probe", Version: "0.1.0"},
	}); err != nil {
		g.setAttach("boundary")
		return
	}
	_, err = second.LoadSession(ctx, acp.LoadSessionRequest{
		SessionId:  sess.SessionId,
		Cwd:        cwd,
		McpServers: []acp.McpServer{},
	})
	if err != nil {
		g.setAttach("boundary")
		return
	}
	g.setAttach("live")
}

func (g *Grok) setAttach(mode string) {
	g.mu.Lock()
	g.attach = mode
	g.mu.Unlock()
}
