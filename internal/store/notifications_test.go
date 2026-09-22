package store

import (
	"fmt"
	"grokmcp/internal/protocol"
	"path/filepath"
	"testing"
)

func TestBoundaryAndResultRollbackWithState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec := Record{Job: protocol.Job{JobID: "j", RequestID: "r", State: protocol.StateExecuting}, Accepted: &WorkRequest{RequestID: "r", Prompt: "task"}}
	if err := s.PutJob(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_state BEFORE UPDATE ON jobs BEGIN SELECT RAISE(ABORT,'write failed'); END`); err != nil {
		t.Fatal(err)
	}
	rec.Job.State = protocol.StateCompleted
	rec.Result = &RequestResult{RequestID: "r", TurnID: "t", Summary: "done"}
	if err := s.PutJob(rec); err == nil {
		t.Fatal("expected injected failure")
	}
	got, err := s.GetJob("j")
	if err != nil || got.Job.State != protocol.StateExecuting || got.Job.EventCursor != 0 {
		t.Fatalf("uncommitted state %+v %v", got, err)
	}
	if _, ok, err := s.NotificationAfter("j", 0, "r"); err != nil || ok {
		t.Fatalf("uncommitted notification %v %v", ok, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM request_results`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncommitted result %d %v", count, err)
	}
}
func TestQueuedRequestSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutJob(Record{Job: protocol.Job{JobID: "j", RequestID: "current", State: protocol.StateExecuting}, Accepted: &WorkRequest{RequestID: "queued", Prompt: "new plan", Planning: true}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pending, err := s.QueuedRequests("j")
	if err != nil || len(pending) != 1 || pending[0].RequestID != "queued" || pending[0].Prompt != "new plan" || !pending[0].Planning {
		t.Fatalf("lost accepted request %+v %v", pending, err)
	}
}

func TestPersistedCursorReplaysAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rec := Record{Job: protocol.Job{JobID: "j", RequestID: "r", State: protocol.StatePlanReady, PlanVersion: 2, PlanTurnID: "turn", PlanSummary: "plan"}}
	if err := s.PutJob(rec); err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetJob("j")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, _ := s.GetJob("j")
	event, ok, err := s.NotificationAfter("j", 0, "r")
	if err != nil || !ok || event.EventCursor != before.Job.EventCursor || after.Job.EventCursor != before.Job.EventCursor || event.PlanTurnID != "turn" {
		t.Fatalf("lost durable boundary %+v %v %v", event, ok, err)
	}
	if _, ok, err := s.NotificationAfter("j", after.Job.EventCursor, "r"); err != nil || ok {
		t.Fatalf("replayed consumed boundary %v %v", ok, err)
	}
}

func TestWaitSnapshotNeverCrossesItsCommittedCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "snapshot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rec := Record{Job: protocol.Job{JobID: "j", RequestID: "r", State: protocol.StateNeedsInput, LastSummary: "0"}}
	if err := s.PutJob(rec); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 40; i++ {
			copy := rec
			copy.Job.LastSummary = fmt.Sprint(i)
			if err := s.PutJob(copy); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		snapshot, events, err := s.WaitSnapshot("j", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 0 {
			t.Fatal("lost committed boundary")
		}
		for _, e := range events {
			if e.EventCursor > snapshot.Job.EventCursor {
				t.Fatal("cursor crossed snapshot")
			}
		}
		last := events[len(events)-1]
		if last.EventCursor != snapshot.Job.EventCursor || last.LastSummary != snapshot.Job.LastSummary {
			t.Fatalf("mixed state/event snapshot %+v %+v", snapshot.Job, last)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
