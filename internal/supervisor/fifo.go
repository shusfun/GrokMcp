package supervisor

import (
	"context"
	"strings"

	"grokmcp/internal/protocol"
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
	busy := rt.busy
	s.mu.Unlock()
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

func tuiOwns(job protocol.Job) bool {
	return job.ViewMode == protocol.ViewHeaded || job.ViewMode == protocol.ViewAttaching || job.InputOwner == protocol.OwnerTUI
}

func (s *Service) pump(jobID string) {
	for {
		job, err := s.load(jobID)
		if err == nil && tuiOwns(job) {
			return
		}
		rt := s.runtime(jobID)
		s.mu.Lock()
		if rt.busy || len(rt.queue) == 0 {
			s.mu.Unlock()
			return
		}
		item := rt.queue[0]
		rt.queue = rt.queue[1:]
		rt.busy = true
		ctx, cancel := context.WithCancel(context.Background())
		rt.cancel = cancel
		s.mu.Unlock()

		s.runItem(ctx, jobID, item)

		s.mu.Lock()
		rt.busy = false
		rt.cancel = nil
		more := len(rt.queue) > 0
		s.mu.Unlock()
		if !more {
			return
		}
	}
}

func (s *Service) runItem(ctx context.Context, jobID string, item queued) {
	job, err := s.load(jobID)
	if err != nil {
		return
	}
	if job.UserCancelled && item.kind != "cancel" {
		return
	}
	if item.kind == "approve" {
		switch job.State {
		case protocol.StateCompleted, protocol.StateCancelled, protocol.StateFailed:
			return
		}
	}
	res, err := s.agent.Prompt(ctx, job.GrokSessionID, item.text)
	if item.gen != s.currentGen(jobID) {
		return
	}
	if err != nil {
		if ctx.Err() != nil {
			return
		}
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
	s.applyPromptResult(jobID, item, res)
}

func isDisconnect(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "eof") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset")
}
