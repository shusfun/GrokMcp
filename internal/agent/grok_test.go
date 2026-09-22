package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"grokmcp/internal/protocol"
)

func testGrok() *Grok {
	return &Grok{
		last:    map[string]string{},
		plan:    map[string]bool{},
		pending: map[string]chan planChoice{},
	}
}

func TestPlanModeIDFallbackWhenUnadvertised(t *testing.T) {
	t.Parallel()
	id, err := planModeID(nil)
	if err != nil || id != "plan" {
		t.Fatalf("nil modes: id=%q err=%v", id, err)
	}
	id, err = planModeID(&acp.SessionModeState{})
	if err != nil || id != "plan" {
		t.Fatalf("empty modes: id=%q err=%v", id, err)
	}
	id, err = planModeID(&acp.SessionModeState{
		AvailableModes: []acp.SessionMode{{Id: "custom-plan", Name: "Plan"}},
	})
	if err != nil || id != "custom-plan" {
		t.Fatalf("advertised: id=%q err=%v", id, err)
	}
	_, err = planModeID(&acp.SessionModeState{
		AvailableModes: []acp.SessionMode{{Id: "code", Name: "Code"}},
	})
	if err == nil {
		t.Fatal("expected no plan session mode")
	}
}

func TestSetPlanModeNilModesCallsPlan(t *testing.T) {
	t.Parallel()
	var got acp.SetSessionModeRequest
	calls := 0
	err := setPlanMode(context.Background(), func(_ context.Context, req acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
		calls++
		got = req
		return acp.SetSessionModeResponse{}, nil
	}, "sess-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if got.SessionId != "sess-1" || got.ModeId != "plan" {
		t.Fatalf("%+v", got)
	}
}

func TestSetPlanModePropagatesSetterError(t *testing.T) {
	t.Parallel()
	err := setPlanMode(context.Background(), func(context.Context, acp.SetSessionModeRequest) (acp.SetSessionModeResponse, error) {
		return acp.SetSessionModeResponse{}, errors.New("denied")
	}, "sess-1", nil)
	if err == nil || err.Error() != "session/set_mode plan: denied" {
		t.Fatalf("err=%v", err)
	}
}

func TestSessionUpdateDoesNotPublishPlanReady(t *testing.T) {
	t.Parallel()
	c := newACPClient()
	var action string
	var planReady bool
	c.onUpdate = func(_ string, last string, plan bool) {
		action = last
		planReady = plan
	}
	err := c.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update:    acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{Title: "exit_plan_mode"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if action != "exit_plan_mode" {
		t.Fatalf("action %q", action)
	}
	if planReady {
		t.Fatal("tool update must not publish plan_ready")
	}
}

func TestLooksLikeExitPlanRealGrokTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"Plan: Exit", true},
		{"plan: exit", true},
		{"exit_plan_mode", true},
		{"exit plan", true},
		{"ExitPlanMode", true},
		{"Edit file", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := looksLikeExitPlan(tc.in); got != tc.want {
			t.Fatalf("%q: got %v want %v", tc.in, got, tc.want)
		}
	}
	title := "Plan: Exit"
	if !isExitPlanCall(acp.ToolCallUpdate{Title: &title}) {
		t.Fatal("Plan: Exit tool call must match")
	}
}

func TestHandlePermissionRegistersPendingBeforeHook(t *testing.T) {
	g := testGrok()
	title := "Plan: Exit"
	hookPending := make(chan bool, 1)
	setTestPlanListener(g, func(sid string, _ string) {
		g.mu.Lock()
		_, ok := g.pending[sid]
		g.mu.Unlock()
		hookPending <- ok
	})
	done := make(chan acp.RequestPermissionResponse, 1)
	go func() {
		resp, err := g.handlePermission(context.Background(), acp.RequestPermissionRequest{
			SessionId: "s1",
			ToolCall:  acp.ToolCallUpdate{Title: &title},
			Options: []acp.PermissionOption{
				{Kind: acp.PermissionOptionKindAllowOnce, OptionId: "allow_once", Name: "Allow"},
			},
		})
		if err != nil {
			done <- acp.RequestPermissionResponse{}
			return
		}
		done <- resp
	}()
	select {
	case ok := <-hookPending:
		if !ok {
			t.Fatal("hook fired before pending channel")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook timeout")
	}
	if err := resolveTestPlan(g, context.Background(), "s1", protocol.PlanApprove, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-done:
		if resp.Outcome.Selected == nil || string(resp.Outcome.Selected.OptionId) != "allow_once" {
			t.Fatalf("%+v", resp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("permission timeout")
	}
}

func TestResolvePlanWithoutPendingDoesNotApproveFutureRequest(t *testing.T) {
	g := testGrok()
	if err := resolveTestPlan(g, context.Background(), "s1", protocol.PlanApprove, ""); !errors.Is(err, ErrNoPlanPermission) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	called := false
	setTestPlanListener(g, func(string, string) { called = true })
	_, err := g.awaitPlanChoice(ctx, "s1", "")
	if !called || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("future approval bypassed: called=%v err=%v", called, err)
	}
}

func TestExitPlanModeExtension(t *testing.T) {
	g := testGrok()
	c := newACPClient()
	c.onExitPlan = g.handleExitPlanMode
	if err := c.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update:    acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "call-1", Title: "exit_plan_mode"}},
	}); err != nil {
		t.Fatal(err)
	}
	title := "Plan: Exit"
	if err := c.SessionUpdate(context.Background(), acp.SessionNotification{
		SessionId: "s1",
		Update: acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{
			ToolCallId: "call-1",
			Title:      &title,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	excerpt := make(chan string, 1)
	setTestPlanListener(g, func(sid string, text string) {
		g.mu.Lock()
		_, ok := g.pending[sid]
		g.mu.Unlock()
		if !ok {
			t.Error("hook fired before pending channel")
		}
		excerpt <- text
	})
	params, err := json.Marshal(exitPlanExtReq{
		SessionID: "s1", ToolCallID: "call-1", PlanContent: "two-step plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan any, 1)
	go func() {
		resp, err := c.HandleExtensionMethod(context.Background(), exitPlanMethod, params)
		if err != nil {
			done <- err
			return
		}
		done <- resp
	}()
	select {
	case text := <-excerpt:
		if text != "two-step plan" {
			t.Fatalf("excerpt %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("extension did not publish plan_ready")
	}
	select {
	case <-done:
		t.Fatal("extension returned before ResolvePlan")
	default:
	}
	if err := resolveTestPlan(g, context.Background(), "s1", protocol.PlanApprove, "ship it"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%T %#v", got, got)
		}
		if m["outcome"] != "approved" || m["comment"] != "ship it" {
			t.Fatalf("%v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("extension timeout")
	}
}

func TestUnknownExtensionRequestMethodNotFound(t *testing.T) {
	c := newACPClient()
	_, err := c.HandleExtensionMethod(context.Background(), "_x.ai/session_notification", json.RawMessage(`{}`))
	var re *acp.RequestError
	if !errors.As(err, &re) || re.Code != -32601 {
		t.Fatalf("err=%v", err)
	}
}
