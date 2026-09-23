package supervisor

import (
	"context"
	"errors"
	"grokmcp/internal/protocol"
	"time"
)

func (s *Service) CancelTurn(ctx context.Context, id string, expected ...string) (protocol.Job, error) {
	turnID := ""
	if len(expected) > 0 {
		turnID = expected[0]
	}
	return s.CancelRequest(ctx, id, "", turnID)
}

func (s *Service) CancelRequest(ctx context.Context, id, requestID, turnID string) (protocol.Job, error) {
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
	activeTurn := rt.turnID
	activeRequest := rt.requestID
	busy := rt.busy
	queued := false
	for _, item := range rt.queue {
		if item.requestID == requestID && requestID != "" {
			queued = true
			break
		}
	}
	s.mu.Unlock()
	if requestID != "" && !queued {
		if persisted, e := s.store.Request(id, requestID); e == nil && persisted.Phase == "queued" {
			queued = true
		}
	}
	if requestID != "" && turnID != "" {
		sameActive := busy && requestID == activeRequest && turnID == activeTurn
		if !sameActive {
			rt.coord.Unlock()
			return protocol.Job{}, errors.New("cancel target does not match one request")
		}
	}
	if requestID != "" && turnID == "" && queued && !(busy && requestID == activeRequest) {
		if err := s.cancelQueuedLocked(id, requestID); err != nil {
			rt.coord.Unlock()
			return protocol.Job{}, err
		}
		rt.coord.Unlock()
		return s.snapshot(id), nil
	}
	if requestID != "" && (!busy || requestID != activeRequest) {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("stale or unknown request cancellation")
	}
	rt.coord.Unlock()
	return s.cancelActiveTurn(ctx, id, turnID)
}

func (s *Service) cancelQueuedLocked(id, requestID string) error {
	ok, err := s.store.CancelQueuedRequest(id, requestID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("stale or unknown request cancellation")
	}
	rt := s.runtime(id)
	s.mu.Lock()
	next := rt.queue[:0]
	for _, item := range rt.queue {
		if item.requestID != requestID {
			next = append(next, item)
		}
	}
	rt.queue = next
	s.mu.Unlock()
	if job, err := s.load(id); err == nil {
		s.emit(job)
	}
	return nil
}

func (s *Service) cancelActiveTurn(ctx context.Context, id, expectedTurn string) (protocol.Job, error) {
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
	if rt.controlPending || (expectedTurn != "" && expectedTurn != rt.turnID) {
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
