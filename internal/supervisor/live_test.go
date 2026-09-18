package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func TestLiveHeadedRequiresPIDWindowTTY(t *testing.T) {
	if testing.Short() || os.Getenv("GROK_LIVE") != "1" || goruntime.GOOS != "darwin" {
		t.Skip("set GROK_LIVE=1 on macOS to open Terminal.app")
	}
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	stub := writeLiveStub(t)
	s := newTest(t, fake, terminal.Exec{})
	s.SetGrokPath(func() string { return stub })
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "live tty"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewHeaded || j.TerminalPID <= 0 || j.TerminalWindowID == "" {
		t.Fatalf("headed not verified %+v", j)
	}
	_, _ = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless})
}

func TestLiveNoDuplicateResumeSameSession(t *testing.T) {
	if testing.Short() || os.Getenv("GROK_LIVE") != "1" || goruntime.GOOS != "darwin" {
		t.Skip("set GROK_LIVE=1 on macOS to open Terminal.app")
	}
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	stub := writeLiveStub(t)
	s := newTest(t, fake, terminal.Exec{})
	s.SetGrokPath(func() string { return stub })
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "live dup"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	j := waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if n := countResume(j.GrokSessionID); n != 1 {
		t.Fatalf("resume count %d", n)
	}
	_, _ = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless})
}

func writeLiveStub(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grokstub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 86400\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func countResume(sessionID string) int {
	out, err := exec.Command("ps", "-ax", "-o", "command=").Output()
	if err != nil {
		return 0
	}
	needle := "--resume " + sessionID
	n := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, needle) {
			n++
		}
	}
	return n
}
