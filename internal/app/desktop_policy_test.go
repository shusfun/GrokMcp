package app

import (
	"os"
	"strings"
	"testing"
)

func TestActivateOnSecondInstance(t *testing.T) {
	if ActivateOnSecondInstance([]string{`C:\GrokMcp.exe`, "desktop", BackgroundArg}) {
		t.Fatal("MCP background relaunch must not activate the existing window")
	}
	if !ActivateOnSecondInstance([]string{`C:\GrokMcp.exe`}) {
		t.Fatal("explicit launch must activate the existing window")
	}
	if !ActivateOnSecondInstance([]string{`C:\GrokMcp.exe`, "desktop"}) {
		t.Fatal("explicit desktop launch must activate the existing window")
	}
	if !ActivateOnSecondInstance(nil) {
		t.Fatal("empty args are an explicit launch")
	}
}

func TestTrayClickUsesSharedExplicitShow(t *testing.T) {
	b, err := os.ReadFile("desktop.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	i := strings.Index(src, "tray.OnClick")
	if i < 0 {
		t.Fatal("tray click handler missing")
	}
	line := src[i:]
	if nl := strings.Index(line, "\n"); nl >= 0 {
		line = line[:nl]
	}
	if !strings.Contains(line, "host.showMain()") {
		t.Fatalf("tray click must use showMain: %s", line)
	}
	if strings.Contains(line, "window.Show") || strings.Contains(line, "window.Focus") {
		t.Fatalf("tray click bypasses explicit show: %s", line)
	}
}
