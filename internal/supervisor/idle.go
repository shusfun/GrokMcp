package supervisor

import (
	"context"
	"errors"
	"time"

	"grokmcp/internal/protocol"
)

func (s *Service) beginOperation(ctx context.Context) (func(), error) {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, errors.New("supervisor closed")
		}
		if done := s.idleDone; done != nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				continue
			}
		}
		s.activeOperations++
		s.operations.Add(1)
		s.idleSince = time.Time{}
		s.mu.Unlock()
		return func() { s.mu.Lock(); s.activeOperations--; s.mu.Unlock(); s.operations.Done() }, nil
	}
}

func (s *Service) releaseIfIdle(now time.Time) {
	s.mu.Lock()
	if s.closed || s.activeOperations > 0 || s.idleDone != nil {
		s.idleSince = time.Time{}
		s.mu.Unlock()
		return
	}
	for _, rt := range s.rt {
		if rt.busy || len(rt.queue) > 0 || rt.term != nil || rt.attaching {
			s.idleSince = time.Time{}
			s.mu.Unlock()
			return
		}
	}
	if t, ok := s.term.(interface{ Active() bool }); ok && t.Active() {
		s.idleSince = time.Time{}
		s.mu.Unlock()
		return
	}
	jobs, err := s.store.ListJobs()
	if err != nil {
		s.mu.Unlock()
		return
	}
	for _, r := range jobs {
		if r.Job.State == protocol.StatePlanReady && s.agent.SessionLoaded(r.Job.GrokSessionID) {
			s.idleSince = time.Time{}
			s.mu.Unlock()
			return
		}
	}
	if s.idleSince.IsZero() {
		s.idleSince = now
		s.mu.Unlock()
		return
	}
	timeout := s.idleTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if now.Sub(s.idleSince) < timeout {
		s.mu.Unlock()
		return
	}
	done := make(chan struct{})
	s.idleDone = done
	s.mu.Unlock()
	_ = s.agent.Release()
	s.mu.Lock()
	s.idleDone = nil
	s.idleSince = now
	close(done)
	s.mu.Unlock()
	s.emitEvent(protocol.Event{Type: "connection"})
}
