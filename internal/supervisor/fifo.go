package supervisor

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"grokmcp/internal/textutil"
	"os"
	"strings"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
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

func (s *Service) enqueue(jobID string, q queued) error {
	rt := s.runtime(jobID)
	if q.requestID == "" {
		if job, err := s.load(jobID); err == nil {
			q.requestID = job.RequestID
		}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("supervisor closed")
	}
	q.gen = rt.gen
	if q.requestID == "" {
		q.requestID = rt.requestID
	}
	if q.requestID == "" {
		q.requestID = uuid.NewString()
	}
	s.mu.Unlock()
	rec, err := s.record(jobID)
	if err != nil {
		return err
	}
	if q.acceptedJob != nil {
		rec.Job = *q.acceptedJob
	}
	rec.Accepted = &store.WorkRequest{RequestID: q.requestID, Prompt: q.text, Planning: q.planning}
	if err := s.store.PutJob(rec); err != nil {
		s.persistenceFailed(jobID, err)
		return err
	}
	s.mu.Lock()
	if q.kind == "continue" && !q.connect {
		rt.queue = append([]queued{q}, rt.queue...)
	} else {
		rt.queue = append(rt.queue, q)
	}
	n := len(rt.queue)
	busy := rt.busy
	s.mu.Unlock()
	job, _ := s.load(jobID)
	s.emit(job)
	s.emitTrace(job, "info", trace.SourceFIFO, "queue.enqueued", q.kind, map[string]any{"kind": q.kind, "queue_length": n})
	if !busy {
		s.pumps.Add(1)
		go func() {
			defer s.pumps.Done()
			s.pump(jobID)
		}()
	}
	return nil
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
	if j, err := s.load(jobID); err == nil {
		if t, ok := s.term.(interface{ WorkerAlive(string) bool }); ok {
			return t.WorkerAlive(j.GrokSessionID)
		}
	}
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
	if job.InputOwner != protocol.OwnerSupervisor || job.ViewMode == protocol.ViewDetaching {
		return false
	}
	rt := s.runtime(jobID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return !rt.busy && len(rt.queue) == 0
}

func (s *Service) pump(jobID string) {
	for {
		rt := s.runtime(jobID)
		rt.coord.Lock()
		job, err := s.load(jobID)
		priorPlanContent, _ := os.ReadFile(s.planPath(job.Cwd, job.GrokSessionID))
		priorPlanDigest := textutil.Digest(string(priorPlanContent))
		s.mu.Lock()
		closed := s.closed
		controlPending := rt.controlPending
		busy := rt.busy
		qlen := len(rt.queue)
		s.mu.Unlock()
		if closed || controlPending {
			rt.coord.Unlock()
			s.skipPump(job, "closed", qlen)
			return
		}
		if err == nil && (job.State == protocol.StatePlanReady || job.PauseReason == "review_required" || job.ApprovalDelivery == "unknown") && qlen > 0 {
			rt.coord.Unlock()
			s.skipPump(job, "approval", qlen)
			return
		}
		if err == nil && job.ViewMode == protocol.ViewDetaching && qlen > 0 {
			rt.coord.Unlock()
			s.skipPump(job, "detaching", qlen)
			return
		}
		if err == nil && s.sessionHeldByTUI(job) && qlen > 0 {
			reason := "tui_owns"
			if !s.liveGrok(jobID) {
				reason = "stale_tui_owner"
			}
			rt.coord.Unlock()
			s.skipPump(job, reason, qlen)
			return
		}
		s.mu.Lock()
		if rt.busy || len(rt.queue) == 0 {
			s.mu.Unlock()
			rt.coord.Unlock()
			if busy && qlen > 0 {
				s.skipPump(job, "already_busy", qlen)
			}
			return
		}
		item := rt.queue[0]
		rt.queue = rt.queue[1:]
		rt.busy = true
		rt.cancelledTurn = false
		rt.turnSeq++
		item.turnID = uuid.NewString()
		rt.turnID = item.turnID
		rt.requestID = item.requestID
		rt.sessionID = job.GrokSessionID
		rt.lastActivity = s.clock.Now()
		rt.activityKind = "prompt_started"
		rt.turnStartedAt = time.Now().UTC()
		rt.planFileDigest = priorPlanDigest
		ctx, cancel := context.WithCancel(context.Background())
		rt.cancel = cancel
		s.mu.Unlock()
		if item.requestID != job.RequestID {
			job.RequestID = item.requestID
			job.LastSummary = ""
			job.LastAction = "Accepted"
		}
		if item.planning {
			job.State = protocol.StatePlanning
			job.Approved = false
			job.PlanSummary = ""
			job.PlanDigest = ""
		} else if job.State != protocol.StatePlanReady {
			job.State = protocol.StateExecuting
		}
		job.PauseReason = ""
		job.ActiveTurnID = item.turnID
		if err := s.commitPhase(job, "preparing"); err != nil {
			cancel()
			s.mu.Lock()
			rt.busy = false
			rt.cancel = nil
			rt.turnID = ""
			s.mu.Unlock()
			rt.coord.Unlock()
			return
		}
		rt.coord.Unlock()
		s.emitTrace(job, "info", trace.SourceFIFO, "queue.dequeued", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})
		s.emitTrace(job, "info", trace.SourceFIFO, "pump.started", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})

		finishResult := s.runItem(ctx, jobID, item)
		cancel()

		rt.coord.Lock()
		s.mu.Lock()
		rt.busy = false
		rt.cancel = nil
		rt.turnID = ""
		more := len(rt.queue) > 0
		s.mu.Unlock()
		if finishResult != nil {
			finishResult()
		}
		rt.coord.Unlock()
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
	rt := s.runtime(jobID)
	s.mu.Lock()
	requested := rt.viewRequested
	epoch := rt.viewEpoch
	s.mu.Unlock()
	job, err := s.load(jobID)
	if !requested || err != nil || job.DesiredViewMode != protocol.ViewHeaded {
		return
	}
	if job.ViewMode != protocol.ViewAttaching {
		return
	}
	_, _ = s.launchTUI(context.Background(), job, epoch)
}

func (s *Service) runItem(ctx context.Context, jobID string, item queued) func() {
	job, err := s.load(jobID)
	if err != nil {
		return nil
	}
	if job.UserCancelled && item.kind != "cancel" {
		return nil
	}
	if job.ViewMode == protocol.ViewDetaching {
		s.skipPump(job, "detaching", 1)
		return func() { _ = s.enqueue(jobID, item) }
	}

	fields := trace.PromptFields(item.text, s.debugPayloads(jobID))
	fields["kind"] = item.kind
	s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.started", item.kind, fields)
	if item.kind != "plan" {
		load := s.agent.LoadConnectedSession
		if item.connect {
			load = s.agent.LoadSession
		}
		if err := load(ctx, job.GrokSessionID, job.Cwd); err != nil {
			return func() {
				if item.gen == s.currentGen(jobID) {
					s.disconnectLocked(jobID, err.Error())
				}
			}
		}
	}
	if item.planning {
		if err := s.agent.PlanSession(ctx, job.GrokSessionID); err != nil {
			return func() {
				if item.gen == s.currentGen(jobID) {
					if isDisconnect(err) {
						s.disconnectLocked(jobID, err.Error())
					} else {
						s.fail(job, err.Error())
					}
				}
			}
		}
	} else if err := s.agent.EnsureExecMode(ctx, job.GrokSessionID); err != nil {
		return func() {
			if item.gen == s.currentGen(jobID) {
				if isDisconnect(err) {
					s.disconnectLocked(jobID, err.Error())
				} else {
					s.fail(job, err.Error())
				}
			}
		}
	}
	rt := s.runtime(jobID)
	rt.coord.Lock()
	if item.gen != s.currentGen(jobID) || ctx.Err() != nil {
		rt.coord.Unlock()
		return nil
	}
	current, loadErr := s.load(jobID)
	if loadErr == nil {
		current.ActiveTurnID = item.turnID
		loadErr = s.commitPhase(current, "sent")
	}
	rt.coord.Unlock()
	if loadErr != nil {
		return func() { s.persistenceFailed(jobID, loadErr) }
	}
	res, err := s.agent.Prompt(agent.WithRequest(agent.WithTurn(ctx, item.turnID), item.requestID), job.GrokSessionID, item.text)
	if item.gen != s.currentGen(jobID) {
		s.emitTrace(job, "debug", trace.SourceFIFO, "pump.skipped", "stale generation", map[string]any{"reason": "stale_generation"})
		return nil
	}
	if err != nil {
		if ctx.Err() != nil {
			s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.cancelled", "prompt cancelled", map[string]any{"turn_id": item.turnID})
			return nil
		}
		s.emitTrace(job, "error", trace.SourceACP, "acp.prompt.completed", err.Error(), map[string]any{"error": err.Error()})
		return func() {
			if item.gen != s.currentGen(jobID) {
				return
			}
			if isDisconnect(err) {
				s.disconnectLocked(jobID, err.Error())
				return
			}
			if current, e := s.load(jobID); e == nil {
				s.fail(current, err.Error())
			}
		}
	}
	s.emitTrace(job, "info", trace.SourceACP, "acp.prompt.completed", item.kind, map[string]any{"kind": item.kind, "turn_id": item.turnID})
	return func() { s.applyPromptResult(jobID, item, res) }
}

func isDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, agent.ErrDisconnected) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset") || strings.Contains(msg, "peer disconnected") || strings.Contains(msg, "connection closed") || strings.Contains(msg, "transport closed")
}

func (s *Service) commitPhase(job protocol.Job, phase string) error {
	rec, err := s.record(job.JobID)
	if err != nil {
		return err
	}
	rec.Job = job
	rec.RequestPhase = phase
	if err := s.store.PutJob(rec); err != nil {
		s.persistenceFailed(job.JobID, err)
		return err
	}
	s.emit(job)
	return nil
}
