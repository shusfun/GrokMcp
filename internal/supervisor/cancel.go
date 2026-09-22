package supervisor

import (
	"context"
	"errors"
	"grokmcp/internal/protocol"
	"time"
)

func (s *Service) CancelTurn(ctx context.Context, id string, expected ...string) (protocol.Job, error) {
	rt := s.runtime(id)
	rt.coord.Lock()
	job, err := s.load(id)
	if err != nil {
		rt.coord.Unlock()
		return protocol.Job{}, err
	}
	if !acceptsTurnControl(job) {
		rt.coord.Unlock()
		return protocol.Job{}, errJobInactive
	}
	s.mu.Lock()
	if rt.controlPending || (len(expected) > 0 && expected[0] != "" && expected[0] != rt.turnID) {
		s.mu.Unlock()
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("stale or pending turn cancellation")
	}
	rt.gen++
	generation := rt.gen
	rt.cancelledTurn = true
	rt.controlPending = true
	for i := range rt.queue {
		rt.queue[i].gen = rt.gen
	}
	if rt.cancel != nil {
		rt.cancel()
	}
	s.mu.Unlock()
	job.State = protocol.StateNeedsInput
	job.PauseReason = "cancel_delivery"
	job.LastAction = "正在取消当前 turn"
	s.touch(&job)
	err = s.save(job)
	rt.coord.Unlock()
	if err == nil && job.GrokSessionID != "" {
		cancelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = s.agent.Cancel(cancelCtx, job.GrokSessionID)
		cancel()
	}
	rt.coord.Lock()
	s.mu.Lock()
	rt.controlPending = false
	currentGeneration := rt.gen
	s.mu.Unlock()
	if currentGeneration == generation {
		if j, e := s.load(id); e == nil {
			j.PauseReason = ""
			j.LastAction = "Turn cancelled"
			j.LastSummary = "turn cancelled"
			if err != nil {
				j.PauseReason = "cancel_delivery_unknown"
				j.LastAction = "取消交付未确认"
			}
			s.touch(&j)
			if e := s.save(j); e != nil {
				err = e
			}
		}
	}
	rt.coord.Unlock()
	if err == nil {
		s.kick(id)
	}
	return s.snapshot(id), err
}
