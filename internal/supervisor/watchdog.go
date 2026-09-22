package supervisor

import (
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/trace"
)

func (s *Service) watchLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopWatch:
			return
		case <-ticker.C:
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			s.inspectWatchdog(s.clock.Now())
			s.releaseIfIdle(s.clock.Now())
		}
	}
}

func (s *Service) inspectWatchdog(now time.Time) {
	recs, err := s.store.ListJobs()
	if err != nil {
		return
	}
	for _, rec := range recs {
		s.checkJobStall(rec.Job, now)
	}
}

func (s *Service) checkJobStall(job protocol.Job, now time.Time) {
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	qlen := len(rt.queue)
	busy := rt.busy
	turnID := rt.turnID
	h := rt.term
	if rt.stallSince == nil {
		rt.stallSince = map[string]time.Time{}
	}
	s.mu.Unlock()

	reason := ""
	need := time.Duration(0)
	live := h != nil && h.PID() > 0
	switch {
	case qlen > 0 && !busy && job.InputOwner == protocol.OwnerSupervisor:
		reason, need = "queue_nonempty_but_pump_idle", 5*time.Second
	case qlen > 0 && job.ViewMode == protocol.ViewHeaded && job.InputOwner == protocol.OwnerTUI && !live:
		reason, need = "stale_tui_owner", 3*time.Second
	case job.ViewMode == protocol.ViewDetaching:
		reason, need = "detach_timeout", 3*time.Second
	case job.State == protocol.StateExecuting && turnID == "" && qlen == 0 && !busy:
		reason, need = "executing_without_turn", 10*time.Second
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if reason == "" {
		if rt.stalled {
			rt.stalled = false
			rt.stalledReason = ""
		}
		rt.stallSince = map[string]time.Time{}
		return
	}
	since, ok := rt.stallSince[reason]
	if !ok {
		rt.stallSince = map[string]time.Time{reason: now}
		return
	}
	if now.Sub(since) < need {
		return
	}
	if rt.stalled && rt.stalledReason == reason {
		return
	}
	rt.stalled = true
	rt.stalledReason = reason
	job.Stalled = true
	job.StalledReason = reason
	s.mu.Unlock()
	s.emitTrace(job, "warn", trace.SourceSupervisor, "watchdog.stalled", reason, map[string]any{
		"reason": reason, "queue_length": qlen, "busy": busy,
	})
	s.mu.Lock()
}
