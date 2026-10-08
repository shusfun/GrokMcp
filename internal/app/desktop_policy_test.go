package app

import "testing"

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
