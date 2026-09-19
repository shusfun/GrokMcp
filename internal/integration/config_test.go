package integration

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"grokmcp/internal/protocol"
)

func TestBuildBundleUnixPath(t *testing.T) {
	exe := `/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp`
	b, err := BuildBundle(exe, "darwin", protocol.MCPServerID)
	if err != nil {
		t.Fatal(err)
	}
	if b.ServerID != protocol.MCPServerID {
		t.Fatalf("server id %q", b.ServerID)
	}
	if !strings.Contains(b.CodexAddCommand, `codex mcp add grok_supervisor -- "`+exe+`" mcp`) {
		t.Fatalf("add command:\n%s", b.CodexAddCommand)
	}
	if strings.Contains(b.CodexAddCommand, `\\`) {
		t.Fatalf("posix add escaped backslashes:\n%s", b.CodexAddCommand)
	}
	if !strings.Contains(b.TOML, `command = `+tomlQuote(exe)) {
		t.Fatalf("toml:\n%s", b.TOML)
	}
	if !strings.Contains(b.TOML, `args = ["mcp"]`) || !strings.Contains(b.TOML, "startup_timeout_sec = 30") || !strings.Contains(b.TOML, "tool_timeout_sec = 21600") {
		t.Fatalf("toml timeouts/args:\n%s", b.TOML)
	}
	var root map[string]map[string]stdioServer
	if err := json.Unmarshal([]byte(b.JSON), &root); err != nil {
		t.Fatal(err)
	}
	srv := root["mcpServers"][protocol.MCPServerID]
	if srv.Command != exe || srv.Type != "stdio" || srv.StartupTimeoutSec != 30 || srv.ToolTimeoutSec != 21600 {
		t.Fatalf("json server %#v", srv)
	}
	if len(srv.Env) != 0 {
		t.Fatalf("env should be empty: %#v", srv.Env)
	}
	if !b.DeepLinkSupported {
		t.Fatal("darwin should support deep link")
	}
}

func TestBuildBundleWindowsPath(t *testing.T) {
	exe := `C:\Users\foo\AppData\Local\Grok Supervisor\GrokMcp.exe`
	b, err := BuildBundle(exe, "windows", protocol.MCPServerID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.CodexAddCommand, `\\`) {
		t.Fatalf("windows add used TOML escapes:\n%s", b.CodexAddCommand)
	}
	if !strings.Contains(b.CodexAddCommand, powershellQuote(exe)) {
		t.Fatalf("missing powershell quote:\n%s", b.CodexAddCommand)
	}
	if !strings.Contains(b.TOML, tomlQuote(exe)) {
		t.Fatalf("toml should escape backslashes:\n%s", b.TOML)
	}
}

func TestPowershellQuoteDoublesSingleQuotes(t *testing.T) {
	exe := `C:\Users\o'brien\GrokMcp.exe`
	if got, want := powershellQuote(exe), `'C:\Users\o''brien\GrokMcp.exe'`; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDeepLinkRoundTrip(t *testing.T) {
	exe := `/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp`
	link, err := DeepLink(protocol.MCPServerID, exe)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "ccswitch" || u.Host != "v1" || u.Path != "/import" {
		t.Fatalf("url %s", link)
	}
	q := u.Query()
	if q.Get("resource") != "mcp" || q.Get("apps") != "codex" {
		t.Fatalf("query %v", q)
	}
	id, command, args, err := ParseDeepLinkConfig(link)
	if err != nil {
		t.Fatal(err)
	}
	if id != protocol.MCPServerID || command != exe || strings.Join(args, ",") != "mcp" {
		t.Fatalf("parsed %s %s %v", id, command, args)
	}
}

func TestFormatCLIConfigMatchesLegacyLayout(t *testing.T) {
	exe := `/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp`
	got := FormatCLIConfig(exe, false)
	if !strings.Contains(got, "[mcp_servers.grok_supervisor]") {
		t.Fatalf("missing toml block:\n%s", got)
	}
	if !strings.HasPrefix(strings.TrimSpace(got), "codex mcp add grok_supervisor --") {
		t.Fatalf("cli:\n%s", got)
	}
}

func TestUpdateJSONUsesLegacyID(t *testing.T) {
	exe := `/tmp/GrokMcp`
	b, err := BuildBundle(exe, "darwin", protocol.MCPServerLegacyID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.JSON, `"grok_supervisor"`) {
		t.Fatalf("new json should use stable id:\n%s", b.JSON)
	}
	if !strings.Contains(b.UpdateJSON, `"Grok Supervisor"`) {
		t.Fatalf("update json should use legacy id:\n%s", b.UpdateJSON)
	}
}
