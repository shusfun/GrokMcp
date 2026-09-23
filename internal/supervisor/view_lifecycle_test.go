package supervisor

import (
	"context"
	"errors"
	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"sync/atomic"
	"testing"
	"time"
)

type persistentTerminal struct {
	*terminal.Fake
	alive atomic.Bool
	exit  func(string, string)
}

func (t *persistentTerminal) WorkerAlive(string) bool                       { return t.alive.Load() }
func (t *persistentTerminal) SetWorkerExitListener(fn func(string, string)) { t.exit = fn }
func (t *persistentTerminal) OpenResume(ctx context.Context, bin, sid, cwd string) (terminal.Handle, error) {
	h, err := t.Fake.OpenResume(ctx, bin, sid, cwd)
	if err == nil {
		t.alive.Store(true)
	}
	return h, err
}

type delayedTerminal struct {
	*persistentTerminal
	started chan struct{}
	release chan struct{}
	fail    bool
}

func (d *delayedTerminal) OpenResume(ctx context.Context, bin, sid, cwd string) (terminal.Handle, error) {
	close(d.started)
	<-d.release
	if d.fail {
		return nil, errors.New("viewer launch failed")
	}
	return d.persistentTerminal.OpenResume(ctx, bin, sid, cwd)
}

func TestCloseDuringViewerLaunchKeepsHeadless(t *testing.T) {
	f := agent.NewFake()
	base := &persistentTerminal{Fake: terminal.NewFake()}
	d := &delayedTerminal{persistentTerminal: base, started: make(chan struct{}), release: make(chan struct{})}
	s := newTest(t, f, d)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StateCompleted)
	done := make(chan error, 1)
	go func() {
		_, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
		done <- err
	}()
	<-d.started
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	close(d.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	j := waitView(t, s, id, protocol.ViewHeadless)
	if j.DesiredViewMode != protocol.ViewHeadless || j.InputOwner == protocol.OwnerSupervisor && base.alive.Load() {
		t.Fatalf("stale attach changed ownership: %+v", j)
	}
}

func TestReopenDuringPreviousViewerLaunchUsesLatestRequest(t *testing.T) {
	f := agent.NewFake()
	base := &persistentTerminal{Fake: terminal.NewFake()}
	d := &delayedTerminal{persistentTerminal: base, started: make(chan struct{}), release: make(chan struct{})}
	s := newTest(t, f, d)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StateCompleted)
	first := make(chan error, 1)
	go func() {
		_, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
		first <- err
	}()
	<-d.started
	if _, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	go func() {
		_, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
		second <- err
	}()
	deadline := time.Now().Add(time.Second)
	for s.snapshot(id).DesiredViewMode != protocol.ViewHeaded && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(d.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	j := waitView(t, s, id, protocol.ViewHeaded)
	if j.InputOwner != protocol.OwnerTUI || base.ResumeCount() != 1 {
		t.Fatalf("latest request did not open TUI: %+v, launches=%d", j, base.ResumeCount())
	}
}

func TestClosingViewDoesNotCancelWorkerAndReturnsControlAtWorkerExit(t *testing.T) {
	f := agent.NewFake()
	term := &persistentTerminal{Fake: terminal.NewFake()}
	s := newTest(t, f, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	job := waitState(t, s, id, protocol.StateCompleted)
	term.alive.Store(true)
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	got := waitView(t, s, id, protocol.ViewHeadless)
	if !term.alive.Load() || got.InputOwner != protocol.OwnerTUI || len(f.Cancels) != 0 {
		t.Fatalf("view close cancelled execution: %+v", got)
	}
	if _, err = s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "next"}); err != errTUIControl {
		t.Fatalf("followup during TUI control = %v", err)
	}
	if f.PromptCount() != 1 {
		t.Fatal("control was stolen before safe boundary")
	}
	term.alive.Store(false)
	term.exit(job.GrokSessionID, "fixture-worker")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got = s.snapshot(id)
		if got.ViewMode == protocol.ViewHeadless && got.InputOwner == protocol.OwnerSupervisor {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got.InputOwner != protocol.OwnerSupervisor {
		t.Fatalf("worker exit did not return control: %+v", got)
	}
	if got.GrokSessionID != job.GrokSessionID || f.PromptCount() != 1 || len(f.NewCalls) != 1 {
		t.Fatal("handoff replaced session or executed rejected prompt")
	}
	if _, err = s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "next"}); err != nil {
		t.Fatal(err)
	}
	got = waitState(t, s, id, protocol.StateCompleted)
	if got.GrokSessionID != job.GrokSessionID || f.PromptCount() != 2 || len(f.NewCalls) != 1 {
		t.Fatal("followup after handoff did not use original session")
	}
}

func TestOpeningBusyViewActivatesWorkerImmediately(t *testing.T) {
	f := agent.NewFake()
	block := make(chan struct{})
	f.PromptBlock["sess-1"] = block
	term := &persistentTerminal{Fake: terminal.NewFake()}
	s := newTest(t, f, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	deadline := time.Now().Add(time.Second)
	for !s.snapshot(id).Busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	term.alive.Store(true)
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if term.ResumeCount() != 1 {
		t.Fatal("busy view did not activate worker immediately")
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if term.ResumeCount() != 1 {
		t.Fatal("repeated request opened another window")
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeadless)
	close(block)
	waitState(t, s, id, protocol.StateCompleted)
	if term.ResumeCount() != 1 || len(f.Cancels) != 0 {
		t.Fatal("closing busy viewer stopped worker or turn")
	}
}

func TestOpeningApprovalYieldsTurnWithoutFakeResult(t *testing.T) {
	f := agent.NewFake()
	f.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan body"}
	}
	term := &persistentTerminal{Fake: terminal.NewFake()}
	s := newTest(t, f, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	job := waitState(t, s, id, protocol.StatePlanReady)
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	got := waitView(t, s, id, protocol.ViewHeaded)
	if got.InputOwner != protocol.OwnerTUI || got.WaitReason != "tui_active" || got.PauseReason != "execution_unknown" || got.RequestPhase != "unknown" || got.State == protocol.StateCompleted {
		t.Fatalf("approval handoff %+v", got)
	}
	if got.ApprovalDelivery != "expired" || len(f.Cancels) != 1 || f.PromptCount() != 1 {
		t.Fatalf("approval was not yielded: delivery=%s cancels=%d prompts=%d", got.ApprovalDelivery, len(f.Cancels), f.PromptCount())
	}
	if _, err = s.Status(context.Background(), id, protocol.ResultQuery{IncludeResult: true, RequestID: job.RequestID}); err == nil {
		t.Fatal("yielded approval stored a result")
	}
	if _, err = s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "next"}); err != errTUIControl {
		t.Fatalf("followup during yielded TUI = %v", err)
	}
	if s.snapshot(id).QueueLength != 0 {
		t.Fatal("rejected followup entered the queue")
	}
}

func TestPersistedHeadedDesireCannotOpenWindow(t *testing.T) {
	f := agent.NewFake()
	term := terminal.NewFake()
	s := newTest(t, f, term)
	seedLifecycleJob(t, s, "history", protocol.StatePlanReady)
	s.maybeAttachDesired("history")
	if term.ResumeCount() != 0 {
		t.Fatal("historical desired mode opened a window")
	}
}

func (t *persistentTerminal) WorkerGeneration(string) string { return "fixture-worker" }
