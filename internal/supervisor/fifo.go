package supervisor

import (
	"context"
	"fmt"
	"strings"

	"grokmcp/internal/protocol"
	"grokmcp/internal/trace"
)

func (s *Service) bumpGen(jobID string) {
	rt := s.runtime(jobID)
	s.mu.Lock()
	rt.gen++
	s.mu.Unlock()
}

func (s *Service) currentGen(jobID string) uint64 {
	rt := s.runtime(jobID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return rt.gen
}

func (s *Service) enqueue(jobID string, q queued) {
	rt := s.runtime(jobID)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	q.gen = rt.gen
	rt.queue = append(rt.queue, q)
	n := len(rt.queue)
	busy := rt.busy
	s.mu.Unlock()
	job, _ := s.load(jobID)
	s.emitTrace(job, "info", trace.SourceFIFO, "queue.enqueued", q.kind, map[string]any{"kind": q.kind, "queue_length": n})
	if !busy {
		s.pumps.Add(1)
		go func() {
			defer s.pumps.Done()
			s.pump(jobID)
		}()
	}
}

func (s *Service) kick(jobID string) {
	rt := s.runtime(jobID)
	s.mu.Lock()
	if s.closed || rt.busy || len(rt.queue) == 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.pumps.Add(1)
	go func() {
		defer s.pumps.Done()
		s.pump(jobID)
	}()
}

func (s *Service) liveGrok(jobID string) bool {
	rt := s.runtime(jobID)
	s.mu.Lock()
	h := rt.term
	s.mu.Unlock()
	return h != nil && h.PID() > 0
}

func (s *Service) isIdle(jobID string) bool {
	job, err := s.load(jobID)
	if err != nil {
		return false
	}
	if job.InputOwner == protocol.OwnerTUI || job.ViewMode == protocol.ViewDetaching {
		return false
	}
	rt := s.runtime(jobID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return !rt.busy && len(rt.queue) == 0
}

func (s *Service) attachAllowed(job protocol.Job) bool {
	if job.GrokSessionID == "" {
		return false
	}
	if job.InputOwner == protocol.OwnerTUI || job.ViewMode == protocol.ViewDetaching {
		return false
	}
	if job.State == protocol.StatePlanReady {
		return true
	}
	return s.isIdle(job.JobID)
}

func (s *Service) pump(jobID string) {
	for {
		job, err := s.load(jobID)
		rt := s.runtime(jobID)
		s.mu.Lock()
		closed := s.closed
		busy := rt.busy
		qlen := len(rt.queue)
		s.mu.Unlock()
		if closed {
			s.skipPump(job, "closed", qlen)
			return
		}
		if err == nil && job.ViewMode == protocol.ViewDetaching && qlen > 0 {
			s.skipPump(job, "detaching", qlen)
			return
		}
		if err == nil && job.InputOwner == protocol.OwnerTUI && qlen > 0 {
			reason := "tui_owns"
			if !s.liveGrok(jobID) {
				reason = "stale_tui_owner"
			}
			s.skipPump(job, reason, qlen)
			return
		}
		s.mu.Lock()
		if rt.busy || len(rt.queue) == 0 {
			s.mu.Unlock()
			if busy && qlen > 0 {
				s.skipPump(job, "already_busy", qlen)
			}
			return
		}
		item := rt.queue[0]
		rt.queue = rt.queue[1:]
		rt.busy = true
		rt.turnSeq++
		item.turnID = fmt.Sprintf("%s-%d", jobID, rt.turnSeq)
		rt.turnID = item.turnID
		ctx, cancel := context.WithCancel(context.Background())
		rt.cancel = cancel
		s.mu.Unlock()
		s.emitTrace(job, "info", trace.SourceFIFO, "queue.dequeued", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})
		s.emitTrace(job, "info", trace.SourceFIFO, "pump.started", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})

		s.runItem(ctx, jobID, item)

		s.mu.Lock()
		rt.busy = false
		rt.cancel = nil
		rt.turnID = ""
		more := len(rt.queue) > 0
		s.mu.Unlock()
		s.emitTrace(job, "info", trace.SourceFIFO, "pump.stopped", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})
		s.maybeAttachDesired(jobID)
		if !more {
			return
		}
	}
}

func (s *Service) skipPump(job protocol.Job, reason string, queueLength int) {
	s.emitTrace(job, "info", trace.SourceFIFO, "pump.skipped", "pump stopped because "+reason, map[string]any{"reason": reason, "queue_length": queueLength})
}

func (s *Service) maybeAttachDesired(jobID string) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	job, err := s.load(jobID)
	if err != nil || job.DesiredViewMode != protocol.ViewHeaded {
		return
	}
	if job.ViewMode == protocol.ViewHeaded || job.ViewMode == protocol.ViewDetaching {
		return
	}
	if !s.attachAllowed(job) {
		return
	}
	_, _ = s.launchTUI(context.Background(), job)
}

func (s *Service) runItem(ctx context.Context, jobID string, item queued) {
	job, err := s.load(jobID)
	if err != nil {
		return
	}
	if job.UserCancelled && item.kind != "cancel" {
		return
	}
	if job.ViewMode == protocol.ViewDetaching {
		s.skipPump(job, "detaching", 1)
		s.enqueue(jobID, item)
		return
	}
	if item.kind == "approve" {
		switch job.State {
		case protocol.StateCompleted, protocol.StateCancelled, protocol.StateFailed:
			return
		}
	}
	fields := trace.PromptFields(item.text, s.debugPayloads(jobID))
	fields["kind"] = item.kind
	s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.started", item.kind, fields)
	res, err := s.agent.Prompt(ctx, job.GrokSessionID, item.text)
	if item.gen != s.currentGen(jobID) {
		s.emitTrace(job, "debug", trace.SourceFIFO, "pump.skipped", "stale generation", map[string]any{"reason": "stale_generation"})
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.cancelled", "prompt cancelled", map[string]any{"turn_id": item.turnID})
			return
		}
		s.emitTrace(job, "error", trace.SourceACP, "acp.prompt.completed", err.Error(), map[string]any{"error": err.Error()})
		if isDisconnect(err) {
			s.Disconnect(jobID)
			return
		}
		job.State = protocol.StateFailed
		job.LastSummary = err.Error()
		job.LastAction = "Failed"
		s.touch(&job)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateFailed), job.LastSummary, s.clock.Now())
		s.save(job)
		return
	}
	s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.completed", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})
	s.applyPromptResult(jobID, item, res)
}

func isDisconnect(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset")
}
