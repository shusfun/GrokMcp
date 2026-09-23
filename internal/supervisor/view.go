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
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return protocol.Job{}, err
	}
	defer finish()
	rt := s.runtime(req.JobID)
	rt.coord.Lock()
	job, err := s.load(req.JobID)
	if err != nil {
		rt.coord.Unlock()
		return protocol.Job{}, err
	}
	switch req.View {
	case protocol.ViewHeaded:
		s.mu.Lock()
		rt.viewRequested = true
		rt.viewEpoch++
		epoch := rt.viewEpoch
		s.mu.Unlock()
		job.DesiredViewMode = protocol.ViewHeaded
		if job.InputOwner == protocol.OwnerSupervisor {
			job.InputOwner = protocol.OwnerHandoff
		}
		if job.ViewMode == protocol.ViewHeadless {
			job.ViewMode = protocol.ViewAttaching
		}
		s.touch(&job)
		s.saveViewLocked(job)
		rt.coord.Unlock()
		s.emitTrace(job, "info", trace.SourceSupervisor, "view.attach.requested", "headed requested", nil)
		return s.attach(ctx, job, epoch)
	case protocol.ViewHeadless:
		s.mu.Lock()
		rt.viewRequested = false
		rt.viewEpoch++
		epoch := rt.viewEpoch
		s.mu.Unlock()
		job.DesiredViewMode = protocol.ViewHeadless
		s.touch(&job)
		s.saveViewLocked(job)
		rt.coord.Unlock()
		s.emitTrace(job, "info", trace.SourceSupervisor, "view.detach.requested", "headless requested", nil)
		return s.detachExpected(ctx, job, false, epoch)
	default:
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("view must be headed or headless")
	}
}

func (s *Service) OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error {
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return err
	}
	defer finish()
	if req.Dashboard {
		return s.term.OpenDashboard(ctx, s.grokPath(), "")
	}
	rt := s.runtime(req.JobID)
	rt.coord.Lock()
	job, err := s.load(req.JobID)
	if err != nil {
		rt.coord.Unlock()
		return err
	}
	s.mu.Lock()
	rt.viewRequested = true
	rt.viewEpoch++
	epoch := rt.viewEpoch
	s.mu.Unlock()
	job.DesiredViewMode = protocol.ViewHeaded
	if job.InputOwner == protocol.OwnerSupervisor {
		job.InputOwner = protocol.OwnerHandoff
	}
	if job.ViewMode == protocol.ViewHeadless {
		job.ViewMode = protocol.ViewAttaching
	}
	s.saveViewLocked(job)
	rt.coord.Unlock()
	_, err = s.attach(ctx, job, epoch)
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

func (s *Service) attach(ctx context.Context, job protocol.Job, epoch uint64) (protocol.Job, error) {
	if job.GrokSessionID == "" {
		return s.failHeadless(job, epoch, errors.New("job has no grok session"))
	}
	return s.launchTUI(ctx, job, epoch)
}

func (s *Service) launchTUI(ctx context.Context, job protocol.Job, epoch uint64) (out protocol.Job, err error) {
	ctx = terminal.WithJob(ctx, job.JobID)
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return protocol.Job{}, err
	}
	defer finish()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.decorate(job), errors.New("supervisor closed")
	}
	s.mu.Unlock()
	rt := s.runtime(job.JobID)
	s.mu.Lock()
	if !rt.viewRequested || rt.viewEpoch != epoch {
		s.mu.Unlock()
		return s.snapshot(job.JobID), nil
	}
	if rt.attaching {
		done := rt.attachDone
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return s.snapshot(job.JobID), ctx.Err()
		case <-done:
		}
		return s.launchTUI(ctx, s.snapshot(job.JobID), epoch)
	}
	rt.attaching = true
	rt.attachDone = make(chan struct{})
	rt.attachErr = nil
	done := rt.attachDone
	existing := rt.term
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if rt.attachDone == done {
			rt.attaching = false
			rt.attachErr = err
			close(done)
		}
		s.mu.Unlock()
	}()
	if existing != nil && s.handleAlive(existing) {
		s.focusHandle(ctx, job.GrokSessionID, existing)
		s.emitTerm(job, "info", "terminal.focused", "existing TUI focused", existing, true, "ok", "")
		s.emitTerm(job, "info", "terminal.reused", "existing TUI reused", existing, true, "ok", "")
		if err := s.waitHeadedReady(ctx, existing); err != nil {
			s.emitTerm(job, "error", "terminal.focused", err.Error(), existing, true, "error", err.Error())
			return s.decorate(job), err
		}
		return s.markHeaded(job, epoch), nil
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
			return s.failHeadless(job, epoch, err)
		}
		s.emitTerm(job, "info", "terminal.reused", "adopted existing TUI", h, true, "ok", "")
		s.emitTerm(job, "info", "terminal.focused", "existing TUI focused", h, true, "ok", "")
		return s.commitHeaded(job, h, epoch)
	}
	if err := s.yieldInFlightTurn(job); err != nil {
		s.emitTerm(job, "error", "terminal.opened", err.Error(), nil, false, "error", err.Error())
		return s.failHeadless(job, epoch, err)
	}
	job.ViewMode = protocol.ViewAttaching
	job.InputOwner = protocol.OwnerHandoff
	s.touch(&job)
	if !s.saveRequestedView(job, epoch) {
		return s.snapshot(job.JobID), nil
	}
	s.emitTerm(job, "info", "terminal.opened", "opening TUI", nil, false, "", "")
	s.agent.InvalidateSession(job.GrokSessionID)
	h, err := s.term.OpenResume(ctx, s.grokPath(), job.GrokSessionID, job.Cwd)
	if err != nil {
		s.emitTerm(job, "error", "terminal.opened", err.Error(), nil, false, "error", err.Error())
		return s.failHeadless(job, epoch, err)
	}
	if generation, ok := s.term.(interface{ WorkerGeneration(string) string }); ok {
		s.mu.Lock()
		rt.workerGeneration = generation.WorkerGeneration(job.GrokSessionID)
		s.mu.Unlock()
	}
	if err := s.waitHeadedReady(ctx, h); err != nil {
		_ = h.Close()
		s.emitTerm(job, "error", "terminal.opened", err.Error(), h, false, "error", err.Error())
		s.emitTerm(job, "info", "terminal.closed", "verify failed", h, false, "error", err.Error())
		return s.failHeadless(job, epoch, err)
	}
	s.mu.Lock()
	requested := rt.viewRequested && rt.viewEpoch == epoch
	newerRequest := rt.viewRequested && rt.viewEpoch != epoch
	s.mu.Unlock()
	if !requested {
		if !newerRequest {
			_ = h.Close()
			s.reconcileClosedLaunch(job.JobID, job.GrokSessionID)
		}
		return s.snapshot(job.JobID), nil
	}
	s.emitTerm(job, "info", "terminal.opened", "TUI opened", h, false, "ok", "")
	return s.commitHeaded(job, h, epoch)
}

func approvalHoldsTurn(job protocol.Job) bool {
	return job.State == protocol.StatePlanReady || job.PauseReason == "approval" || job.PauseReason == "plan_content_missing"
}

// 打开 TUI 前让出仍在等待审批的 ACP turn。只取消这一次调用，不把 TUI 文本写成原请求结果。
func (s *Service) yieldInFlightTurn(job protocol.Job) error {
	rt := s.runtime(job.JobID)
	if !approvalHoldsTurn(job) {
		return nil
	}
	rt.coord.Lock()
	s.mu.Lock()
	busy := rt.busy
	cancelTurn := rt.cancel
	if busy {
		rt.cancelledTurn = true
		rt.gen++
		if cancelTurn != nil {
			cancelTurn()
		}
	}
	s.mu.Unlock()
	rt.coord.Unlock()
	if !busy {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	cancelErr := s.agent.Cancel(ctx, job.GrokSessionID)
	cancel()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		busy = rt.busy
		s.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if busy {
		return errors.New("交互交接未能等来 ACP turn 退出")
	}
	if err := s.markYieldedRequest(job.JobID); err != nil {
		return err
	}
	return cancelErr
}

func (s *Service) markYieldedRequest(jobID string) error {
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	current, err := s.load(jobID)
	if err != nil {
		return err
	}
	rec, err := s.record(jobID)
	if err != nil {
		return err
	}
	if current.State == protocol.StatePlanReady || current.ApprovalDelivery == "pending" {
		current.ApprovalDelivery = "expired"
	}
	switch current.State {
	case protocol.StatePlanReady, protocol.StateExecuting, protocol.StatePlanning, protocol.StateStarting:
		current.State = protocol.StateNeedsInput
	}
	current.PauseReason = "execution_unknown"
	current.ActiveTurnID = ""
	current.LastAction = "交互交接已让出未完成的 ACP turn"
	current.LastSummary = "原 ACP 请求结果未知，TUI 输入不会写成该请求的回答"
	s.touch(&current)
	rec.Job = current
	rec.Result = nil
	if rec.RequestPhase == "" || rec.RequestPhase == "queued" || rec.RequestPhase == "preparing" || rec.RequestPhase == "sent" {
		rec.RequestPhase = "unknown"
	}
	if err := s.store.PutJob(rec); err != nil {
		s.persistenceFailed(jobID, err)
		return err
	}
	s.emit(current)
	return nil
}

func (s *Service) reconcileClosedLaunch(jobID, sessionID string) {
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	s.mu.Lock()
	requested := rt.viewRequested
	s.mu.Unlock()
	if requested {
		return
	}
	job, err := s.load(jobID)
	if err != nil {
		return
	}
	job.ViewMode = protocol.ViewHeadless
	job.DesiredViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	if t, ok := s.term.(interface{ WorkerAlive(string) bool }); ok && t.WorkerAlive(sessionID) {
		job.InputOwner = protocol.OwnerTUI
	}
	s.saveViewLocked(job)
}

func (s *Service) failHeadless(job protocol.Job, epoch uint64, err error) (protocol.Job, error) {
	job.ViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	if t, ok := s.term.(interface{ WorkerAlive(string) bool }); ok && t.WorkerAlive(job.GrokSessionID) {
		job.InputOwner = protocol.OwnerTUI
		if generation, ok := s.term.(interface{ WorkerGeneration(string) string }); ok {
			rt := s.runtime(job.JobID)
			s.mu.Lock()
			rt.workerGeneration = generation.WorkerGeneration(job.GrokSessionID)
			s.mu.Unlock()
		}
	}
	job.LastSummary = err.Error()
	job.DesiredViewMode = protocol.ViewHeadless
	s.touch(&job)
	rt := s.runtime(job.JobID)
	rt.coord.Lock()
	s.mu.Lock()
	current := rt.viewRequested && rt.viewEpoch == epoch
	if current {
		rt.viewRequested = false
		rt.viewEpoch++
	}
	s.mu.Unlock()
	if current {
		s.saveViewLocked(job)
	}
	rt.coord.Unlock()
	return s.decorate(job), err
}

func (s *Service) commitHeaded(job protocol.Job, h terminal.Handle, epoch uint64) (protocol.Job, error) {
	rt := s.runtime(job.JobID)
	rt.coord.Lock()
	s.mu.Lock()
	if s.closed || !rt.viewRequested || rt.viewEpoch != epoch {
		s.mu.Unlock()
		rt.coord.Unlock()
		_ = h.Close()
		return s.snapshot(job.JobID), nil
	}
	prev := rt.term
	prevCancel := rt.waitCancel
	waitCtx, waitCancel := context.WithCancel(context.Background())
	rt.term = h
	rt.waitCancel = waitCancel
	rt.attachGen++
	gen := rt.attachGen
	s.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}
	if prev != nil && prev != h {
		_ = prev.Close()
	}
	job = s.markHeadedLocked(job)
	rt.coord.Unlock()
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
		_ = h.Close()
	case <-done:
	case <-s.stopWatch:
		_ = h.Close()
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
	timeout := 20 * time.Second
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
		workerReady := true
		if ready, ok := h.(interface{ WorkerReady() bool }); ok {
			workerReady = ready.WorkerReady()
		}
		if workerReady && pid > 0 && (!needWin || wid != "") && (!needTTY || tty != "") {
			if s.focusable(ctx, h) {
				return nil
			}
			last = "window not focusable"
		} else {
			last = "TUI worker or viewer not ready"
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

func (s *Service) markHeaded(job protocol.Job, epoch uint64) protocol.Job {
	rt := s.runtime(job.JobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	if !s.viewRequestCurrent(job.JobID, epoch) {
		return s.snapshot(job.JobID)
	}
	return s.markHeadedLocked(job)
}

func (s *Service) markHeadedLocked(job protocol.Job) protocol.Job {
	if t, ok := s.term.(interface{ WorkerGeneration(string) string }); ok {
		generation := t.WorkerGeneration(job.GrokSessionID)
		rt := s.runtime(job.JobID)
		s.mu.Lock()
		rt.workerGeneration = generation
		s.mu.Unlock()
	}
	job.ViewMode = protocol.ViewHeaded
	job.InputOwner = protocol.OwnerTUI
	s.touch(&job)
	s.saveViewLocked(job)
	s.emitTrace(job, "info", trace.SourceSupervisor, "view.attached", "TUI attached", nil)
	return s.snapshot(job.JobID)
}

func (s *Service) viewRequestCurrent(jobID string, epoch uint64) bool {
	rt := s.runtime(jobID)
	s.mu.Lock()
	defer s.mu.Unlock()
	return rt.viewRequested && rt.viewEpoch == epoch
}

func (s *Service) saveRequestedView(job protocol.Job, epoch uint64) bool {
	rt := s.runtime(job.JobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	if !s.viewRequestCurrent(job.JobID, epoch) {
		return false
	}
	s.saveViewLocked(job)
	return true
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
	return s.detachExpected(context.Background(), job, fromClose, 0)
}

func (s *Service) detachExpected(_ context.Context, job protocol.Job, fromClose bool, epoch uint64) (protocol.Job, error) {
	r := s.runtime(job.JobID)
	r.coord.Lock()
	s.mu.Lock()
	if epoch != 0 && r.viewEpoch != epoch {
		s.mu.Unlock()
		r.coord.Unlock()
		return s.snapshot(job.JobID), nil
	}
	r.viewRequested = false
	r.viewEpoch++
	s.mu.Unlock()
	job, err := s.load(job.JobID)
	if err != nil {
		r.coord.Unlock()
		return protocol.Job{}, err
	}
	if !fromClose && job.ViewMode == protocol.ViewHeadless && job.InputOwner == protocol.OwnerSupervisor {
		if job.DesiredViewMode != protocol.ViewHeadless {
			job.DesiredViewMode = protocol.ViewHeadless
			s.touch(&job)
			s.saveViewLocked(job)
		}
		r.coord.Unlock()
		return s.decorate(job), nil
	}
	job.DesiredViewMode = protocol.ViewHeadless
	job.ViewMode = protocol.ViewDetaching
	s.touch(&job)
	s.saveViewLocked(job)
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
	r.coord.Unlock()
	s.goWatch(func() { s.finishDetach(job.JobID, h, gen) })
	return s.snapshot(job.JobID), nil
}

func (s *Service) finishDetach(jobID string, h terminal.Handle, gen uint64) {
	if h != nil {
		_ = h.Close()
		s.emitTerm(protocol.Job{JobID: jobID}, "info", "terminal.closed", "close requested", h, false, "ok", "")
	}
	rt := s.runtime(jobID)
	rt.coord.Lock()
	s.mu.Lock()
	current := rt.attachGen == gen && !rt.viewRequested
	s.mu.Unlock()
	if !current {
		rt.coord.Unlock()
		s.emitTrace(protocol.Job{JobID: jobID}, "debug", trace.SourceSupervisor, "pump.skipped", "stale detach", map[string]any{"reason": "stale_generation"})
		return
	}
	job, err := s.load(jobID)
	if err != nil {
		rt.coord.Unlock()
		return
	}
	if t, ok := s.term.(interface{ WorkerAlive(string) bool }); ok && t.WorkerAlive(job.GrokSessionID) {
		job.ViewMode = protocol.ViewHeadless
		job.InputOwner = protocol.OwnerTUI
		job.LastAction = "查看窗口已关闭，交互会话继续在后台运行"
		s.touch(&job)
		s.saveViewLocked(job)
		rt.coord.Unlock()
		return
	}
	rt.coord.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	s.emitTrace(job, "info", trace.SourceACP, "session.load.started", "reclaim session", nil)
	if job.GrokSessionID != "" {
		if err := s.agent.LoadConnectedSession(ctx, job.GrokSessionID, job.Cwd); err != nil {
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
	rt.coord.Lock()
	s.mu.Lock()
	current = rt.attachGen == gen && !rt.viewRequested
	s.mu.Unlock()
	if !current {
		rt.coord.Unlock()
		return
	}
	job, err = s.load(jobID)
	if err != nil {
		rt.coord.Unlock()
		return
	}
	job.ViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	s.touch(&job)
	s.saveViewLocked(job)
	rt.coord.Unlock()
	s.emitTrace(job, "info", trace.SourceSupervisor, "view.detached", "input returned to supervisor", nil)
	s.kick(job.JobID)
}

// 后台 TUI 自身退出才是已知安全的输入归还边界；关查看窗口不触发任务取消。
func (s *Service) terminalWorkerExited(sessionID, generation string) {
	s.goWatch(func() {
		jobs, err := s.store.ListJobs()
		if err != nil {
			return
		}
		for _, rec := range jobs {
			if rec.Job.GrokSessionID != sessionID || (rec.Job.InputOwner != protocol.OwnerTUI && rec.Job.InputOwner != protocol.OwnerHandoff) {
				continue
			}
			rt := s.runtime(rec.Job.JobID)
			s.mu.Lock()
			current := rt.workerGeneration == generation
			s.mu.Unlock()
			if !current {
				continue
			}
			_, _ = s.detach(context.Background(), rec.Job, true)
		}
	})
}
