package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"grokmcp/internal/protocol"
)

func TestPutGetList(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	rec := Record{Job: protocol.Job{
		JobID: "j1", Title: "Runtime", Cwd: "/tmp/suiyuan", State: protocol.StatePlanning,
		ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now,
	}}
	if err := s.PutJob(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetJob("j1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Job.Project != "suiyuan" || got.Job.Title != "Runtime" {
		t.Fatalf("%+v", got.Job)
	}
	list, err := s.ListJobs()
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if err := s.AddEvent("j1", "plan_ready", "Plan ready", now); err != nil {
		t.Fatal(err)
	}
	ev, err := s.Events("j1", 5)
	if err != nil || len(ev) != 1 || ev[0].Summary != "Plan ready" {
		t.Fatalf("events %+v %v", ev, err)
	}
}

func TestEventsEmptyJSONArray(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev, err := s.Events("missing", 5)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("Events returned nil slice")
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "[]" {
		t.Fatalf("Events JSON = %s, want []", raw)
	}
}
