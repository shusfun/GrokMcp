//go:build windows

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellOpenCCSwitchImportLink(t *testing.T) {
	if os.Getenv("GROKMCP_OPEN_CCSWITCH") != "1" {
		t.Skip("set GROKMCP_OPEN_CCSWITCH=1 to hand the import link to the registered handler")
	}
	exe := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Grok Supervisor", "GrokMcp.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skip(err)
	}
	link, err := DeepLink("grok_supervisor", exe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "resource=mcp") || !strings.Contains(link, "apps=codex") || !strings.Contains(link, "config=") {
		t.Fatalf("link %s", link)
	}
	t.Logf("link_len=%d", len(link))
	if err := shellOpen(link); err != nil {
		t.Fatal(err)
	}
}
