package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grokmcp/internal/protocol"

	_ "modernc.org/sqlite"
)

func TestConfigSucceedsWhenDBUnreadable(t *testing.T) {
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: filepath.Join(t.TempDir(), "nope", "cc-switch.db"),
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
	})
	b, err := s.Config(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b.JSON == "" || b.DeepLink == "" {
		t.Fatal("copyable config should still generate")
	}
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.CCSwitch.Detected != protocol.MCPDetectMissing {
		t.Fatalf("detected %s", st.CCSwitch.Detected)
	}
	if st.Generated.JSON == "" {
		t.Fatal("status should still include generated config")
	}
}

func TestProbeCCSwitchRegisteredLegacyAndNeedsUpdate(t *testing.T) {
	exe := "/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp"
	db := tempCCSwitchDB(t, []mcpRow{{
		ID:      protocol.MCPServerLegacyID,
		Name:    protocol.MCPServerLegacyID,
		Config:  `{"type":"stdio","command":"/old/GrokMcp","args":["mcp"],"startup_timeout_sec":30,"tool_timeout_sec":21600}`,
		Enabled: true,
	}, {
		ID:      "other",
		Name:    "other",
		Config:  `{"type":"stdio","command":"/bin/true","args":["mcp"]}`,
		Enabled: true,
	}})
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: db,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
	})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.CCSwitch.Registered || !st.CCSwitch.NeedsUpdate || !st.CCSwitch.EnabledCodex {
		t.Fatalf("%#v", st.CCSwitch)
	}
	if st.CCSwitch.LegacyID != protocol.MCPServerLegacyID || st.CCSwitch.MatchedID != protocol.MCPServerLegacyID {
		t.Fatalf("alias %#v", st.CCSwitch)
	}
	if !containsJSONKey(st.Generated.UpdateJSON, protocol.MCPServerLegacyID) {
		t.Fatalf("update json should use matched id:\n%s", st.Generated.UpdateJSON)
	}
}

func TestProbeCCSwitchMatchNoUpdate(t *testing.T) {
	exe := "/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp"
	db := tempCCSwitchDB(t, []mcpRow{{
		ID:      protocol.MCPServerID,
		Name:    protocol.MCPServerID,
		Config:  `{"type":"stdio","command":"` + exe + `","args":["mcp"],"startup_timeout_sec":30,"tool_timeout_sec":21600}`,
		Enabled: true,
	}})
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: db,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
	})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.CCSwitch.Registered || st.CCSwitch.NeedsUpdate || !st.CCSwitch.EnabledCodex {
		t.Fatalf("%#v", st.CCSwitch)
	}
}

func TestProbeCCSwitchUnsupportedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE mcp_servers (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: path,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
	})
	st, err := s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.CCSwitch.Detected != protocol.MCPDetectUnsupportedSchema {
		t.Fatalf("detected %s", st.CCSwitch.Detected)
	}
	if st.Generated.JSON == "" {
		t.Fatal("config still required")
	}
}

func TestOpenCCSwitchImportPendingAndDoesNotWriteDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cc-switch.db")
	opened := ""
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: dbPath,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
		OpenURL: func(_ context.Context, u string) error {
			opened = u
			return nil
		},
	})
	res, err := s.OpenCCSwitchMCPImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Action != protocol.MCPActionPendingUser || res.LiveEffective {
		t.Fatalf("%#v", res)
	}
	if opened == "" || !containsJSONKeyFromLink(t, opened, protocol.MCPServerID) {
		t.Fatalf("opened %s", opened)
	}
	if fileExists(dbPath) {
		t.Fatal("must not create or write CC-Switch db")
	}
}

func TestOpenCCSwitchImportDoesNotUseDeepLinkWhenNeedsUpdate(t *testing.T) {
	exe := "/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp"
	db := tempCCSwitchDB(t, []mcpRow{{
		ID:      protocol.MCPServerLegacyID,
		Name:    protocol.MCPServerLegacyID,
		Config:  `{"type":"stdio","command":"/old/GrokMcp","args":["mcp"],"startup_timeout_sec":30,"tool_timeout_sec":21600}`,
		Enabled: true,
	}})
	called := false
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: db,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
		OpenURL: func(context.Context, string) error {
			called = true
			return nil
		},
	})
	res, err := s.OpenCCSwitchMCPImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionNeedsManual || called {
		t.Fatalf("action=%s called=%v", res.Action, called)
	}
}

func TestOpenCCSwitchImportUnchangedWhenMatched(t *testing.T) {
	exe := "/tmp/GrokMcp"
	db := tempCCSwitchDB(t, []mcpRow{{
		ID:      protocol.MCPServerID,
		Name:    protocol.MCPServerID,
		Config:  `{"type":"stdio","command":"` + exe + `","args":["mcp"],"startup_timeout_sec":30,"tool_timeout_sec":21600}`,
		Enabled: true,
	}})
	called := false
	s := New(Options{
		Executable: func() (string, error) { return exe, nil },
		CCSwitchDB: db,
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
		OpenURL: func(context.Context, string) error {
			called = true
			return nil
		},
	})
	res, err := s.OpenCCSwitchMCPImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionUnchanged || called {
		t.Fatalf("%#v called=%v", res, called)
	}
}

func TestOpenURLFailure(t *testing.T) {
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: filepath.Join(t.TempDir(), "missing.db"),
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
		OpenURL:    func(context.Context, string) error { return errString("boom") },
	})
	res, err := s.OpenCCSwitchMCPImport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || res.Action != protocol.MCPActionFailed {
		t.Fatalf("%#v", res)
	}
}

func TestOpenCCSwitchAppUsesAppOpenerNotImportURL(t *testing.T) {
	openedURL := ""
	openedApp := false
	s := New(Options{
		Executable: func() (string, error) { return "/tmp/GrokMcp", nil },
		CCSwitchDB: filepath.Join(t.TempDir(), "missing.db"),
		GOOS:       "darwin",
		LookPath:   func(string) (string, error) { return "", errString("missing") },
		OpenURL: func(_ context.Context, u string) error {
			openedURL = u
			return nil
		},
		OpenApp: func(context.Context) error {
			openedApp = true
			return nil
		},
	})
	res, err := s.OpenCCSwitchApp(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Action != protocol.MCPActionPendingUser || res.LiveEffective || !openedApp || openedURL != "" {
		t.Fatalf("res=%#v openedApp=%v openedURL=%q", res, openedApp, openedURL)
	}
}

type mcpRow struct {
	ID, Name, Config string
	Enabled          bool
}

func tempCCSwitchDB(t *testing.T, rows []mcpRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cc-switch.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE mcp_servers (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, server_config TEXT NOT NULL,
		enabled_codex BOOLEAN NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		en := 0
		if r.Enabled {
			en = 1
		}
		if _, err := db.Exec(`INSERT INTO mcp_servers (id, name, server_config, enabled_codex) VALUES (?, ?, ?, ?)`, r.ID, r.Name, r.Config, en); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func containsJSONKey(raw, key string) bool {
	return strings.Contains(raw, `"`+key+`"`)
}

func containsJSONKeyFromLink(t *testing.T, link, key string) bool {
	t.Helper()
	id, _, _, err := ParseDeepLinkConfig(link)
	if err != nil {
		t.Fatal(err)
	}
	return id == key
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
