package agent

import (
	"context"
	"grokmcp/internal/protocol"
	"testing"
	"time"
)

func TestUnsupportedNotesDoNotReleasePermission(t *testing.T) {
	g := testGrok()
	ready := make(chan struct{})
	setTestPlanListener(g, func(string, string) { close(ready) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := g.awaitPlanChoice(ctx, "s", ""); done <- err }()
	<-ready
	if err := resolveTestPlan(g, ctx, "s", protocol.PlanApprove, "note"); err == nil {
		t.Fatal("unsupported note accepted")
	}
	select {
	case <-done:
		t.Fatal("permission was released")
	case <-time.After(10 * time.Millisecond):
	}
	if err := resolveTestPlan(g, ctx, "s", protocol.PlanApprove, ""); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func setTestPlanListener(g *Grok, fn func(string, string)) {
	g.SetPlanHandler(func(p PlanRequest) error { fn(p.SessionID, p.Content); return nil })
}
func resolveTestPlan(g *Grok, ctx context.Context, sid string, decision protocol.PlanDecision, notes string) error {
	g.mu.Lock()
	var p PlanRequest
	for _, r := range g.approvals {
		if r.SessionID == sid {
			p = r
			break
		}
	}
	g.mu.Unlock()
	if p.ID == "" {
		return ErrNoPlanPermission
	}
	return g.ResolveApproval(ctx, PlanDecision{PlanRequest: p, Decide: decision, Notes: notes})
}
