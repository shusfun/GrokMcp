package supervisor

import (
	"context"
	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovalContentNeverFallsBackToStalePlan(t *testing.T) {
	s := newTest(t, agent.NewFake(), terminal.NewFake())
	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte("old plan"), 0600); err != nil {
		t.Fatal(err)
	}
	s.planPath = func(string, string) string { return path }
	j := seedWaitJob(t, s, "content", protocol.StateExecuting)
	j.GrokSessionID = "session"
	j.PlanSummary = "old saved plan"
	s.save(j)
	rt := s.runtime(j.JobID)
	s.mu.Lock()
	rt.busy = true
	rt.requestID = j.RequestID
	rt.turnID = "turn"
	rt.turnStartedAt = time.Now().Add(time.Second)
	s.mu.Unlock()
	p := agent.PlanRequest{ID: "approval-1", ConnectionID: "conn", SessionID: j.GrokSessionID, RequestID: j.RequestID, TurnID: "turn", Content: "fresh wire plan", CreatedAt: time.Now()}
	if err := s.onApproval(p); err != nil {
		t.Fatal(err)
	}
	if got := s.snapshot(j.JobID); got.PlanSummary != "fresh wire plan" {
		t.Fatalf("stale file won %+v", got)
	}
	p.ID = "approval-2"
	p.Content = ""
	if err := s.onApproval(p); err != nil {
		t.Fatal(err)
	}
	got := s.snapshot(j.JobID)
	if got.PlanSummary != "" || got.PauseReason != "plan_content_missing" || got.ApprovalID != p.ID || got.State != protocol.StatePlanReady {
		t.Fatalf("missing real request discarded or stale fallback %+v", got)
	}
}
func TestAnswerWithoutMarkerIsSavedWithoutRepairPrompt(t *testing.T) {
	fake := agent.NewFake()
	answer := strings.Repeat("已经实现🙂", 100)
	fake.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{Text: answer, StopReason: "end_turn"}
	}
	s := newTest(t, fake, terminal.NewFake())
	res, err := s.Dispatch(context.Background(), protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "inspect"}}})
	if err != nil {
		t.Fatal(err)
	}
	j := waitState(t, s, res.Jobs[0].JobID, protocol.StateNeedsInput)
	if j.PauseReason != "review_required" || fake.PromptCount() != 1 || j.Busy {
		t.Fatalf("format repair or false completion %+v", j)
	}
	first, err := s.Status(context.Background(), j.JobID, protocol.ResultQuery{IncludeResult: true, RequestID: j.RequestID, Limit: 9})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Status(context.Background(), j.JobID, protocol.ResultQuery{IncludeResult: true, RequestID: j.RequestID, TurnID: first.Result.TurnID, Offset: first.Result.NextOffset})
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Text+second.Result.Text != answer {
		t.Fatal("paged answer changed")
	}
	if _, err := s.Status(context.Background(), j.JobID, protocol.ResultQuery{IncludeResult: true, RequestID: "another-request"}); err == nil {
		t.Fatal("old answer leaked into another request")
	}
}

func TestPlanPathResolvesSameSessionAcrossWindowsSeparators(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "C%3A%2Fwork%2Fproject", "original")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if got := resolvePlanPath(root, `C:\work\project`, "original"); got != filepath.Join(dir, "plan.md") {
		t.Fatalf("session path mismatch %q", got)
	}
	duplicate := filepath.Join(root, "C%3A%5Cwork%5Cproject", "original")
	if err := os.MkdirAll(duplicate, 0700); err != nil {
		t.Fatal(err)
	}
	if got := resolvePlanPath(root, `C:\work\project`, "original"); got != "" {
		t.Fatalf("ambiguous session silently selected %q", got)
	}
}
