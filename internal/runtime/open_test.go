package runtime

import (
	"context"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func TestOpenSecondConnects(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	a := agent.NewFake()
	a.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	b1, c1, err := Open(ctx, Options{Home: home, Agent: a, Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c1)
	res, err := b1.Dispatch(ctx, protocol.DispatchRequest{
		Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := b1.Status(ctx, res.Jobs[0].JobID)
		if j.State == protocol.StatePlanReady {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	b2, c2, err := Open(ctx, Options{Home: home, Agent: agent.NewFake(), Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c2)
	jobs, err := b2.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) == 0 {
		t.Fatal("second open did not see host jobs")
	}
}
