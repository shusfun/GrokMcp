package supervisor

import (
	"context"
	"errors"
	goruntime "runtime"
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
	if job.ViewMode == protocol.ViewHeaded || job.InputOwner == protocol.OwnerTUI {
		return s.launchTUI(ctx, job)
	}
	if !s.attachAllowed(job) {
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
		job, err := s.load(jobID)
		if err != nil || job.DesiredViewMode != protocol.ViewHeaded {
			return
		}
		if job.ViewMode != protocol.ViewAttaching && job.ViewMode != protocol.ViewHeadless {
			return
		}
		if s.attachAllowed(job) {
			_, _ = s.launchTUI(context.Background(), job)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *Service) launchTUI(ctx context.Context, job protocol.Job) (protocol.Job, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.decorate(job), errors.New("supervisor closed")
	}
	s.mu.Unlock()
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	existing := rt.term
	s.mu.Unlock()
	if existing != nil && s.handleAlive(existing) {
		s.focusHandle(ctx, job.GrokSessionID, existing)
		s.emitTerm(job, "info", "terminal.focused", "existing TUI focused", existing, true, "ok", "")
		s.emitTerm(job, "info", "terminal.reused", "existing TUI reused", existing, true, "ok", "")
		if err := s.waitHeadedReady(ctx, existing); err != nil {
			s.emitTerm(job, "error", "terminal.focused", err.Error(), existing, true, "error", err.Error())
			return s.decorate(job), err
		}
		return s.markHeaded(job), nil
	}
	if existing != nil {
		s.mu.Lock()
		if rt.term == existing {
			rt.term = nil
		}
		s.mu.Unlock()
	}
	if h, ok, err := s.term.ExistingResume(ctx, job.GrokSessionID); err == nil && ok && h != nil {
		s.focusHandle(ctx, job.GrokSessionID, h)
		if err := s.waitHeadedReady(ctx, h); err != nil {
			_ = h.Close()
			s.emitTerm(job, "error", "terminal.reused", err.Error(), h, true, "error", err.Error())
			return s.failHeadless(job, err)
		}
		s.emitTerm(job, "info", "terminal.reused", "adopted existing TUI", h, true, "ok", "")
		s.emitTerm(job, "info", "terminal.focused", "existing TUI focused", h, true, "ok", "")
		return s.commitHeaded(job, h)
	}
	job.ViewMode = protocol.ViewAttaching
	s.touch(&job)
	s.save(job)
	s.emitTerm(job, "info", "terminal.opened", "opening TUI", nil, false, "", "")
	h, err := s.term.OpenResume(ctx, s.grokPath(), job.GrokSessionID, job.Cwd)
	if err != nil {
		s.emitTerm(job, "error", "terminal.opened", err.Error(), nil, false, "error", err.Error())
		return s.failHeadless(job, err)
	}
	if err := s.waitHeadedReady(ctx, h); err != nil {
		_ = h.Close()
		s.emitTerm(job, "error", "terminal.opened", err.Error(), h, false, "error", err.Error())
		s.emitTerm(job, "info", "terminal.closed", "verify failed", h, false, "error", err.Error())
		return s.failHeadless(job, err)
	}
	s.emitTerm(job, "info", "terminal.opened", "TUI opened", h, false, "ok", "")
	return s.commitHeaded(job, h)
}

func (s *Service) failHeadless(job protocol.Job, err error) (protocol.Job, error) {
	job.ViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	job.LastSummary = err.Error()
	s.touch(&job)
	s.save(job)
	return s.decorate(job), err
}

func (s *Service) commitHeaded(job protocol.Job, h terminal.Handle) (protocol.Job, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = h.Close()
		return s.decorate(job), errors.New("supervisor closed")
	}
	rt := s.rt[job.JobID]
	if rt == nil {
		rt = &runtime{stallSince: map[string]time.Time{}}
		s.rt[job.JobID] = rt
	}
	waitCtx, waitCancel := context.WithCancel(context.Background())
	rt.term = h
	rt.waitCancel = waitCancel
	rt.attachGen++
	gen := rt.attachGen
	s.mu.Unlock()
	job = s.markHeaded(job)
	s.goWatch(func() { s.watchTUI(job, h, waitCtx, gen) })
	return s.snapshot(job.JobID), nil
}

func (s *Service) watchTUI(job protocol.Job, h terminal.Handle, waitCtx context.Context, gen uint64) {
	done := make(chan struct{})
	go func() {
		_ = h.Wait()
		close(done)
	}()
	select {
	case <-waitCtx.Done():
	case <-done:
	}
	s.mu.Lock()
	rt := s.rt[job.JobID]
	if rt == nil || rt.term != h || rt.attachGen != gen {
		s.mu.Unlock()
		s.emitTrace(job, "debug", trace.SourceTerminal, "pump.skipped", "stale TUI watcher", map[string]any{"reason": "stale_generation"})
		return
	}
	rt.term = nil
	if rt.waitCancel != nil {
		rt.waitCancel = nil
	}
	s.mu.Unlock()
	s.emitTerm(job, "info", "terminal.closed", "grok process exited", h, false, "ok", "")
	loaded, err := s.load(job.JobID)
	if err == nil {
		_, _ = s.detach(context.Background(), loaded, true)
	}
}

func (s *Service) handleAlive(h terminal.Handle) bool {
	if h == nil {
		return false
	}
	return h.PID() > 0 || h.WindowID() != ""
}

func (s *Service) waitHeadedReady(ctx context.Context, h terminal.Handle) error {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := 5 * time.Second
	if _, ok := s.term.(*terminal.Fake); ok {
		timeout = 300 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	needWin := goruntime.GOOS == "darwin"
	needTTY := goruntime.GOOS == "darwin"
	var last string
	for time.Now().Before(deadline) {
		pid := h.PID()
		wid := h.WindowID()
		tty := h.TTY()
		if pid > 0 && (!needWin || wid != "") && (!needTTY || tty != "") {
			if s.focusable(ctx, h) {
				return nil
			}
			last = "window not focusable"
		} else {
			last = "missing pid, window, or tty"
		}
		time.Sleep(50 * time.Millisecond)
	}
	if last == "" {
		last = "TUI not ready"
	}
	return errors.New(last)
}

func (s *Service) focusable(_ context.Context, h terminal.Handle) bool {
	type focuser interface {
		Focus() (bool, error)
	}
	if f, ok := h.(focuser); ok {
		ok, err := f.Focus()
		return err == nil && ok
	}
	return h.PID() > 0 || h.WindowID() != ""
}

func (s *Service) emitTerm(job protocol.Job, level, name, message string, h terminal.Handle, reused bool, result, errStr string) {
	fields := map[string]any{
		"provider": s.termProvider(),
		"reused":   reused,
	}
	if result != "" {
		fields["result"] = result
	}
	if errStr != "" {
		fields["error"] = errStr
	}
	if h != nil {
		fields["pid"] = h.PID()
		fields["window_id"] = h.WindowID()
		fields["tty"] = h.TTY()
	}
	s.emitTrace(job, level, trace.SourceTerminal, name, message, fields)
}

func (s *Service) termProvider() string {
	if e, ok := s.term.(terminal.Exec); ok && e.Provider != "" {
		return e.Provider
	}
	return "default"
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
	if rt.waitCancel != nil {
		rt.waitCancel()
		rt.waitCancel = nil
	}
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
		s.emitTerm(protocol.Job{JobID: jobID}, "info", "terminal.closed", "close requested", h, false, "ok", "")
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
