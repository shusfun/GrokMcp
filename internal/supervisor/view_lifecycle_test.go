package supervisor

import (
	"context"
	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"sync/atomic"
	"testing"
	"time"
)

type persistentTerminal struct {
	*terminal.Fake
	alive       atomic.Bool
	activations atomic.Int32
	exit        func(string, string)
}

func (t *persistentTerminal) WorkerAlive(string) bool                       { return t.alive.Load() }
func (t *persistentTerminal) SetWorkerExitListener(fn func(string, string)) { t.exit = fn }
func (t *persistentTerminal) OpenPending(ctx context.Context, bin, sid, cwd string) (terminal.Handle, error) {
	return t.Fake.OpenResume(ctx, bin, sid, cwd)
}
func (t *persistentTerminal) ActivateSession(context.Context, string, string, string) error {
	t.activations.Add(1)
	t.alive.Store(true)
	return nil
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
	if _, err = s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "next"}); err != nil {
		t.Fatal(err)
	}
	if f.PromptCount() != 1 {
		t.Fatal("control was stolen before safe boundary")
	}
	term.alive.Store(false)
	term.exit(job.GrokSessionID, "fixture-worker")
	got = waitState(t, s, id, protocol.StateCompleted)
	if got.GrokSessionID != job.GrokSessionID || f.PromptCount() != 2 || len(f.NewCalls) != 1 {
		t.Fatal("handoff replaced session or lost queued prompt")
	}
}

func TestClosingPendingViewPreventsLateWindowOrWorker(t *testing.T) {
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
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if term.ResumeCount() != 1 {
		t.Fatal("pending view did not open on explicit request")
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	if term.ResumeCount() != 1 {
		t.Fatal("repeated pending request opened another window")
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeadless)
	close(block)
	waitState(t, s, id, protocol.StateCompleted)
	if term.activations.Load() != 0 || term.ResumeCount() != 1 || len(f.Cancels) != 0 {
		t.Fatal("closed pending request later activated a worker/window")
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
