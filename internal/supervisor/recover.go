package supervisor

import "grokmcp/internal/protocol"

func (s *Service) connectionLost() {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return
	}
	jobs, err := s.store.ListJobs()
	if err != nil {
		return
	}
	if t, ok := s.term.(interface{ ReleaseWorkers() }); ok {
		t.ReleaseWorkers()
	}
	for _, rec := range jobs {
		if rec.Job.State.IsActive() || rec.Job.State == protocol.StatePlanReady || rec.Job.InputOwner == protocol.OwnerTUI || rec.Job.InputOwner == protocol.OwnerHandoff {
			s.Disconnect(rec.Job.JobID)
		}
	}
	s.emitEvent(protocol.Event{Type: "connection"})
}

// 重启只恢复记录，原会话在用户操作时加载。
func (s *Service) markAwaitingResume(jobID string) error {
	rec, err := s.record(jobID)
	if err != nil {
		return err
	}
	job := rec.Job
	if job.UserCancelled {
		return nil
	}
	if job.State == protocol.StatePlanReady {
		job.PauseReason = "approval_expired"
		job.ApprovalDelivery = "expired"
	}
	if job.ApprovalDelivery == "submitted" {
		job.ApprovalDelivery = "unknown"
		job.PauseReason = "approval_delivery_unknown"
		job.Approved = false
	}
	active := job.State.IsActive() || job.State == protocol.StateDisconnected
	if !active && job.State != protocol.StatePlanReady && job.ViewMode == protocol.ViewHeadless && job.DesiredViewMode == protocol.ViewHeadless && job.InputOwner == protocol.OwnerSupervisor {
		return nil
	}
	if active {
		job.State = protocol.StateDisconnected
		job.LastAction = "等待手动恢复"
		if job.PauseReason == "" {
			switch job.RequestPhase {
			case "queued", "preparing":
				job.PauseReason = "request_not_sent"
			default:
				job.PauseReason = "execution_unknown"
			}
		}
	}
	job.ViewMode = protocol.ViewHeadless
	job.DesiredViewMode = protocol.ViewHeadless
	job.InputOwner = protocol.OwnerSupervisor
	s.touch(&job)
	rec.Job = job
	if err := s.store.PutJob(rec); err != nil {
		return err
	}
	s.emit(job)
	return nil
}

func (s *Service) Disconnect(jobID string) {
	s.disconnectWithError(jobID, "连接已断开，请手动继续")
}

func (s *Service) disconnectWithError(jobID, reason string) {
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	s.disconnectLocked(jobID, reason)
}
func (s *Service) disconnectLocked(jobID, reason string) {
	job, err := s.load(jobID)
	if err != nil || job.UserCancelled {
		return
	}
	rt := s.runtime(jobID)
	s.mu.Lock()
	rt.gen++
	rt.cancelledTurn = true
	rt.queue = nil
	if rt.cancel != nil {
		rt.cancel()
	}
	h := rt.term
	rt.term = nil
	rt.viewRequested = false
	rt.attachGen++
	if rt.waitCancel != nil {
		rt.waitCancel()
		rt.waitCancel = nil
	}
	s.mu.Unlock()
	if h != nil {
		_ = h.Close()
	}
	{
		job.ViewMode = protocol.ViewHeadless
		job.DesiredViewMode = protocol.ViewHeadless
		job.InputOwner = protocol.OwnerSupervisor
	}
	if job.State != protocol.StatePlanReady {
		job.State = protocol.StateDisconnected
	}
	if job.ApprovalDelivery == "submitted" {
		job.ApprovalDelivery = "unknown"
		job.PauseReason = "approval_delivery_unknown"
		job.Approved = false
	} else if job.State == protocol.StatePlanReady {
		job.ApprovalDelivery = "expired"
		job.PauseReason = "approval_expired"
	}
	job.LastAction = "连接已断开，请手动继续"
	job.LastSummary = reason
	s.touch(&job)
	s.save(job)
}
