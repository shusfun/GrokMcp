package agent

import (
	"context"
	"errors"
	"grokmcp/internal/protocol"
)

func (g *Grok) SetPlanHandler(fn func(PlanRequest) error) {
	g.mu.Lock()
	g.planHandler = fn
	g.mu.Unlock()
}
func (g *Grok) SetActivityHandler(fn func(Activity)) {
	g.mu.Lock()
	g.activityHandler = fn
	g.mu.Unlock()
}
func (g *Grok) PendingApproval(id string) (PlanRequest, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.approvals[id]
	if !ok || p.ConnectionID != g.connectionID || g.pending[p.SessionID] == nil {
		return PlanRequest{}, ErrNoPlanPermission
	}
	return p, nil
}
func (g *Grok) ResolveApproval(ctx context.Context, d PlanDecision) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.approvals[d.ID]
	if !ok || p.ConnectionID != g.connectionID || p.ConnectionID != d.ConnectionID || p.SessionID != d.SessionID || p.RequestID != d.RequestID || p.TurnID != d.TurnID {
		return errors.New("stale approval identity")
	}
	ch := g.pending[p.SessionID]
	if ch == nil {
		return ErrNoPlanPermission
	}
	if d.Notes != "" && !p.SupportsNotes {
		return errors.New("this ACP permission does not support approval notes")
	}
	if d.Decide != protocol.PlanApprove && d.Decide != protocol.PlanRevise && d.Decide != protocol.PlanCancel {
		return errors.New("invalid plan decision")
	}
	select {
	case ch <- planChoice{decide: d.Decide, notes: d.Notes}:
		delete(g.pending, p.SessionID)
		delete(g.pendingNotes, p.SessionID)
		delete(g.approvals, p.ID)
		g.plan[p.SessionID] = false
		return nil
	default:
		return errors.New("plan decision already submitted")
	}
}
