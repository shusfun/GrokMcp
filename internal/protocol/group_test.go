package protocol

import (
	"slices"
	"testing"
	"time"
)

func TestDashboardGroups(t *testing.T) {
	active := []JobState{StateStarting, StatePlanning, StateExecuting, StateRecovering, StatePlanReady, StateNeedsInput, StateDisconnected}
	history := []JobState{StateCompleted, StateCancelled, StateFailed, StateBlocked}
	for _, s := range active {
		if !DashboardActive(s) || DashboardHistory(s) || ActiveGroup(s) != 1 {
			t.Fatalf("%s grouping", s)
		}
	}
	for _, s := range history {
		if DashboardActive(s) || !DashboardHistory(s) || ActiveGroup(s) != 0 {
			t.Fatalf("%s grouping", s)
		}
	}
	if DashboardActive(StateCreated) || DashboardHistory(StateCreated) || ActiveGroup(StateCreated) != 0 {
		t.Fatal("created must not be active or history")
	}
}

func TestCompareJobsOrder(t *testing.T) {
	sec := func(n int64) time.Time { return time.Unix(n, 0).UTC() }
	jobs := []Job{
		{JobID: "z", State: StateExecuting, UpdatedAt: sec(100), CreatedAt: sec(50)},
		{JobID: "a", State: StateCompleted, UpdatedAt: sec(200), CreatedAt: sec(10)},
		{JobID: "m", State: StateNeedsInput, UpdatedAt: sec(90), CreatedAt: sec(80)},
		{JobID: "c", State: StateCreated, UpdatedAt: sec(300), CreatedAt: sec(300)},
		{JobID: "f", State: StateFailed, UpdatedAt: sec(250), CreatedAt: sec(20)},
		{JobID: "y", State: StateExecuting, UpdatedAt: sec(100), CreatedAt: sec(60)},
		{JobID: "x", State: StateExecuting, UpdatedAt: sec(100), CreatedAt: sec(60)},
	}
	slices.SortFunc(jobs, CompareJobs)
	got := make([]string, len(jobs))
	for i, j := range jobs {
		got[i] = j.JobID
	}
	want := []string{"y", "x", "z", "m", "c", "f", "a"}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v want %v", got, want)
	}
}

func TestJobCursorRoundTrip(t *testing.T) {
	j := Job{JobID: "abc", State: StatePlanning, UpdatedAt: time.Unix(10, 0).UTC(), CreatedAt: time.Unix(5, 0).UTC()}
	cur, err := DecodeJobCursor(EncodeJobCursor(j))
	if err != nil {
		t.Fatal(err)
	}
	if cur.G != 1 || cur.U != 10 || cur.C != 5 || cur.I != "abc" {
		t.Fatalf("%+v", cur)
	}
	if _, err := DecodeJobCursor("not-a-cursor"); err == nil {
		t.Fatal("expected invalid cursor")
	}
}
