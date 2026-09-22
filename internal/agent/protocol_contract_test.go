package agent

import (
	"context"
	acp "github.com/coder/acp-go-sdk"
	"os"
	"path/filepath"
	"testing"
)

func TestOldPermissionKeepsItsOriginalTurnIdentity(t *testing.T) {
	g := testGrok()
	g.connectionID = "connection"
	g.turns = map[string]string{"s": "new-turn"}
	g.requests = map[string]string{"s": "new-request"}
	calls := 0
	g.SetPlanHandler(func(PlanRequest) error { calls++; return nil })
	c := newACPClient()
	c.origin = func(string) Activity {
		return Activity{ConnectionID: "connection", SessionID: "s", TurnID: "old-turn", RequestID: "old-request"}
	}
	c.rememberToolCall("s", "old-call", "exit_plan_mode", nil, "", acp.ToolCallStatusPending)
	c.origin = func(string) Activity {
		return Activity{ConnectionID: "connection", SessionID: "s", TurnID: "new-turn", RequestID: "new-request"}
	}
	c.onPermission = g.handlePermission
	_, err := c.RequestPermission(context.Background(), acp.RequestPermissionRequest{SessionId: "s", ToolCall: acp.ToolCallUpdate{ToolCallId: "old-call"}})
	if err == nil || calls != 0 {
		t.Fatalf("stale permission rebound to current turn: %v calls=%d", err, calls)
	}
}
func TestSearchTextDoesNotBecomePlanPermission(t *testing.T) {
	title := "Search exit_plan_mode implementation"
	if isExitPlanCall(acp.ToolCallUpdate{Title: &title, RawInput: map[string]any{"query": "exit_plan_mode"}}) {
		t.Fatal("ordinary search interpreted as approval")
	}
}
func TestAdvertisedFileReadHonorsLineRange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0600); err != nil {
		t.Fatal(err)
	}
	start, limit := 2, 1
	c := newACPClient()
	got, err := c.ReadTextFile(context.Background(), acp.ReadTextFileRequest{Path: p, Line: &start, Limit: &limit})
	if err != nil || got.Content != "two\n" {
		t.Fatalf("%q %v", got.Content, err)
	}
	start = 0
	if _, err := c.ReadTextFile(context.Background(), acp.ReadTextFileRequest{Path: p, Line: &start}); err == nil {
		t.Fatal("invalid line range accepted")
	}
	if r, err := c.CreateTerminal(context.Background(), acp.CreateTerminalRequest{}); err == nil || r.TerminalId != "" {
		t.Fatal("unsupported terminal fabricated success")
	}
}
