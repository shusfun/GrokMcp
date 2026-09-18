package supervisor

import (
	"context"
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
)

func newTest(t *testing.T, fake *agent.Fake, term *terminal.Fake) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	clk := &clock.Fake{T: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	s := New(st, fake, term, clk, &ids.Seq{})
	t.Cleanup(func() {
		_ = s.Close()
		_ = st.Close()
	})
	return s
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
		if strings.Contains(text, "plan mode") || strings.Contains(text, "User task") {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	j := waitState(t, s, id, protocol.StateCompleted)
	if j.LastSummary != "shipped" {
		t.Fatal(j.LastSummary)
	}
	found := false
	for _, p := range fake.PromptSnapshot() {
		if strings.Contains(p, "The plan is approved") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing ApprovePrompt: %v", fake.PromptSnapshot())
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
	_, _ = s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove})
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
		if strings.Contains(text, "plan mode") || strings.Contains(text, "User task") {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
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
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{
			Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerWorking, Summary: "go"}),
		}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StateExecuting)
	s.Disconnect(id)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fake.LoadSnapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	loads := fake.LoadSnapshot()
	if len(loads) == 0 || !strings.HasPrefix(loads[0], "sess-1|") {
		t.Fatalf("loads %v", loads)
	}
	j, _ := s.Status(context.Background(), id)
	if j.GrokSessionID != "sess-1" {
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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fake.LoadSnapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := s.Status(context.Background(), id)
	if j.State != protocol.StatePlanReady {
		t.Fatalf("state %s", j.State)
	}
	if len(fake.LoadSnapshot()) == 0 {
		t.Fatal("expected session/load for plan_ready recover")
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
	if j.ViewMode != protocol.ViewHeadless || j.State != protocol.StatePlanReady {
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

func TestLiveAttachWaitsWhileBusy(t *testing.T) {
	fake := agent.NewFake()
	fake.Mode = "live"
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "plan mode") || strings.Contains(text, "User task") {
			return agent.PromptResult{PlanReady: true, Text: "plan"}
		}
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
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "make it three steps"}); err != nil {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "more"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
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
	_, _ = s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "v2"})
	waitState(t, s, id, protocol.StatePlanReady)
	_, _ = s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "v3"})
	j := waitState(t, s, id, protocol.StatePlanReady)
	if !strings.Contains(j.PlanSummary, "plan-vyyy") && !strings.Contains(j.PlanSummary, "plan-v") {
		t.Fatalf("summary %q n=%d", j.PlanSummary, n)
	}
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanApprove}); err != nil {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "tweak"}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, id, protocol.StatePlanReady)
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanCancel}); err != nil {
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
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: id, Decide: protocol.PlanRevise, Notes: "shorter"}); err != nil {
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
