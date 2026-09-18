package supervisor

import (
	"context"
	"errors"
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
	"grokmcp/internal/trace"
)

func (s *Service) SetView(ctx context.Context, req protocol.SetViewRequest) (protocol.Job, error) {
	job, err := s.load(req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	switch req.View {
	case protocol.ViewHeaded:
		job.DesiredViewMode = protocol.ViewHeaded
		s.touch(&job)
		s.save(job)
		s.emitTrace(job, "info", trace.SourceSupervisor, "view.attach.requested", "headed requested", nil)
		return s.attach(ctx, job)
	case protocol.ViewHeadless:
		job.DesiredViewMode = protocol.ViewHeadless
		s.touch(&job)
		s.save(job)
		s.emitTrace(job, "info", trace.SourceSupervisor, "view.detach.requested", "headless requested", nil)
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
	if !s.isIdle(job.JobID) {
		job.ViewMode = protocol.ViewAttaching
		s.touch(&job)
		s.save(job)
		rt := s.runtime(job.JobID)
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
		s.mu.Unlock()
		if cur != gen {
			return
		}
		if s.isIdle(jobID) {
			job, err := s.load(jobID)
			if err != nil || job.DesiredViewMode != protocol.ViewHeaded {
				return
			}
			if job.ViewMode != protocol.ViewAttaching && job.ViewMode != protocol.ViewHeadless {
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
	s.emitTrace(job, "info", trace.SourceTerminal, "terminal.opened", "opening TUI", nil)
	h, err := s.term.OpenResume(ctx, s.grokPath(), job.GrokSessionID, job.Cwd)
	if err != nil {
		job.ViewMode = protocol.ViewHeadless
		job.InputOwner = protocol.OwnerSupervisor
		job.LastSummary = err.Error()
		s.touch(&job)
		s.save(job)
		s.emitTrace(job, "error", trace.SourceTerminal, "terminal.opened", err.Error(), map[string]any{"error": err.Error()})
		return s.decorate(job), err
	}
	s.mu.Lock()
	rt.term = h
	rt.attachGen++
	gen := rt.attachGen
	s.mu.Unlock()
	if pid := h.PID(); pid > 0 {
		s.emitTrace(job, "info", trace.SourceTerminal, "terminal.process_found", "grok pid", map[string]any{"pid": pid, "window_id": h.WindowID()})
	}
	job = s.markHeaded(job)
	s.goWatch(func() {
		_ = h.Wait()
		s.mu.Lock()
		rt := s.rt[job.JobID]
		if rt == nil || rt.term != h || rt.attachGen != gen {
			s.mu.Unlock()
			s.emitTrace(job, "debug", trace.SourceTerminal, "pump.skipped", "stale TUI watcher", map[string]any{"reason": "stale_generation"})
			return
		}
		rt.term = nil
		s.mu.Unlock()
		s.emitTrace(job, "info", trace.SourceTerminal, "terminal.process_exited", "grok process exited", map[string]any{"window_id": h.WindowID()})
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
	s.emitTrace(job, "info", trace.SourceSupervisor, "view.attached", "TUI attached", nil)
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

func (s *Service) detach(_ context.Context, job protocol.Job, fromClose bool) (protocol.Job, error) {
	if !fromClose && job.ViewMode == protocol.ViewHeadless && job.InputOwner == protocol.OwnerSupervisor {
		if job.DesiredViewMode != protocol.ViewHeadless {
			job.DesiredViewMode = protocol.ViewHeadless
			s.touch(&job)
			s.save(job)
		}
		return s.decorate(job), nil
	}
	job.DesiredViewMode = protocol.ViewHeadless
	job.ViewMode = protocol.ViewDetaching
	s.touch(&job)
	s.save(job)
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	h := rt.term
	if !fromClose {
		rt.attachGen++
		rt.term = nil
	}
	gen := rt.attachGen
	s.mu.Unlock()
	s.goWatch(func() { s.finishDetach(job.JobID, h, gen) })
	return s.snapshot(job.JobID), nil
}

func (s *Service) finishDetach(jobID string, h terminal.Handle, gen uint64) {
	if h != nil {
		_ = h.Close()
		s.emitTrace(protocol.Job{JobID: jobID}, "info", trace.SourceTerminal, "terminal.window_closed", "close requested", map[string]any{"window_id": h.WindowID(), "pid": h.PID()})
	}
	s.mu.Lock()
	rt := s.rt[jobID]
	if rt == nil || rt.attachGen != gen {
		s.mu.Unlock()
		s.emitTrace(protocol.Job{JobID: jobID}, "debug", trace.SourceSupervisor, "pump.skipped", "stale detach", map[string]any{"reason": "stale_generation"})
		return
	}
	s.mu.Unlock()
	job, err := s.load(jobID)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s.emitTrace(job, "info", trace.SourceACP, "session.load.started", "reclaim session", nil)
	if job.GrokSessionID != "" {
		if err := s.agent.LoadSession(ctx, job.GrokSessionID, job.Cwd); err != nil {
			s.emitTrace(job, "error", trace.SourceACP, "session.load.failed", err.Error(), map[string]any{"error": err.Error()})
			s.mu.Lock()
			rt := s.rt[jobID]
			if rt == nil {
				rt = &runtime{}
				s.rt[jobID] = rt
			}
			rt.stalled = true
			rt.stalledReason = "session_load_failed"
			s.mu.Unlock()
		} else {
			s.emitTrace(job, "info", trace.SourceACP, "session.load.completed", "session reclaimed", nil)
			s.mu.Lock()
			if rt := s.rt[jobID]; rt != nil && rt.stalledReason == "session_load_failed" {
				rt.stalled = false
				rt.stalledReason = ""
			}
			s.mu.Unlock()
		}
	}
	job, err = s.load(jobID)
	if err != nil {
		return
	}
	s.mu.Lock()
	if rt := s.rt[jobID]; rt == nil || rt.attachGen != gen {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	job.ViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	s.touch(&job)
	s.save(job)
	s.emitTrace(job, "info", trace.SourceSupervisor, "view.detached", "input returned to supervisor", nil)
	s.kick(job.JobID)
}
