package app

import (
	"strings"
	"testing"
)

func TestDesktopInstanceIDFollowsHome(t *testing.T) {
	t.Setenv("GROK_SUPERVISOR_HOME", t.TempDir())
	a := desktopInstanceID()
	t.Setenv("GROK_SUPERVISOR_HOME", t.TempDir())
	b := desktopInstanceID()
	if a == b || !strings.HasPrefix(a, "grokmcp.supervisor.desktop.") {
		t.Fatalf("ids = %q %q", a, b)
	}
}
