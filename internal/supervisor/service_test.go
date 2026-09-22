package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/ids"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
	"grokmcp/internal/terminal"
	"grokmcp/internal/trace"
)

func newTest(t *testing.T, fake *agent.Fake, term terminal.Launcher) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock.Fake{T: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	s := New(st, fake, term, clk, &ids.Seq{})
	tr, err := trace.Open(t.TempDir(), func() time.Time { return clk.Now() })
	if err != nil {
		t.Fatal(err)
	}
	s.SetTrace(tr)
	t.Cleanup(func() {
		_ = s.Close()
		_ = st.Close()
	})
	return s
}

func waitView(t *testing.T, s *Service, id string, want protocol.ViewMode) protocol.Job {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var j protocol.Job
	for time.Now().Before(deadline) {
		var err error
		j, err = s.Status(context.Background(), id)
		if err == nil && j.ViewMode == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want view %s got %s owner=%s state=%s", want, j.ViewMode, j.InputOwner, j.State)
	return j
}

func waitState(t *testing.T, s *Service, id string, want protocol.JobState) protocol.Job {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var j protocol.Job
	for time.Now().Before(deadline) {
		var err error
		j, err = s.Status(context.Background(), id)
		if err == nil && j.State == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %s got %s action=%s summary=%s", want, j.State, j.LastAction, j.LastSummary)
	return j
}

func TestDispatchHeadlessNoTerminal(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, LastAction: "Plan ready", Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd:   "/tmp/suiyuan",
		Tasks: []protocol.DispatchTask{{Prompt: "build runtime"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Jobs) != 1 {
		t.Fatal(res.Jobs)
	}
	j := res.Jobs[0]
	if j.ViewMode != protocol.ViewHeadless {
		t.Fatalf("view %s", j.ViewMode)
	}
	if j.GrokSessionID != "sess-1" {
		t.Fatalf("session %s", j.GrokSessionID)
	}
	if fake.NewCalls[0].Worktree {
		t.Fatal("worktree should be off")
	}
	waitState(t, s, j.JobID, protocol.StatePlanReady)
	if term.ResumeCount() != 0 {
		t.Fatalf("opened terminal: %v", term.ResumeSnapshot())
	}
}

func TestPlanRequiresApprove(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "approved") || strings.Contains(text, "Implement") {
			t.Fatal("implemented without approve")
		}
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	waitState(t, s, res.Jobs[0].JobID, protocol.StatePlanReady)
	time.Sleep(50 * time.Millisecond)
}

func TestApproveThenComplete(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "Start in plan mode") || strings.Contains(text, "User task") {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
		return agent.PromptResult{
			Text:       protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "shipped"}),
			LastAction: "Completed",
		}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	j := waitState(t, s, id, protocol.StateCompleted)
	if j.LastSummary != "shipped" {
		t.Fatal(j.LastSummary)
	}
	if fake.PromptCount() != 1 || len(fake.Resolves) != 1 {
		t.Fatalf("approval must resume the original prompt: prompts=%v decisions=%v", fake.PromptSnapshot(), fake.Resolves)
	}
}

func TestWorkingAutoContinues(t *testing.T) {
	fake := agent.NewFake()
	n := 0
	fake.PromptFn = func(string, string) agent.PromptResult {
		n++
		if n == 1 {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
		if n == 2 {
			return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerWorking, Summary: "coding"})}
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	_, _ = decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove})
	waitState(t, s, id, protocol.StateCompleted)
	if n < 3 {
		t.Fatalf("turns %d", n)
	}
}

func TestMissingMarkerThenNeedsInput(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{Text: "no marker", LastAction: "Talking"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	waitState(t, s, res.Jobs[0].JobID, protocol.StateNeedsInput)
}

func TestCancelKeepsSession(t *testing.T) {
	fake := agent.NewFake()
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	time.Sleep(30 * time.Millisecond)
	j, err := s.CancelTurn(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != protocol.StateNeedsInput || j.GrokSessionID != "sess-1" {
		t.Fatalf("%+v", j)
	}
	if len(fake.Cancels) == 0 {
		t.Fatal("agent cancel not called")
	}
	time.Sleep(40 * time.Millisecond)
	j, _ = s.Status(context.Background(), id)
	if j.State != protocol.StateNeedsInput {
		t.Fatalf("auto continued: %s", j.State)
	}
}

func TestFollowupQueuesWhileBusy(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "Start in plan mode") || strings.Contains(text, "User task") {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "ok"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StateCompleted)
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	if _, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "please also lint"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	close(block)
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		for _, p := range fake.PromptSnapshot() {
			if strings.Contains(p, "please also lint") {
				found = true
			}
		}
		if found {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatalf("followup dropped: %v", fake.PromptSnapshot())
	}
}

func TestDetachDoesNotCancel(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewHeaded || j.GrokSessionID != "sess-1" {
		t.Fatalf("%+v", j)
	}
	resumes := term.ResumeSnapshot()
	if len(resumes) != 1 || !strings.HasPrefix(resumes[0], "sess-1|") {
		t.Fatalf("resume %v", resumes)
	}
	term.CloseResume("sess-1")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ = s.Status(context.Background(), id)
		if j.ViewMode == protocol.ViewHeadless {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if j.ViewMode != protocol.ViewHeadless || j.State != protocol.StatePlanReady {
		t.Fatalf("%+v", j)
	}
	if len(fake.Cancels) != 0 {
		t.Fatalf("cancel on detach: %v", fake.Cancels)
	}
}

func TestSecondShowTUIFocuses(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if term.ResumeCount() != 1 {
		t.Fatalf("expected one resume, got %v", term.ResumeSnapshot())
	}
}

func TestRecoverLoadsSameSession(t *testing.T) {
	fake := agent.NewFake()
	s := newTest(t, fake, terminal.NewFake())
	seedLifecycleJob(t, s, "recover", protocol.StateExecuting)
	s.Disconnect("recover")
	if len(fake.LoadSnapshot()) != 0 {
		t.Fatal("disconnect must wait for explicit continue")
	}
	if _, err := s.Continue(context.Background(), "recover"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "recover", protocol.StateCompleted)
	loads := fake.LoadSnapshot()
	if len(loads) != 1 || !strings.HasPrefix(loads[0], "original-recover|") {
		t.Fatalf("loads %v", loads)
	}
	j, _ := s.Status(context.Background(), "recover")
	if j.GrokSessionID != "original-recover" {
		t.Fatal(j.GrokSessionID)
	}
}

func TestUserCancelSkipsRecover(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	_, _ = s.CancelTurn(context.Background(), id)
	s.Disconnect(id)
	time.Sleep(50 * time.Millisecond)
	if len(fake.LoadSnapshot()) != 0 {
		t.Fatalf("recovered after cancel: %v", fake.LoadSnapshot())
	}
}

func TestWaitAnyBoundary(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}, {Prompt: "y"}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := s.Wait(ctx, protocol.WaitRequest{JobIDs: []string{res.Jobs[0].JobID, res.Jobs[1].JobID}, Mode: "any"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Jobs) == 0 || !got.Jobs[0].State.IsBoundary() {
		t.Fatalf("%+v", got)
	}
}

func TestRecoverLeavesPlanReady(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan body"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	planPrompts := fake.PromptCount()
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, _ := s.Status(context.Background(), id)
	if j.State != protocol.StatePlanReady {
		t.Fatalf("state %s", j.State)
	}
	if len(fake.LoadSnapshot()) != 0 {
		t.Fatal("startup must not load a plan-ready session")
	}
	if fake.PromptCount() != planPrompts {
		t.Fatalf("recover must not implement: %v", fake.PromptSnapshot())
	}
	if j.PlanSummary == "" {
		t.Fatal("missing plan summary")
	}
}

func TestContinueRejectedWhenPlanReady(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.Continue(context.Background(), id); err == nil {
		t.Fatal("expected plan pending error")
	}
	j, _ := s.Status(context.Background(), id)
	if j.State != protocol.StatePlanReady {
		t.Fatalf("state %s", j.State)
	}
}

func TestCancelTurnThenContinue(t *testing.T) {
	fake := agent.NewFake()
	n := 0
	fake.PromptFn = func(string, string) agent.PromptResult {
		n++
		if n == 1 {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "ok"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.CancelTurn(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	j, err := s.Continue(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if j.State == protocol.StateCancelled {
		t.Fatal("continue should not stay cancelled")
	}
}

func TestSetViewHeadlessClosesTerminal(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if !term.HasResume("sess-1") {
		t.Fatal("expected open terminal")
	}
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless})
	if err != nil {
		t.Fatal(err)
	}
	if j.State != protocol.StatePlanReady {
		t.Fatalf("%+v", j)
	}
	j = waitView(t, s, id, protocol.ViewHeadless)
	if j.State != protocol.StatePlanReady {
		t.Fatalf("%+v", j)
	}
	if term.HasResume("sess-1") {
		t.Fatal("terminal handle still open")
	}
	if len(fake.Cancels) != 0 {
		t.Fatalf("cancel on headless: %v", fake.Cancels)
	}
}

func TestServiceCloseClosesTerminal(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if term.HasResume("sess-1") {
		t.Fatal("terminal handle still open")
	}
}

func TestWorktreeWritesEffectiveCwd(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x", Worktree: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	j := res.Jobs[0]
	if j.Cwd == "/tmp/p" || !strings.Contains(j.Cwd, "-wt-") {
		t.Fatalf("cwd %s", j.Cwd)
	}
	if fake.NewCalls[0].Cwd != "/tmp/p" || !fake.NewCalls[0].Worktree {
		t.Fatalf("%+v", fake.NewCalls[0])
	}
	if fake.Sessions[j.GrokSessionID] != j.Cwd {
		t.Fatalf("session cwd %s job %s", fake.Sessions[j.GrokSessionID], j.Cwd)
	}
}

func TestAttachAtPlanReadyWhileBusy(t *testing.T) {
	fake := agent.NewFake()
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	fake.PlanReadyBeforeBlock = "plan excerpt"
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	j := waitState(t, s, id, protocol.StatePlanReady)
	if !j.Busy {
		t.Fatal("expected busy plan prompt")
	}
	if j.GrokSessionID != "sess-1" {
		t.Fatalf("session %s", j.GrokSessionID)
	}
	j, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewHeaded || j.InputOwner != protocol.OwnerTUI {
		t.Fatalf("view=%s owner=%s", j.ViewMode, j.InputOwner)
	}
	if j.State != protocol.StatePlanReady || j.GrokSessionID != "sess-1" {
		t.Fatalf("%+v", j)
	}
	if !j.Busy {
		t.Fatal("attach must not wait for prompt to finish")
	}
	if term.ResumeCount() != 1 {
		t.Fatalf("resume %v", term.ResumeSnapshot())
	}
	if len(fake.Cancels) != 0 {
		t.Fatalf("cancel on attach: %v", fake.Cancels)
	}
	if fake.PromptCount() != 1 {
		t.Fatalf("re-prompted: %d", fake.PromptCount())
	}
	snap, err := s.DebugSnapshot(context.Background(), protocol.DebugSnapshotRequest{JobID: id, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, ev := range snap.Events {
		got[ev.Event] = true
	}
	for _, name := range []string{"view.attach.requested", "terminal.opened", "view.attached"} {
		if !got[name] {
			t.Fatalf("missing %s", name)
		}
	}
	select {
	case <-block:
		t.Fatal("plan prompt returned during attach")
	default:
	}
}

func TestAttachWhenIdleProceedsAtPlanReady(t *testing.T) {
	fake := agent.NewFake()
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	deadline := time.Now().Add(2 * time.Second)
	var j protocol.Job
	for time.Now().Before(deadline) {
		j, _ = s.Status(context.Background(), id)
		if j.Busy && j.State == protocol.StatePlanning && j.GrokSessionID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !j.Busy || j.State != protocol.StatePlanning {
		t.Fatalf("expected busy planning, got state=%s busy=%v", j.State, j.Busy)
	}
	j, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewAttaching {
		t.Fatalf("view %s", j.ViewMode)
	}
	if term.ResumeCount() != 0 {
		t.Fatalf("resumed before plan_ready: %v", term.ResumeSnapshot())
	}
	s.becomePlanReady(j, "plan excerpt")
	j = waitView(t, s, id, protocol.ViewHeaded)
	if j.InputOwner != protocol.OwnerTUI || j.State != protocol.StatePlanReady {
		t.Fatalf("%+v", j)
	}
	if term.ResumeCount() == 0 {
		t.Fatal("expected resume after plan_ready")
	}
	if len(fake.Cancels) != 0 {
		t.Fatalf("cancel on attach: %v", fake.Cancels)
	}
}

func TestLiveAttachWaitsWhileBusy(t *testing.T) {
	fake := agent.NewFake()
	fake.Mode = "live"
	block := make(chan struct{})
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "Start in plan mode") || strings.Contains(text, "User task") {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
		<-block
		return agent.PromptResult{
			Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"}),
		}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var j protocol.Job
	for time.Now().Before(deadline) {
		j, _ = s.Status(context.Background(), id)
		if j.Busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !j.Busy {
		t.Fatal("expected busy implement turn")
	}
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewAttaching {
		t.Fatalf("view %s", j.ViewMode)
	}
	if term.ResumeCount() != 0 {
		t.Fatalf("resumed while busy: %v", term.ResumeSnapshot())
	}
	close(block)
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if term.ResumeCount() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected resume after idle, got %v", term.ResumeSnapshot())
}

func TestReviseIgnoresOldNeedsInputMarker(t *testing.T) {
	fake := agent.NewFake()
	hold := make(chan struct{})
	fake.PromptBlock["sess-1"] = hold
	fake.PlanReadyBeforeBlock = "old two-step"
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "Revise the plan") {
			return agent.PromptResult{PlanReady: true, Text: "new three-step"}
		}
		if strings.Contains(text, "The plan is approved") {
			return agent.PromptResult{
				Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "shipped"}),
			}
		}
		return agent.PromptResult{
			PlanReady: false,
			Text: protocol.RenderTaskState(protocol.TaskState{
				State:   protocol.MarkerNeedsInput,
				Summary: "两步只读验证计划已提交，等待修订意见。",
			}),
		}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	old, _ := s.Status(context.Background(), id)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "make it three steps"}); err != nil {
		t.Fatal(err)
	}
	close(hold)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := s.Status(context.Background(), id)
		if j.State == protocol.StateNeedsInput {
			t.Fatalf("old turn overwrote planning: %+v", j)
		}
		if j.State == protocol.StatePlanReady && j.PlanSummary != old.PlanSummary {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	j := waitState(t, s, id, protocol.StatePlanReady)
	if !strings.Contains(j.PlanSummary, "new three-step") {
		t.Fatalf("summary %q", j.PlanSummary)
	}
	if j.PlanDigest == "" || j.PlanDigest == old.PlanDigest {
		t.Fatalf("digest old=%q new=%q", old.PlanDigest, j.PlanDigest)
	}
}

func TestReviseThenApproveCompletes(t *testing.T) {
	fake := agent.NewFake()
	n := 0
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "The plan is approved") {
			return agent.PromptResult{
				Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"}),
			}
		}
		n++
		return agent.PromptResult{PlanReady: true, Text: "plan-" + strings.Repeat("x", n)}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "more"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StateCompleted)
}

func TestReviseTwiceThenApproveCompletes(t *testing.T) {
	fake := agent.NewFake()
	n := 0
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "The plan is approved") {
			return agent.PromptResult{
				Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"}),
			}
		}
		n++
		return agent.PromptResult{PlanReady: true, Text: "plan-v" + strings.Repeat("y", n)}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	_, _ = decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "v2"})
	waitState(t, s, id, protocol.StatePlanReady)
	_, _ = decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "v3"})
	j := waitState(t, s, id, protocol.StatePlanReady)
	if !strings.Contains(j.PlanSummary, "plan-vyyy") && !strings.Contains(j.PlanSummary, "plan-v") {
		t.Fatalf("summary %q n=%d", j.PlanSummary, n)
	}
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StateCompleted)
}

func TestReviseThenCancel(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "tweak"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanCancel}); err != nil {
		t.Fatal(err)
	}
	j, _ := s.Status(context.Background(), id)
	if j.State != protocol.StateCancelled {
		t.Fatalf("state %s", j.State)
	}
	if _, err := s.CancelTurn(context.Background(), id); err == nil {
		t.Fatal("expected cancel turn to fail after plan cancel")
	}
	if _, err := s.Continue(context.Background(), id); err == nil {
		t.Fatal("expected continue to fail after plan cancel")
	}
	j, _ = s.Status(context.Background(), id)
	if j.State != protocol.StateCancelled {
		t.Fatalf("state after turn controls %s", j.State)
	}
	ev, err := s.Events(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ev {
		if e.EventType == string(protocol.StateNeedsInput) {
			t.Fatalf("unexpected needs_input event after plan cancel: %+v", ev)
		}
	}
}

func TestReviseDoesNotRepublishOldPlan(t *testing.T) {
	fake := agent.NewFake()
	block := make(chan struct{})
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "Revise the plan") {
			<-block
			return agent.PromptResult{PlanReady: true, Text: "new plan"}
		}
		return agent.PromptResult{PlanReady: true, Text: "old plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "shorter"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := s.Status(context.Background(), id)
		if j.State == protocol.StatePlanReady {
			t.Fatalf("old plan republished: %+v", j)
		}
		if j.State == protocol.StatePlanning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := s.Status(context.Background(), id)
	if j.State != protocol.StatePlanning {
		t.Fatalf("state %s", j.State)
	}
	close(block)
	j = waitState(t, s, id, protocol.StatePlanReady)
	if j.PlanSummary != "new plan" && !strings.Contains(j.PlanSummary, "new plan") {
		t.Fatalf("summary %q", j.PlanSummary)
	}
}

func TestDispatchRejectsAutomaticHeadedDefault(t *testing.T) {
	fake := agent.NewFake()
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	if err := s.SaveSettings(context.Background(), protocol.Settings{DefaultViewMode: "headed"}); err == nil {
		t.Fatal("headed default must be rejected")
	}
	fake.PromptFn = func(string, string) agent.PromptResult { return agent.PromptResult{PlanReady: true, Text: "plan"} }
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	job := waitState(t, s, res.Jobs[0].JobID, protocol.StatePlanReady)
	if term.ResumeCount() != 0 || job.DesiredViewMode != protocol.ViewHeadless {
		t.Fatal("automatic terminal opened")
	}
}

func TestCloseAfterHeadedWindowsCwdUnblocksWait(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: `C:\Work\GrokMcp`, Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close hung after headed TUI on windows cwd")
	}
}

func TestPumpTraceContainsSkipReason(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	seedLifecycleJob(t, s, "trace", protocol.StateExecuting)
	if _, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: "trace", Prompt: "queued while TUI owns input"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := s.DebugSnapshot(context.Background(), protocol.DebugSnapshotRequest{JobID: "trace", Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range snap.Events {
			if ev.Event == "pump.skipped" {
				reason, _ := ev.Fields["reason"].(string)
				if reason == "tui_owns" || reason == "stale_tui_owner" {
					return
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("missing pump.skipped reason")
}

func TestSetViewHeadlessReturnsWithoutWaitingForClose(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	block := make(chan struct{})
	fake.LoadBlock["sess-1"] = block
	term := terminal.NewFake()
	term.CloseDelay = 200 * time.Millisecond
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	start := time.Now()
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > time.Second {
		t.Fatalf("set_view blocked %s", elapsed)
	}
	if j.GrokSessionID != "sess-1" || j.State != protocol.StatePlanReady {
		t.Fatalf("%+v", j)
	}
	close(block)
}

func TestDetachDoesNotCancelTurn(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	j := waitView(t, s, id, protocol.ViewHeadless)
	if j.State != protocol.StatePlanReady || j.GrokSessionID != "sess-1" {
		t.Fatalf("%+v", j)
	}
	if len(fake.Cancels) != 0 {
		t.Fatalf("cancel on detach: %v", fake.Cancels)
	}
}

func TestTUIProcessExitReclaimsOwnership(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	term.ExitProcess("sess-1")
	j := waitView(t, s, id, protocol.ViewHeadless)
	if j.InputOwner != protocol.OwnerSupervisor {
		t.Fatalf("owner %s", j.InputOwner)
	}
}

func TestTerminalWindowCanRemainAfterTUIExit(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	term.ExitProcess("sess-1")
	waitView(t, s, id, protocol.ViewHeadless)
	if !term.HasWindow("sess-1") {
		t.Fatal("window should remain after grok exit")
	}
}

func TestDetachLoadSessionFailureIsObservable(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	fake.LoadErr = errLoadBoom
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snap, _ := s.DebugSnapshot(context.Background(), protocol.DebugSnapshotRequest{JobID: id, Limit: 200})
		for _, ev := range snap.Events {
			if ev.Event == "session.load.failed" {
				j, _ := s.Status(context.Background(), id)
				if j.State == protocol.StateCancelled {
					t.Fatal("load failure cancelled job")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session.load.failed not observed")
}

func TestRepeatedDetachIsIdempotent(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	if len(fake.LoadSnapshot()) != 0 {
		t.Fatalf("headless detach loaded session: %v", fake.LoadSnapshot())
	}
}

func TestAttachDetachGenerationRejectsStaleWatcher(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeaded)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	term.ExitProcess("sess-1")
	waitView(t, s, id, protocol.ViewHeadless)
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	j := waitView(t, s, id, protocol.ViewHeaded)
	if j.InputOwner != protocol.OwnerTUI {
		t.Fatalf("%+v", j)
	}
}

func TestWatchdogDetectsStaleTUIOwner(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	clk := s.clock.(*clock.Fake)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	job, _ := s.load(id)
	job.ViewMode = protocol.ViewHeaded
	job.InputOwner = protocol.OwnerTUI
	s.save(job)
	s.enqueue(id, queued{kind: "continue", text: "x"})
	time.Sleep(20 * time.Millisecond)
	s.inspectWatchdog(clk.Now())
	clk.Advance(4 * time.Second)
	s.inspectWatchdog(clk.Now())
	j, _ := s.Status(context.Background(), id)
	if !j.Stalled || j.StalledReason != "stale_tui_owner" {
		t.Fatalf("stalled %+v", j)
	}
}

func TestDebugSnapshotUsesCursorAndLimit(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	snap, err := s.DebugSnapshot(context.Background(), protocol.DebugSnapshotRequest{JobID: id, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Events) != 2 {
		t.Fatalf("limit %+v", snap)
	}
	next, err := s.DebugSnapshot(context.Background(), protocol.DebugSnapshotRequest{JobID: id, Cursor: snap.Cursor, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Events) == 0 {
		t.Fatal("expected remaining events")
	}
	if next.Events[0].Seq <= snap.Cursor {
		t.Fatalf("cursor overlap %d <= %d", next.Events[0].Seq, snap.Cursor)
	}
}

func TestNormalWaitDoesNotContainDebugLogs(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	got, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{res.Jobs[0].JobID}, TimeoutSec: 3})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "pump.started") || strings.Contains(string(raw), "acp.prompt.started") {
		t.Fatalf("wait leaked traces: %s", raw)
	}
}

func TestDebugSetEmptyJobIDSetsGlobal(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	s := newTest(t, fake, terminal.NewFake())
	if _, err := s.DebugSet(context.Background(), protocol.DebugSetRequest{Enabled: true, Payloads: true}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.DebugEnabled || !st.DebugPayloads {
		t.Fatalf("settings %+v", st)
	}
	bar, err := s.StatusBar(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bar.DebugEnabled {
		t.Fatal("status bar should show global debug")
	}
	s.traces.Emit(trace.Event{JobID: "none", Level: trace.LevelDebug, Source: trace.SourceACP, Name: "acp.session_update"})
	if len(s.traces.Snapshot("none", 0, 10, nil, nil).Events) == 0 {
		t.Fatal("global debug should record debug events")
	}
	if _, err := s.DebugSet(context.Background(), protocol.DebugSetRequest{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	bar, _ = s.StatusBar(context.Background())
	if bar.DebugEnabled {
		t.Fatal("debug should turn off")
	}
}

var errLoadBoom = errors.New("load failed")
