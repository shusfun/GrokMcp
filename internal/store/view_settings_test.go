package store

import (
	"grokmcp/internal/protocol"
	"path/filepath"
	"testing"
)

func TestLegacyHeadedSettingMigratesWithoutChangingOtherSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSettings(protocol.Settings{GrokBinaryPath: "keep-binary", TerminalCommandTemplate: "keep-template", DebugEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE settings SET value='headed' WHERE key='default_view_mode'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultViewMode != "headless" || settings.GrokBinaryPath != "keep-binary" || settings.TerminalCommandTemplate != "keep-template" || !settings.DebugEnabled {
		t.Fatalf("migration changed other settings: %+v", settings)
	}
}
