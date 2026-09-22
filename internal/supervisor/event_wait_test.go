package supervisor

import (
	"context"
	"errors"
	"grokmcp/internal/agent"
	"grokmcp/internal/core"
	"grokmcp/internal/ipc"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"net"
	"sync"
	"testing"
	"time"
)

func seedWaitJob(t *testing.T, s *Service, id string, state protocol.JobState) protocol.Job {
	t.Helper()
	j := protocol.Job{JobID: id, RequestID: id + "-request", State: state, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: s.clock.Now(), UpdatedAt: s.clock.Now(), LastSummary: "old result", PlanSummary: "full plan"}
	if err := s.put(j, 0, nil); err != nil {
		t.Fatal(err)
	}
	return s.snapshot(id)
}
func TestWaitReportDueAndCursorReplay(t *testing.T) {
	if defaultWaitTimeout != 300*time.Second {
		t.Fatal(defaultWaitTimeout)
	}
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	j := seedWaitJob(t, s, "wait", protocol.StatePlanReady)
	first, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{j.JobID}})
	if err != nil || first.Reason != "boundary" || first.Cursors[j.JobID] == 0 {
		t.Fatalf("%+v %v", first, err)
	}
	second, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{j.JobID}, Cursors: first.Cursors, TimeoutSec: 1})
	if err != nil || second.Reason != "report_due" || second.Jobs[0].PlanSummary != "" {
		t.Fatalf("%+v %v", second, err)
	}
	// 不发送实时通知，验证仅凭持久化游标也可补读。
	j.State = protocol.StateNeedsInput
	j.LastSummary = "new input"
	s.save(j)
	third, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{j.JobID}, Cursors: second.Cursors})
	if err != nil || third.Reason != "boundary" || third.Cursors[j.JobID] <= second.Cursors[j.JobID] {
		t.Fatalf("%+v %v", third, err)
	}
}
func TestWaitAllDoesNotConsumePartialBoundary(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	a := seedWaitJob(t, s, "a", protocol.StateNeedsInput)
	b := seedWaitJob(t, s, "b", protocol.StateExecuting)
	req := protocol.WaitRequest{JobIDs: []string{a.JobID, b.JobID}, Mode: "all", TimeoutSec: 1}
	out, err := s.Wait(context.Background(), req)
	if err != nil || out.Reason != "report_due" || out.Cursors[a.JobID] != 0 {
		t.Fatalf("%+v %v", out, err)
	}
	b.State = protocol.StateCompleted
	s.save(b)
	req.Cursors = out.Cursors
	out, err = s.Wait(context.Background(), req)
	if err != nil || out.Reason != "boundary" || len(out.Boundaries) != 2 {
		t.Fatalf("%+v %v", out, err)
	}
}
func TestWaitRejectsInvalidAndMissingJobs(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	j := seedWaitJob(t, s, "a", protocol.StateCompleted)
	for _, req := range []protocol.WaitRequest{{}, {JobIDs: []string{"missing"}}, {JobIDs: []string{j.JobID, "missing"}, Mode: "all"}, {JobIDs: []string{j.JobID}, Mode: "wrong"}, {JobIDs: []string{j.JobID}, TimeoutSec: -1}, {JobIDs: []string{j.JobID}, Cursors: map[string]int64{j.JobID: j.EventCursor + 1}}, {JobIDs: []string{j.JobID, j.JobID}}} {
		if _, err := s.Wait(context.Background(), req); err == nil {
			t.Fatalf("accepted %+v", req)
		}
	}
}
func TestWaitSubscribeRaceAndCancellation(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	j := seedWaitJob(t, s, "a", protocol.StateExecuting)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		out, err := s.Wait(ctx, protocol.WaitRequest{JobIDs: []string{j.JobID}})
		if err == nil && out.Reason != "boundary" {
			err = errors.New("missing boundary")
		}
		done <- err
	}()
	j.State = protocol.StateNeedsInput
	s.save(j)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx2, stop := context.WithCancel(context.Background())
	stop()
	_, err := s.Wait(ctx2, protocol.WaitRequest{JobIDs: []string{j.JobID}, Cursors: map[string]int64{j.JobID: s.snapshot(j.JobID).EventCursor}})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if got := s.snapshot(j.JobID); got.State != protocol.StateNeedsInput {
		t.Fatal(got.State)
	}
}
func TestCompletedReplanApprovalResumesOriginalCall(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "GROK_OK"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "connectivity"}}})
	if err != nil {
		t.Fatal(err)
	}
	initial := waitState(t, s, res.Jobs[0].JobID, protocol.StateCompleted)
	block := make(chan struct{})
	fake.PromptBlock[initial.GrokSessionID] = block
	fake.PlanReadyBeforeBlock = "repair plan"
	next, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: initial.JobID, Prompt: "repair", Replan: true})
	if err != nil {
		t.Fatal(err)
	}
	if next.RequestID == initial.RequestID || next.LastSummary == "GROK_OK" || next.GrokSessionID != initial.GrokSessionID {
		t.Fatalf("stale request %+v", next)
	}
	plan := waitState(t, s, initial.JobID, protocol.StatePlanReady)
	if !plan.Busy || plan.PlanTurnID == "" {
		t.Fatalf("missing active approval %+v", plan)
	}
	decide := protocol.PlanDecideRequest{JobID: plan.JobID, Decide: protocol.PlanApprove, ApprovalID: plan.ApprovalID, RequestID: plan.RequestID, TurnID: plan.PlanTurnID, PlanVersion: plan.PlanVersion}
	stale := decide
	stale.PlanVersion++
	if _, err := s.PlanDecide(context.Background(), stale); err == nil {
		t.Fatal("stale approval accepted")
	}
	if _, err := s.PlanDecide(context.Background(), decide); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanDecide(context.Background(), decide); err == nil {
		t.Fatal("duplicate approval accepted")
	}
	close(block)
	done := waitState(t, s, initial.JobID, protocol.StateCompleted)
	if done.Busy || done.QueueLength != 0 || done.ActiveTurnID != "" || done.RequestID != plan.RequestID || fake.PromptCount() != 2 {
		t.Fatalf("duplicate or unfinished execution %+v prompts=%d", done, fake.PromptCount())
	}
	boundary, err := s.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{initial.JobID}, Cursors: map[string]int64{initial.JobID: plan.EventCursor}})
	if err != nil || len(boundary.Boundaries) != 1 || boundary.Boundaries[0].ActiveTurnID != "" {
		t.Fatalf("unfinished durable completion %+v %v", boundary, err)
	}
	history, _ := s.Events(context.Background(), initial.JobID)
	if len(history) < 3 {
		t.Fatal("history lost")
	}
}
func TestCancelledLateResultCannotCompleteNewRequest(t *testing.T) {
	fake := agent.NewFake()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	fake.PromptFn = func(string, string) agent.PromptResult {
		once.Do(func() { close(entered); <-release })
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	id := res.Jobs[0].JobID
	<-entered
	old := s.snapshot(id)
	if _, err := s.CancelTurn(context.Background(), id, "other-turn"); err == nil {
		t.Fatal("stale cancel accepted")
	}
	if _, err := s.CancelTurn(context.Background(), id, old.ActiveTurnID); err != nil {
		t.Fatal(err)
	}
	s.onPlanTurnReady(old.GrokSessionID, old.ActiveTurnID, "late plan")
	if j := s.snapshot(id); j.State != protocol.StateNeedsInput {
		t.Fatalf("late callback %+v", j)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for s.snapshot(id).Busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if j := s.snapshot(id); j.State != protocol.StateNeedsInput {
		t.Fatalf("late result %+v", j)
	}
}

func TestCompletionWaitsForQueuedWork(t *testing.T) {
	fake := agent.NewFake()
	first, release, second, releaseSecond := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	n := 0
	fake.PromptFn = func(string, string) agent.PromptResult {
		n++
		if n == 1 {
			close(first)
			<-release
		} else {
			close(second)
			<-releaseSecond
		}
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "finished"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	id := res.Jobs[0].JobID
	<-first
	if _, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "next"}); err != nil {
		t.Fatal(err)
	}
	invalid := make(chan protocol.Job, 1)
	off := s.Subscribe(func(ev protocol.Event) {
		if ev.Job != nil && ev.Job.State == protocol.StateCompleted && (ev.Job.Busy || ev.Job.QueueLength > 0) {
			select {
			case invalid <- *ev.Job:
			default:
			}
		}
	})
	defer off()
	close(release)
	<-second
	if j := s.snapshot(id); j.State == protocol.StateCompleted {
		t.Fatalf("premature completion %+v", j)
	}
	close(releaseSecond)
	waitState(t, s, id, protocol.StateCompleted)
	select {
	case j := <-invalid:
		t.Fatalf("invalid published completion %+v", j)
	default:
	}
	history, _ := s.Events(context.Background(), id)
	results := 0
	for _, e := range history {
		if e.EventType == "request_completed" {
			results++
		}
	}
	if results != 2 {
		t.Fatalf("lost queued results %+v", history)
	}
}

type hiddenWorker struct{ *terminal.Fake }

func (hiddenWorker) WorkerAlive(string) bool { return true }
func TestClosedViewerReportsControlWait(t *testing.T) {
	s := newTest(t, agent.NewFake(), hiddenWorker{terminal.NewFake()})
	j := seedWaitJob(t, s, "control", protocol.StateExecuting)
	j.GrokSessionID = "original"
	j.InputOwner = protocol.OwnerTUI
	s.save(j)
	rt := s.runtime(j.JobID)
	s.mu.Lock()
	rt.queue = []queued{{requestID: j.RequestID}}
	s.mu.Unlock()
	s.checkJobStall(j, s.clock.Now())
	s.checkJobStall(j, s.clock.Now().Add(time.Minute))
	got := s.snapshot(j.JobID)
	if got.WaitReason != "input_control" || got.Stalled {
		t.Fatalf("hidden worker misclassified %+v", got)
	}
}

func TestReplanWhileExecutingReachesApproval(t *testing.T) {
	fake := agent.NewFake()
	block := make(chan struct{})
	fake.PromptBlock["sess-1"] = block
	fake.PlanReadyBeforeBlock = "new approval"
	s := newTest(t, fake, terminal.NewFake())
	j := seedWaitJob(t, s, "executing", protocol.StateCompleted)
	j.Approved = true
	j.GrokSessionID = "sess-1"
	s.save(j)
	if _, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: j.JobID, Prompt: "continue approved work"}); err != nil {
		t.Fatal(err)
	}
	plan := waitState(t, s, j.JobID, protocol.StatePlanReady)
	if !plan.Busy {
		t.Fatal("approval lost active turn")
	}
	if _, err := decideCurrent(s, context.Background(), protocol.PlanDecideRequest{JobID: j.JobID, Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	close(block)
	waitState(t, s, j.JobID, protocol.StateCompleted)
}

// 旧业务用例也按新版消费者协议，从当前快照携带完整审批绑定。
func decideCurrent(s *Service, ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
	j, err := s.Status(ctx, req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	req.ApprovalID = j.ApprovalID
	req.RequestID = j.RequestID
	req.TurnID = j.PlanTurnID
	req.PlanVersion = j.PlanVersion
	return s.PlanDecide(ctx, req)
}

func TestMCPConnectionCloseDoesNotStopBackgroundTask(t *testing.T) {
	fake := agent.NewFake()
	hold := make(chan struct{})
	fake.PromptBlock["sess-1"] = hold
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "background"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	deadline := time.Now().Add(time.Second)
	for !s.snapshot(id).Busy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := ipc.Serve(ln, waitIPCBackend{svc: s})
	defer server.Close()
	client, err := ipc.Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan error, 1)
	go func() {
		_, err := client.Wait(context.Background(), protocol.WaitRequest{JobIDs: []string{id}})
		waiting <- err
	}()
	client.Close()
	select {
	case err := <-waiting:
		if err == nil {
			t.Fatal("disconnected wait succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("wait leaked")
	}
	if job := s.snapshot(id); !job.Busy || job.GrokSessionID != "sess-1" {
		t.Fatalf("IPC close cancelled background job %+v", job)
	}
	close(hold)
	waitState(t, s, id, protocol.StateCompleted)
}

type waitIPCBackend struct {
	core.Backend
	svc *Service
}

func (b waitIPCBackend) Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	return b.svc.Wait(ctx, req)
}
func (b waitIPCBackend) Subscribe(fn func(protocol.Event)) func() { return b.svc.Subscribe(fn) }

func TestManualResumeRestoresQueuedRequestInSameSession(t *testing.T) {
	fake := agent.NewFake()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	fake.PromptFn = func(string, string) agent.PromptResult {
		once.Do(func() { close(entered); <-release })
		return agent.PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"})}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "initial"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	<-entered
	queued, err := s.Followup(context.Background(), protocol.FollowupRequest{JobID: id, Prompt: "saved queued request"})
	if err != nil {
		t.Fatal(err)
	}
	s.Disconnect(id)
	// 原调用还没退栈，人工继续也必须恢复已经落盘的排队请求。
	if _, err := s.Continue(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(release)
	done := waitState(t, s, id, protocol.StateCompleted)
	if done.RequestID != queued.AcceptedRequestID || done.GrokSessionID != "sess-1" || fake.PromptCount() != 3 {
		t.Fatalf("manual resume lost request %+v calls=%v", done, fake.PromptSnapshot())
	}
}

func TestApprovalCallbackFromEveryActiveStage(t *testing.T) {
	for _, state := range []protocol.JobState{protocol.StateStarting, protocol.StatePlanning, protocol.StateExecuting, protocol.StateRecovering} {
		t.Run(string(state), func(t *testing.T) {
			s := newTest(t, agent.NewFake(), terminal.NewFake())
			job := seedWaitJob(t, s, "job", state)
			job.GrokSessionID = "session"
			s.save(job)
			rt := s.runtime(job.JobID)
			s.mu.Lock()
			rt.busy = true
			rt.turnID = "turn"
			rt.requestID = job.RequestID
			s.mu.Unlock()
			s.onPlanTurnReady("session", "turn", "new plan")
			if got := s.snapshot(job.JobID); got.State != protocol.StatePlanReady || got.PlanTurnID != "turn" || !got.Busy {
				t.Fatalf("approval dropped %+v", got)
			}
		})
	}
}

func (s *Service) onPlanTurnReady(sid, turn, text string) {
	id := s.jobIDForSession(sid)
	j := s.snapshot(id)
	_ = s.onApproval(agent.PlanRequest{ID: "test-" + turn, ConnectionID: "test", SessionID: sid, RequestID: j.RequestID, TurnID: turn, Content: text, CreatedAt: time.Now()})
}
func (s *Service) becomePlanReady(j protocol.Job, text string) {
	current := s.snapshot(j.JobID)
	s.onPlanTurnReady(j.GrokSessionID, current.ActiveTurnID, text)
}
