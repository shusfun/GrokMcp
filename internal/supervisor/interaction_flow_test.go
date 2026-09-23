package supervisor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func TestPlanningSkipRequiredAndReplan(t *testing.T) {
	fake := agent.NewFake()
	var texts []string
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		texts = append(texts, text)
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	if _, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Planning: "later", Prompt: "no"}}}); err == nil {
		t.Fatal("invalid planning accepted")
	}
	if len(fake.NewCalls) != 0 {
		t.Fatal("invalid planning created a session")
	}
	skipped, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "quick"}}})
	if err != nil {
		t.Fatal(err)
	}
	skipJob := waitState(t, s, skipped.Jobs[0].JobID, protocol.StateCompleted)
	if fake.PlanCalls != 0 || strings.Contains(texts[0], "Start in plan mode") || !strings.Contains(texts[0], "Do not enter plan mode") {
		t.Fatalf("skip entered plan: calls=%d text=%q", fake.PlanCalls, texts[0])
	}
	required, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Planning: protocol.PlanningRequired, Prompt: "design"}}})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, s, required.Jobs[0].JobID, protocol.StateCompleted)
	if fake.PlanCalls == 0 || !strings.Contains(texts[1], "Start in plan mode") {
		t.Fatalf("required did not enter plan: calls=%d text=%q", fake.PlanCalls, texts[1])
	}
	follow, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: skipJob.JobID, Prompt: "again", Planning: protocol.PlanningSkip, Replan: true})
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, s, follow.JobID, protocol.StateCompleted)
	if !strings.Contains(texts[len(texts)-1], "Start in plan mode") || follow.GrokSessionID != skipJob.GrokSessionID {
		t.Fatalf("replan did not force required on the original session: %s", texts[len(texts)-1])
	}
}

func TestBusyFollowupReturnsAndPreservesOrder(t *testing.T) {
	fake := agent.NewFake()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var order []string
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		order = append(order, text)
		if strings.Contains(text, "first") {
			once.Do(func() { close(started) })
			<-release
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "first"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	<-started
	start := time.Now()
	second, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "second"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "third"})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("busy followup waited for the active turn")
	}
	if second.AcceptedRequestID == "" || third.AcceptedRequestID == "" || second.AcceptedRequestID == third.AcceptedRequestID {
		t.Fatalf("missing distinct request ids: %+v %+v", second, third)
	}
	if second.GrokSessionID != res.Jobs[0].GrokSessionID || len(fake.NewCalls) != 1 {
		t.Fatal("busy followup created another session")
	}
	close(release)
	waitState(t, s, id, protocol.StateCompleted)
	joined := strings.Join(order, "\n")
	secondAt := strings.Index(joined, "second")
	thirdAt := strings.Index(joined, "third")
	if secondAt < 0 || thirdAt < 0 || secondAt > thirdAt {
		t.Fatalf("queue order lost: %v", order)
	}
}

func TestTUIActiveQueueSurvivesCloseReopenAndWorkerExit(t *testing.T) {
	fake := agent.NewFake()
	var prompts []string
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		prompts = append(prompts, text)
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	term := &persistentTerminal{Fake: terminal.NewFake()}
	s := newTest(t, fake, term)
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "seed"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	job := waitState(t, s, id, protocol.StateCompleted)
	term.alive.Store(true)
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if first.AcceptedRequestID == "" || second.AcceptedRequestID == "" || s.snapshot(id).QueueLength != 2 || len(fake.Prompts) != 1 {
		t.Fatalf("TUI did not hold the queue: %+v %+v prompts=%d", first, second, len(fake.Prompts))
	}
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeadless}); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, id, protocol.ViewHeadless)
	if _, err = s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded}); err != nil {
		t.Fatal(err)
	}
	reopened := waitView(t, s, id, protocol.ViewHeaded)
	if reopened.GrokSessionID != job.GrokSessionID || reopened.QueueLength != 2 || len(fake.NewCalls) != 1 {
		t.Fatalf("reopen dropped the queue or session: %+v", reopened)
	}
	if len(fake.Prompts) != 1 {
		t.Fatal("queue wrote ACP while the TUI worker was alive")
	}
	term.alive.Store(false)
	term.exit(job.GrokSessionID, "fixture-worker")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(fake.Prompts) < 3 {
		time.Sleep(10 * time.Millisecond)
	}
	joined := strings.Join(prompts, "\n")
	if strings.Index(joined, "alpha") < 0 || strings.Index(joined, "alpha") > strings.Index(joined, "beta") || len(fake.NewCalls) != 1 {
		t.Fatalf("queue did not resume in order on the original session: %v", prompts)
	}
}

func TestDisconnectAndCancelDoNotLetLateCallbacksOverwrite(t *testing.T) {
	fake := agent.NewFake()
	started := make(chan struct{})
	release := make(chan struct{})
	fake.PromptFn = func(_ string, text string) agent.PromptResult {
		if strings.Contains(text, "active") {
			close(started)
			<-release
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: text})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "active"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	<-started
	queuedJob, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "queued-work"})
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "kept-work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CancelRequest(context.Background(), id, queuedJob.AcceptedRequestID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CancelRequest(context.Background(), id, kept.AcceptedRequestID, "stale-turn"); err == nil {
		t.Fatal("mismatched cancel target was accepted")
	}
	if s.snapshot(id).QueueLength != 1 {
		t.Fatalf("cancel removed another queued request: %+v", s.snapshot(id))
	}
	active := s.snapshot(id)
	s.applyPromptResult(id, queued{requestID: queuedJob.AcceptedRequestID, turnID: "late", gen: s.currentGen(id) - 1, text: "late"}, agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "overwrite"})})
	if got := s.snapshot(id); got.LastSummary == "overwrite" || (got.RequestID == queuedJob.AcceptedRequestID && got.State == protocol.StateCompleted) {
		t.Fatalf("late callback overwrote the active request: %+v", got)
	}
	s.Disconnect(id)
	close(release)
	time.Sleep(50 * time.Millisecond)
	restored, err := s.store.QueuedRequests(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].RequestID != kept.AcceptedRequestID {
		t.Fatalf("disconnect dropped or reordered the queue: %+v", restored)
	}
	if len(fake.NewCalls) != 1 || active.GrokSessionID == "" {
		t.Fatal("disconnect created a replacement session")
	}
}

func TestReportDueDoesNotEnqueueProgress(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "quiet"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StateCompleted)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	out, err := s.Wait(ctx, protocol.WaitRequest{JobIDs: []string{id}, TimeoutSec: 0})
	if err != nil || out.Reason != "boundary" {
		t.Fatalf("completed boundary: %+v %v", out, err)
	}
	before := s.snapshot(id).QueueLength
	due, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{id}, Cursors: out.Cursors, TimeoutSec: 1})
	if err != nil {
		t.Fatal(err)
	}
	if due.Reason != "report_due" || due.Cursors[id] != out.Cursors[id] || s.snapshot(id).QueueLength != before {
		t.Fatalf("report_due changed the wait cursor or queued progress: %+v", due)
	}
}
