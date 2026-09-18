package supervisor

import (
	"context"

	"grokmcp/internal/protocol"
)

func (s *Service) CancelTurn(ctx context.Context, jobID string) (protocol.Job, error) {
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	rt := s.runtime(jobID)
	s.mu.Lock()
	if rt.cancel != nil {
		rt.cancel()
	}
	s.mu.Unlock()
	if job.GrokSessionID != "" {
		_ = s.agent.Cancel(ctx, job.GrokSessionID)
	}
	job.State = protocol.StateNeedsInput
	job.LastAction = "Turn cancelled"
	job.LastSummary = "turn cancelled"
	s.touch(&job)
	_ = s.store.AddEvent(job.JobID, string(protocol.StateNeedsInput), job.LastSummary, s.clock.Now())
	s.save(job)
	return s.snapshot(jobID), nil
}
