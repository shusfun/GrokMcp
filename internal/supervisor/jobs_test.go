package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
	"grokmcp/internal/terminal"
	"grokmcp/internal/trace"
)

func TestArchiveRejectsActiveAndAllowsHistory(t *testing.T) {
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
	if _, err := s.ArchiveJob(context.Background(), id); !errors.Is(err, errArchiveActive) {
		t.Fatalf("archive active: %v", err)
	}
	job, _ := s.load(id)
	job.State = protocol.StateCompleted
	s.touch(&job)
	s.save(job)
	got, err := s.ArchiveJob(context.Background(), id)
	if err != nil || got.ArchivedAt.IsZero() {
		t.Fatalf("%+v %v", got, err)
	}
	page, err := s.ListJobsPage(context.Background(), protocol.ListJobsQuery{Limit: 10})
	if err != nil || len(page.Jobs) != 0 {
		t.Fatalf("archived still listed %+v %v", page, err)
	}
	arch, err := s.ListJobsPage(context.Background(), protocol.ListJobsQuery{Limit: 10, IncludeArchived: true})
	if err != nil || len(arch.Jobs) != 1 || arch.Jobs[0].JobID != id {
		t.Fatalf("include archived %+v %v", arch, err)
	}
	got, err = s.UnarchiveJob(context.Background(), id)
	if err != nil || !got.ArchivedAt.IsZero() {
		t.Fatalf("unarchive %+v %v", got, err)
	}
}

func TestDeleteRejectsActiveCleansTraceNotGrokSessions(t *testing.T) {
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
	if err := s.DeleteJob(context.Background(), id); !errors.Is(err, errDeleteActive) {
		t.Fatalf("delete active: %v", err)
	}
	sessDir := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(sessDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sessFile := filepath.Join(sessDir, "keep.json")
	if err := os.WriteFile(sessFile, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	job, _ := s.load(id)
	job.State = protocol.StateFailed
	s.save(job)
	s.traces.Emit(trace.Event{JobID: id, Level: trace.LevelInfo, Source: trace.SourceSupervisor, Name: "job.created"})
	if err := s.DeleteJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.GetJob(id); err == nil {
		t.Fatal("job still in store")
	}
	if _, err := os.Stat(sessFile); err != nil {
		t.Fatalf("grok session touched: %v", err)
	}
	if err := s.DeleteJob(context.Background(), id); err != nil {
		t.Fatal(err)
	}
}

func TestContinueTouchesUpdatedAt(t *testing.T) {
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
	job, _ := s.load(id)
	job.State = protocol.StateNeedsInput
	s.save(job)
	before := job.UpdatedAt
	s.clock.(*clock.Fake).Advance(2 * time.Second)
	got, err := s.Continue(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !got.UpdatedAt.After(before) {
		t.Fatalf("updated_at %s not after %s", got.UpdatedAt, before)
	}
}

func TestListJobsPageCreatedNotActive(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	now := time.Unix(50, 0).UTC()
	if err := s.store.PutJob(store.Record{Job: protocol.Job{
		JobID: "c1", Cwd: "/tmp/p", Title: "leftover", State: protocol.StateCreated,
		ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor,
		CreatedAt: now, UpdatedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListJobsPage(context.Background(), protocol.ListJobsQuery{Limit: 10})
	if err != nil || len(page.Jobs) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	if protocol.DashboardActive(page.Jobs[0].State) || protocol.ActiveGroup(page.Jobs[0].State) != 0 {
		t.Fatalf("created grouped as active: %+v", page.Jobs[0])
	}
	if _, err := s.ArchiveJob(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
}

func TestHeadedVerifyFailureDoesNotPersist(t *testing.T) {
	fake := agent.NewFake()
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	term := terminal.NewFake()
	term.NoPID = true
	s := newTest(t, fake, term)
	res, _ := s.Dispatch(context.Background(), protocol.DispatchRequest{
		Cwd: "/tmp/p", Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	id := res.Jobs[0].JobID
	waitState(t, s, id, protocol.StatePlanReady)
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err == nil {
		t.Fatal("expected verify failure")
	}
	if j.ViewMode == protocol.ViewHeaded || j.InputOwner == protocol.OwnerTUI {
		t.Fatalf("persisted headed %+v", j)
	}
}

func TestExistingResumeDoesNotSpawn(t *testing.T) {
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
	term.SeedWindow("sess-1")
	j, err := s.SetView(context.Background(), protocol.SetViewRequest{JobID: id, View: protocol.ViewHeaded})
	if err != nil {
		t.Fatal(err)
	}
	if j.ViewMode != protocol.ViewHeaded {
		t.Fatalf("%+v", j)
	}
	if term.ResumeCount() != 0 {
		t.Fatalf("spawned resume %v", term.ResumeSnapshot())
	}
}
