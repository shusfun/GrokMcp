package supervisor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

type countedAgent struct {
	*agent.Fake
	ensures, diagnoses, releases    atomic.Int32
	releaseEntered, releaseContinue chan struct{}
}

func (a *countedAgent) EnsureLeader(ctx context.Context) error {
	a.ensures.Add(1)
	return a.Fake.EnsureLeader(ctx)
}
func (a *countedAgent) Diagnose(ctx context.Context) protocol.DiagnoseResult {
	a.diagnoses.Add(1)
	return a.Fake.Diagnose(ctx)
}
func (a *countedAgent) Release() error {
	a.releases.Add(1)
	if a.releaseEntered != nil {
		close(a.releaseEntered)
		<-a.releaseContinue
	}
	return a.Fake.Release()
}

func seedLifecycleJob(t *testing.T, s *Service, id string, state protocol.JobState) {
	t.Helper()
	now := s.clock.Now()
	j := protocol.Job{JobID: id, GrokSessionID: "original-" + id, Cwd: t.TempDir(), Title: "fixture", State: state, ViewMode: protocol.ViewHeaded, DesiredViewMode: protocol.ViewHeaded, InputOwner: protocol.OwnerTUI, CreatedAt: now, UpdatedAt: now, PlanSummary: "keep plan", PlanDigest: "keep digest"}
	if err := s.put(j, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestColdStartAndStatusNeverStartOrDiagnoseAgent(t *testing.T) {
	f := agent.NewFake()
	s := newTest(t, f, terminal.NewFake())
	a := &countedAgent{Fake: f}
	s.agent = a
	seedLifecycleJob(t, s, "running", protocol.StateExecuting)
	seedLifecycleJob(t, s, "approval", protocol.StatePlanReady)
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := s.StatusBar(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListJobs(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if a.ensures.Load() != 0 || a.diagnoses.Load() != 0 || len(f.LoadSnapshot()) != 0 || f.PromptCount() != 0 {
		t.Fatal("read-only startup launched agent work")
	}
	running, _ := s.Status(context.Background(), "running")
	plan, _ := s.Status(context.Background(), "approval")
	if running.State != protocol.StateDisconnected || running.InputOwner != protocol.OwnerSupervisor || running.DesiredViewMode != protocol.ViewHeadless {
		t.Fatalf("running job not parked: %+v", running)
	}
	if plan.State != protocol.StatePlanReady || plan.PlanSummary != "keep plan" || plan.GrokSessionID != "original-approval" {
		t.Fatalf("approval lost: %+v", plan)
	}
	if _, err := s.Continue(context.Background(), "running"); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "running", protocol.StateCompleted)
	if len(f.NewCalls) != 0 || len(f.LoadSnapshot()) != 1 || f.LoadSnapshot()[0][:len("original-running")] != "original-running" {
		t.Fatalf("did not resume original: %v", f.LoadSnapshot())
	}
	if _, err := s.PlanDecide(context.Background(), protocol.PlanDecideRequest{JobID: "approval", Decide: protocol.PlanApprove}); err != nil {
		t.Fatal(err)
	}
	waitState(t, s, "approval", protocol.StateCompleted)
	if len(f.NewCalls) != 0 {
		t.Fatal("approval created replacement session")
	}
}

func TestDisconnectDoesNotLoadOrRetry(t *testing.T) {
	f := agent.NewFake()
	s := newTest(t, f, terminal.NewFake())
	a := &countedAgent{Fake: f}
	s.agent = a
	seedLifecycleJob(t, s, "running", protocol.StateExecuting)
	s.Disconnect("running")
	if a.ensures.Load() != 0 || len(f.LoadSnapshot()) != 0 || f.PromptCount() != 0 {
		t.Fatal("disconnect performed recovery")
	}
	j, _ := s.Status(context.Background(), "running")
	if j.State != protocol.StateDisconnected {
		t.Fatal(j.State)
	}
}

func TestIdleReleaseWaitsForThirtySecondsAndProtectsWork(t *testing.T) {
	for _, kind := range []string{"idle", "busy", "queue", "approval", "stored-approval", "terminal", "operation"} {
		t.Run(kind, func(t *testing.T) {
			f := agent.NewFake()
			s := newTest(t, f, terminal.NewFake())
			a := &countedAgent{Fake: f}
			s.agent = a
			rt := s.runtime("fixture")
			switch kind {
			case "busy":
				rt.busy = true
			case "queue":
				rt.queue = []queued{{kind: "followup"}}
			case "approval":
				seedLifecycleJob(t, s, "approval", protocol.StatePlanReady)
				f.Sessions["original-approval"] = "fixture"
			case "stored-approval":
				seedLifecycleJob(t, s, "approval", protocol.StatePlanReady)
			case "terminal":
				rt.attaching = true
			case "operation":
				finish, err := s.beginOperation(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer finish()
			}
			now := s.clock.Now()
			s.releaseIfIdle(now)
			s.releaseIfIdle(now.Add(29 * time.Second))
			if a.releases.Load() != 0 {
				t.Fatal("released too early")
			}
			s.releaseIfIdle(now.Add(30 * time.Second))
			want := int32(0)
			if kind == "idle" || kind == "stored-approval" {
				want = 1
			}
			if a.releases.Load() != want {
				t.Fatalf("releases=%d want=%d", a.releases.Load(), want)
			}
		})
	}
}

func TestNewOperationWaitsForIdleCleanup(t *testing.T) {
	f := agent.NewFake()
	s := newTest(t, f, terminal.NewFake())
	a := &countedAgent{Fake: f, releaseEntered: make(chan struct{}), releaseContinue: make(chan struct{})}
	s.agent = a
	now := s.clock.Now()
	s.releaseIfIdle(now)
	done := make(chan struct{})
	go func() { s.releaseIfIdle(now.Add(30 * time.Second)); close(done) }()
	<-a.releaseEntered
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := s.beginOperation(ctx); err == nil {
		t.Fatal("new operation raced old process cleanup")
	}
	close(a.releaseContinue)
	<-done
	finish, err := s.beginOperation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	finish()
}
