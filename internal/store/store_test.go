package store

import (
	"encoding/json"
	"path/filepath"
	"slices"
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

func TestDebugSettingsRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	st := protocol.Settings{DefaultViewMode: "headless", DebugEnabled: true, DebugPayloads: true}
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	got, err := s.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.DebugEnabled || !got.DebugPayloads {
		t.Fatalf("%+v", got)
	}
}

func putJob(t *testing.T, s *Store, j protocol.Job) {
	t.Helper()
	if err := s.PutJob(Record{Job: j}); err != nil {
		t.Fatal(err)
	}
}

func TestListJobsPageSortAndCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sec := func(n int64) time.Time { return time.Unix(n, 0).UTC() }
	jobs := []protocol.Job{
		{JobID: "z", Cwd: "/tmp/z", Title: "z", State: protocol.StateExecuting, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(100), CreatedAt: sec(50)},
		{JobID: "a", Cwd: "/tmp/a", Title: "a", State: protocol.StateCompleted, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(200), CreatedAt: sec(10)},
		{JobID: "m", Cwd: "/tmp/m", Title: "m", State: protocol.StateNeedsInput, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(90), CreatedAt: sec(80)},
		{JobID: "c", Cwd: "/tmp/c", Title: "c", State: protocol.StateCreated, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(300), CreatedAt: sec(300)},
		{JobID: "f", Cwd: "/tmp/f", Title: "f", State: protocol.StateFailed, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(250), CreatedAt: sec(20)},
		{JobID: "y", Cwd: "/tmp/y", Title: "y", State: protocol.StateExecuting, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(100), CreatedAt: sec(60)},
		{JobID: "x", Cwd: "/tmp/x", Title: "x", State: protocol.StateExecuting, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, UpdatedAt: sec(100), CreatedAt: sec(60)},
	}
	for _, j := range jobs {
		putJob(t, s, j)
	}
	page, next, more, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !more || next == "" || len(page) != 3 {
		t.Fatalf("page1 len=%d more=%v next=%q", len(page), more, next)
	}
	got := []string{page[0].Job.JobID, page[1].Job.JobID, page[2].Job.JobID}
	if !slices.Equal(got, []string{"y", "x", "z"}) {
		t.Fatalf("page1 %v", got)
	}
	page2, next2, more2, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 3, Cursor: next})
	if err != nil {
		t.Fatal(err)
	}
	got = []string{page2[0].Job.JobID, page2[1].Job.JobID, page2[2].Job.JobID}
	if !slices.Equal(got, []string{"m", "c", "f"}) {
		t.Fatalf("page2 %v", got)
	}
	if !more2 || next2 == "" {
		t.Fatal("expected more after page2")
	}
	page3, _, more3, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 3, Cursor: next2})
	if err != nil {
		t.Fatal(err)
	}
	if more3 || len(page3) != 1 || page3[0].Job.JobID != "a" {
		t.Fatalf("page3 %+v more=%v", page3, more3)
	}
	if protocol.ActiveGroup(page2[1].Job.State) != 0 || page2[1].Job.State != protocol.StateCreated {
		t.Fatal("created must not be active")
	}
}

func TestListJobsPageHidesArchivedAndFilters(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(10, 0).UTC()
	putJob(t, s, protocol.Job{JobID: "live", Cwd: "/tmp/auth", Title: "登录", State: protocol.StateCompleted, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now})
	putJob(t, s, protocol.Job{JobID: "old", Cwd: "/tmp/auth", Title: "旧", State: protocol.StateFailed, ViewMode: protocol.ViewHeaded, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now, ArchivedAt: now})
	page, _, more, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 10})
	if err != nil || more || len(page) != 1 || page[0].Job.JobID != "live" {
		t.Fatalf("default %+v %v %v", page, more, err)
	}
	arch, _, _, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 10, IncludeArchived: true})
	if err != nil || len(arch) != 1 || arch[0].Job.JobID != "old" {
		t.Fatalf("archived %+v %v", arch, err)
	}
	if err := s.backfillProjects(); err != nil {
		t.Fatal(err)
	}
	liveRec, err := s.GetJob("live")
	if err != nil || liveRec.Job.ProjectID == "" {
		t.Fatalf("backfill project_id %+v %v", liveRec.Job, err)
	}
	q, _, _, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 10, Query: "登录", Project: liveRec.Job.ProjectID})
	if err != nil || len(q) != 1 || q[0].Job.JobID != "live" {
		t.Fatalf("query %+v %v", q, err)
	}
	headed, _, _, err := s.ListJobsPage(protocol.ListJobsQuery{Limit: 10, IncludeArchived: true, View: "headed"})
	if err != nil || len(headed) != 1 || headed[0].Job.JobID != "old" {
		t.Fatalf("view %+v %v", headed, err)
	}
}

func TestDeleteJobRemovesEventsKeepsOthers(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(10, 0).UTC()
	putJob(t, s, protocol.Job{JobID: "j1", Cwd: "/tmp/p", Title: "a", State: protocol.StateFailed, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now})
	putJob(t, s, protocol.Job{JobID: "j2", Cwd: "/tmp/p", Title: "b", State: protocol.StateFailed, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now})
	if err := s.AddEvent("j1", "failed", "x", now); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteJob("j1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetJob("j1"); err == nil {
		t.Fatal("j1 still present")
	}
	ev, err := s.Events("j1", 5)
	if err != nil || len(ev) != 0 {
		t.Fatalf("events leftover %+v %v", ev, err)
	}
	if _, err := s.GetJob("j2"); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateBackfillAndCanonicalDedup(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(10, 0).UTC()
	putJob(t, s, protocol.Job{JobID: "a", Cwd: "/tmp/same", Title: "a", State: protocol.StateCompleted, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now})
	putJob(t, s, protocol.Job{JobID: "b", Cwd: "/tmp/same/", Title: "b", State: protocol.StateFailed, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now.Add(time.Second)})
	if err := s.backfillProjects(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetJob("a")
	b, _ := s.GetJob("b")
	if a.Job.ProjectID == "" || a.Job.ProjectID != b.Job.ProjectID {
		t.Fatalf("project ids %q %q", a.Job.ProjectID, b.Job.ProjectID)
	}
	if a.Job.Project == "" {
		t.Fatal("missing project name")
	}
	p, err := s.GetProject(a.Job.ProjectID)
	if err != nil || p.Imported {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestDemoteProjectKeepsRowAndJobs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(20, 0).UTC()
	p := protocol.Project{ProjectID: "p1", Name: "keep", Root: "/tmp/keep", CanonicalPath: "/tmp/keep", Imported: true, CreatedAt: now, UpdatedAt: now, LastUsedAt: now}
	if err := s.PutProject(p); err != nil {
		t.Fatal(err)
	}
	putJob(t, s, protocol.Job{JobID: "j1", ProjectID: "p1", Cwd: "/tmp/keep", Title: "t", State: protocol.StateCompleted, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: now, UpdatedAt: now})
	got, err := s.DemoteProject("p1", now.Add(time.Minute))
	if err != nil || got.Imported || got.ProjectID != "p1" {
		t.Fatalf("%+v %v", got, err)
	}
	j, err := s.GetJob("j1")
	if err != nil || j.Job.ProjectID != "p1" {
		t.Fatalf("%+v %v", j.Job, err)
	}
	list, err := s.ListProjects()
	if err != nil || len(list) != 1 || list[0].ProjectID != "p1" {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestListProjectsActiveFirst(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	sec := func(n int64) time.Time { return time.Unix(n, 0).UTC() }
	if err := s.PutProject(protocol.Project{ProjectID: "old", Name: "old", Root: "/tmp/old", CanonicalPath: "/tmp/old", LastUsedAt: sec(500), CreatedAt: sec(1), UpdatedAt: sec(1)}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProject(protocol.Project{ProjectID: "live", Name: "live", Root: "/tmp/live", CanonicalPath: "/tmp/live", LastUsedAt: sec(10), CreatedAt: sec(1), UpdatedAt: sec(1)}); err != nil {
		t.Fatal(err)
	}
	putJob(t, s, protocol.Job{JobID: "j", ProjectID: "live", Cwd: "/tmp/live", Title: "t", State: protocol.StateExecuting, ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor, CreatedAt: sec(1), UpdatedAt: sec(1)})
	list, err := s.ListProjects()
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	if list[0].ProjectID != "live" || list[0].ActiveCount != 1 {
		t.Fatalf("order %+v", list)
	}
}
