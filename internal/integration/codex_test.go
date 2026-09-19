package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"grokmcp/internal/protocol"
)

type fakeCodex struct {
	list    []codexServer
	calls   [][]string
	fail    map[string]string
	removed bool
}

func (f *fakeCodex) run(_ context.Context, _ string, args ...string) (string, string, error) {
	cp := append([]string(nil), args...)
	f.calls = append(f.calls, cp)
	if len(args) >= 2 && args[0] == "mcp" {
		switch args[1] {
		case "list":
			b, _ := json.Marshal(f.list)
			return string(b), "", nil
		case "add":
			if msg := f.fail["add"]; msg != "" {
				return "", msg, errString(msg)
			}
			name := args[2]
			exe := ""
			var mcpArgs []string
			for i, a := range args {
				if a == "--" && i+1 < len(args) {
					exe = args[i+1]
					mcpArgs = args[i+2:]
					break
				}
			}
			f.list = append(f.list, codexServer{
				Name:    name,
				Enabled: true,
				Transport: &codexTransport{
					Type:    "stdio",
					Command: exe,
					Args:    mcpArgs,
				},
			})
			return "", "", nil
		case "remove":
			if msg := f.fail["remove"]; msg != "" {
				return "", msg, errString(msg)
			}
			name := args[2]
			out := f.list[:0]
			for _, s := range f.list {
				if s.Name != name {
					out = append(out, s)
				}
			}
			f.list = out
			f.removed = true
			return "", "", nil
		}
	}
	return "", "unknown", errString("unknown")
}

func TestAddCodexWhenMissing(t *testing.T) {
	fake := &fakeCodex{}
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Action != protocol.MCPActionCreated || !res.LiveEffective {
		t.Fatalf("%#v", res)
	}
	if len(fake.calls) < 2 || strings.Join(fake.calls[1], " ") != "mcp add grok_supervisor -- /tmp/GrokMcp mcp" {
		t.Fatalf("calls %#v", fake.calls)
	}
}

func TestFindCodexAliasCoversStableAndLegacyNames(t *testing.T) {
	exe := "/tmp/GrokMcp"
	old := "/old/GrokMcp"
	got, ok := findCodexAlias([]codexServer{{
		Name:      protocol.MCPServerLegacyID,
		Transport: &codexTransport{Command: old, Args: []string{"mcp"}},
	}}, exe)
	if !ok || got.Name != protocol.MCPServerLegacyID {
		t.Fatalf("legacy old path missed: ok=%v %#v", ok, got)
	}
	got, ok = findCodexAlias([]codexServer{{
		Name:      protocol.MCPServerID,
		Transport: &codexTransport{Command: old, Args: []string{"mcp"}},
	}}, exe)
	if !ok || got.Name != protocol.MCPServerID {
		t.Fatalf("stable old path missed: ok=%v %#v", ok, got)
	}
	got, ok = findCodexAlias([]codexServer{
		{Name: "other", Transport: &codexTransport{Command: "/bin/true"}},
		{Name: protocol.MCPServerID, Transport: &codexTransport{Command: exe, Args: []string{"mcp"}}},
	}, exe)
	if !ok || got.Name != protocol.MCPServerID {
		t.Fatalf("stable current path missed: ok=%v %#v", ok, got)
	}
}

func TestAddCodexDoesNotDuplicateAlias(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name:    protocol.MCPServerLegacyID,
		Enabled: true,
		Transport: &codexTransport{
			Command: exe,
			Args:    []string{"mcp"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionUnchanged || !res.LiveEffective {
		t.Fatalf("%#v", res)
	}
	for _, c := range fake.calls {
		if len(c) > 1 && c[1] == "add" {
			t.Fatalf("should not add: %#v", fake.calls)
		}
	}
}

func TestAddCodexMatchingDisabledNeedsManual(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name:    protocol.MCPServerLegacyID,
		Enabled: false,
		Transport: &codexTransport{
			Command: exe,
			Args:    []string{"mcp"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionNeedsManual || res.LiveEffective || fake.removed {
		t.Fatalf("%#v removed=%v", res, fake.removed)
	}
	if !strings.Contains(res.Message, "被禁用") || !strings.Contains(res.NextStep, "启用") {
		t.Fatalf("message %#v", res)
	}
	for _, c := range fake.calls {
		if len(c) > 1 && (c[1] == "add" || c[1] == "remove") {
			t.Fatalf("must not add or remove disabled server: %#v", fake.calls)
		}
	}
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Codex.LiveVisible || !st.Codex.CommandMatch || st.Codex.Enabled {
		t.Fatalf("status %#v", st.Codex)
	}
}

func TestAddCodexMatchingEnabledMissingTimeoutsUnchanged(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name:    protocol.MCPServerID,
		Enabled: true,
		Transport: &codexTransport{
			Command: exe,
			Args:    []string{"mcp"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionUnchanged || !res.LiveEffective || fake.removed {
		t.Fatalf("%#v removed=%v", res, fake.removed)
	}
	if !strings.Contains(res.NextStep, "TOML") {
		t.Fatalf("want TOML hint: %#v", res)
	}
}

func TestProbeCodexOldPathNeedsUpdateNotMissed(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name:      protocol.MCPServerLegacyID,
		Enabled:   true,
		Transport: &codexTransport{Command: "/old/GrokMcp", Args: []string{"mcp"}},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Codex.LiveVisible || st.Codex.LiveName != protocol.MCPServerLegacyID || st.Codex.CommandMatch || !st.Codex.NeedsUpdate {
		t.Fatalf("%#v", st.Codex)
	}
}

func TestAddCodexDoesNotRemoveWhenTimeoutsPresent(t *testing.T) {
	exe := "/tmp/GrokMcp"
	sec := float64(30)
	tool := float64(21600)
	fake := &fakeCodex{list: []codexServer{{
		Name:              protocol.MCPServerLegacyID,
		Enabled:           true,
		StartupTimeoutSec: &sec,
		ToolTimeoutSec:    &tool,
		Transport: &codexTransport{
			Command: "/old/GrokMcp",
			Args:    []string{"mcp"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionNeedsManual || fake.removed {
		t.Fatalf("action=%s removed=%v res=%#v", res.Action, fake.removed, res)
	}
}

func TestAddCodexDoesNotRemoveWhenEnvPresent(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name: protocol.MCPServerID,
		Transport: &codexTransport{
			Command: "/old/GrokMcp",
			Args:    []string{"mcp"},
			Env:     map[string]string{"FOO": "1"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionNeedsManual || fake.removed {
		t.Fatalf("%#v removed=%v", res, fake.removed)
	}
}

func TestAddCodexLosslessRemoveAdd(t *testing.T) {
	exe := "/tmp/GrokMcp"
	fake := &fakeCodex{list: []codexServer{{
		Name: protocol.MCPServerID,
		Transport: &codexTransport{
			Command: "/old/GrokMcp",
			Args:    []string{"mcp"},
		},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionUpdated || !fake.removed {
		t.Fatalf("%#v removed=%v calls=%#v", res, fake.removed, fake.calls)
	}
	var sawAdd bool
	for _, c := range fake.calls {
		if len(c) > 1 && c[1] == "add" && c[2] == protocol.MCPServerID {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatalf("expected add after remove: %#v", fake.calls)
	}
}

func TestAddCodexMissingCLI(t *testing.T) {
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("not found") },
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Action != protocol.MCPActionFailed {
		t.Fatalf("%#v", res)
	}
}

func TestAddCodexCapturesCLIError(t *testing.T) {
	fake := &fakeCodex{fail: map[string]string{"add": "boom"}}
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	res, err := s.AddMCPToCodex(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Message, "boom") {
		t.Fatalf("%#v", res)
	}
}

func TestProbeCodexStatusLayers(t *testing.T) {
	exe := "/tmp/GrokMcp"
	sec := float64(30)
	tool := float64(21600)
	fake := &fakeCodex{list: []codexServer{{
		Name:              protocol.MCPServerLegacyID,
		Enabled:           true,
		StartupTimeoutSec: &sec,
		ToolTimeoutSec:    &tool,
		Transport:         &codexTransport{Command: exe, Args: []string{"mcp"}},
	}}}
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: t.TempDir() + "/missing.db",
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "/fake/codex", nil },
		Run:        fake.run,
	})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Codex.CLIFound || !st.Codex.LiveVisible || !st.Codex.CommandMatch || !st.Codex.Enabled || !st.Codex.TimeoutsPresent || st.Codex.NeedsUpdate {
		t.Fatalf("%#v", st.Codex)
	}
	if st.CCSwitch.Registered {
		t.Fatal("ccswitch missing db should not be registered")
	}
}
