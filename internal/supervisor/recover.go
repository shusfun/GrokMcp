package supervisor

import (
	"context"
	"time"

	"grokmcp/internal/protocol"
)

func (s *Service) recoverJob(ctx context.Context, jobID string) {
	rec, err := s.record(jobID)
	if err != nil {
		return
	}
	job := rec.Job
	if job.UserCancelled {
		return
	}
	switch job.State {
	case protocol.StatePlanReady:
		if err := s.agent.EnsureLeader(ctx); err == nil && job.GrokSessionID != "" {
			_ = s.agent.LoadSession(ctx, job.GrokSessionID, job.Cwd)
		}
		s.emit(job)
		return
	case protocol.StateNeedsInput, protocol.StateBlocked, protocol.StateCompleted, protocol.StateCancelled, protocol.StateFailed:
		return
	}

	job.State = protocol.StateDisconnected
	job.LastAction = "Disconnected"
	s.touch(&job)
	_ = s.store.AddEvent(job.JobID, string(protocol.StateDisconnected), "leader disconnected", s.clock.Now())
	s.save(job)

	backoffs := []time.Duration{200 * time.Millisecond, time.Second, 2 * time.Second}
	var last error
	for i, wait := range backoffs {
		job.State = protocol.StateRecovering
		s.touch(&job)
		s.save(job)
		if err := s.agent.EnsureLeader(ctx); err != nil {
			last = err
		} else if err := s.agent.LoadSession(ctx, job.GrokSessionID, job.Cwd); err != nil {
			last = err
		} else {
			job, _ = s.load(jobID)
			if job.UserCancelled {
				return
			}
			job.State = protocol.StateExecuting
			job.LastAction = "Recovered"
			s.touch(&job)
			s.save(job)
			s.enqueue(jobID, queued{kind: "continue", text: protocol.ContinuePrompt()})
			return
		}
		_ = last
		if i == len(backoffs)-1 {
			break
		}
		select {
		case <-ctx.Done():
			s.noteRecoverFail(jobID)
			return
		case <-time.After(wait):
		}
	}
	s.noteRecoverFail(jobID)
}

func (s *Service) noteRecoverFail(jobID string) {
	rec, err := s.record(jobID)
	if err != nil {
		return
	}
	now := s.clock.Now().Unix()
	fails := append(rec.RecoverFails, now)
	window := now - 10*60
	kept := fails[:0]
	for _, ts := range fails {
		if ts >= window {
			kept = append(kept, ts)
		}
	}
	job := rec.Job
	if len(kept) >= 2 {
		job.State = protocol.StateNeedsInput
		job.LastSummary = "recovery failed twice in 10 minutes"
		job.LastAction = "Needs input"
		s.touch(&job)
		_ = s.put(job, rec.MissingMarkerCount, kept)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateNeedsInput), job.LastSummary, time.Unix(now, 0).UTC())
		s.emit(job)
		return
	}
	job.State = protocol.StateDisconnected
	s.touch(&job)
	_ = s.put(job, rec.MissingMarkerCount, kept)
	s.emit(job)
}

func (s *Service) Disconnect(jobID string) {
	s.goWatch(func() { s.recoverJob(context.Background(), jobID) })
}
