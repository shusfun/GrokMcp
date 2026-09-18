package supervisor

import (
	"context"
	"errors"
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func (s *Service) SetView(ctx context.Context, req protocol.SetViewRequest) (protocol.Job, error) {
	job, err := s.load(req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	switch req.View {
	case protocol.ViewHeaded:
		return s.attach(ctx, job)
	case protocol.ViewHeadless:
		return s.detach(ctx, job, false)
	default:
		return protocol.Job{}, errors.New("view must be headed or headless")
	}
}

func (s *Service) OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error {
	if req.Dashboard {
		return s.term.OpenDashboard(ctx, s.grokPath(), "")
	}
	job, err := s.load(req.JobID)
	if err != nil {
		return err
	}
	_, err = s.attach(ctx, job)
	return err
}

func (s *Service) OpenProject(ctx context.Context, jobID string) error {
	job, err := s.load(jobID)
	if err != nil {
		return err
	}
	return s.term.OpenDirectory(ctx, job.Cwd)
}

func (s *Service) DetachView(ctx context.Context, jobID string) (protocol.Job, error) {
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	return s.detach(ctx, job, false)
}

func (s *Service) attach(ctx context.Context, job protocol.Job) (protocol.Job, error) {
	if job.GrokSessionID == "" {
		return protocol.Job{}, errors.New("job has no grok session")
	}
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	busy := rt.busy
	s.mu.Unlock()
	if busy {
		job.ViewMode = protocol.ViewAttaching
		s.touch(&job)
		s.save(job)
		s.mu.Lock()
		rt.attachGen++
		gen := rt.attachGen
		s.mu.Unlock()
		s.goWatch(func() { s.attachWhenIdle(job.JobID, gen) })
		return s.snapshot(job.JobID), nil
	}
	return s.launchTUI(ctx, job)
}

func (s *Service) attachWhenIdle(jobID string, gen uint64) {
	for {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		rt := s.runtime(jobID)
		s.mu.Lock()
		cur := rt.attachGen
		busy := rt.busy
		s.mu.Unlock()
		if cur != gen {
			return
		}
		if !busy {
			job, err := s.load(jobID)
			if err != nil || job.ViewMode != protocol.ViewAttaching {
				return
			}
			_, _ = s.launchTUI(context.Background(), job)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *Service) launchTUI(ctx context.Context, job protocol.Job) (protocol.Job, error) {
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	existing := rt.term
	s.mu.Unlock()
	if existing != nil {
		s.focusHandle(ctx, job.GrokSessionID, existing)
		return s.markHeaded(job), nil
	}
	if focused, err := s.term.FocusResume(ctx, job.GrokSessionID); err == nil && focused {
		return s.markHeaded(job), nil
	}
	job.ViewMode = protocol.ViewAttaching
	s.touch(&job)
	s.save(job)
	h, err := s.term.OpenResume(ctx, s.grokPath(), job.GrokSessionID, job.Cwd)
	if err != nil {
		job.ViewMode = protocol.ViewHeadless
		job.LastSummary = err.Error()
		s.touch(&job)
		s.save(job)
		return s.decorate(job), err
	}
	s.mu.Lock()
	rt.term = h
	s.mu.Unlock()
	job.ViewMode = protocol.ViewHeaded
	job.InputOwner = protocol.OwnerTUI
	s.touch(&job)
	s.save(job)
	s.goWatch(func() {
		_ = h.Wait()
		s.mu.Lock()
		if rt := s.rt[job.JobID]; rt != nil && rt.term == h {
			rt.term = nil
		}
		s.mu.Unlock()
		loaded, err := s.load(job.JobID)
		if err == nil {
			_, _ = s.detach(context.Background(), loaded, true)
		}
	})
	return s.snapshot(job.JobID), nil
}

func (s *Service) markHeaded(job protocol.Job) protocol.Job {
	job.ViewMode = protocol.ViewHeaded
	job.InputOwner = protocol.OwnerTUI
	s.touch(&job)
	s.save(job)
	return s.snapshot(job.JobID)
}

func (s *Service) focusHandle(ctx context.Context, sessionID string, h terminal.Handle) {
	type focuser interface {
		Focus() (bool, error)
	}
	if f, ok := h.(focuser); ok {
		if ok, err := f.Focus(); err == nil && ok {
			return
		}
	}
	_, _ = s.term.FocusResume(ctx, sessionID)
}

func (s *Service) detach(ctx context.Context, job protocol.Job, fromClose bool) (protocol.Job, error) {
	job.ViewMode = protocol.ViewDetaching
	s.touch(&job)
	s.save(job)
	if !fromClose {
		rt := s.runtime(job.JobID)
		s.mu.Lock()
		h := rt.term
		rt.term = nil
		rt.attachGen++
		s.mu.Unlock()
		if h != nil {
			_ = h.Close()
		}
		if job.GrokSessionID != "" {
			_ = s.agent.LoadSession(ctx, job.GrokSessionID, job.Cwd)
		}
	}
	job.ViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	s.touch(&job)
	s.save(job)
	s.kick(job.JobID)
	return s.decorate(job), nil
}
