package supervisor

import (
	"context"
	"errors"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
)

var (
	errPlanPending = errors.New("plan pending decision")
	errJobInactive = errors.New("job is no longer active")
)

func acceptsTurnControl(job protocol.Job) bool {
	if job.UserCancelled {
		return false
	}
	switch job.State {
	case protocol.StateCancelled, protocol.StateCompleted, protocol.StateFailed:
		return false
	default:
		return true
	}
}

func (s *Service) applyPromptResult(jobID string, item queued, res agent.PromptResult) {
	if item.gen != s.currentGen(jobID) {
		return
	}
	rec, err := s.record(jobID)
	if err != nil {
		return
	}
	job := rec.Job
	if res.LastAction != "" {
		job.LastAction = res.LastAction
	}
	if res.StopReason == "cancelled" {
		if job.UserCancelled {
			job.State = protocol.StateCancelled
			job.LastAction = "Cancelled"
			s.touch(&job)
			_ = s.store.AddEvent(job.JobID, string(protocol.StateCancelled), "cancelled", s.clock.Now())
			s.save(job)
			return
		}
	}

	if res.PlanReady && (job.State == protocol.StatePlanning || job.State == protocol.StateStarting || job.State == protocol.StatePlanReady) {
		s.becomePlanReady(job, res.Text)
		return
	}

	ts, ok := protocol.ParseTaskState(res.Text)
	if !ok {
		if item.kind == "plan" && job.State == protocol.StateExecuting {
			s.touch(&job)
			s.save(job)
			return
		}
		missing := rec.MissingMarkerCount + 1
		if item.repair {
			job.State = protocol.StateNeedsInput
			job.LastSummary = "missing GROK_TASK_STATE"
			job.LastAction = "Needs input"
			s.touch(&job)
			_ = s.put(job, missing, rec.RecoverFails)
			_ = s.store.AddEvent(job.JobID, string(protocol.StateNeedsInput), job.LastSummary, s.clock.Now())
			s.emit(job)
			return
		}
		_ = s.put(job, missing, rec.RecoverFails)
		s.enqueue(jobID, queued{kind: "repair", text: protocol.RepairPrompt(), repair: true})
		return
	}

	job.LastSummary = ts.Summary
	switch ts.State {
	case protocol.MarkerWorking:
		if job.State == protocol.StatePlanning || job.State == protocol.StatePlanReady {
			job.State = protocol.StateExecuting
		} else if job.State != protocol.StateCancelled {
			job.State = protocol.StateExecuting
		}
		job.LastAction = firstNonEmpty(res.LastAction, "Working")
		s.touch(&job)
		_ = s.put(job, 0, rec.RecoverFails)
		s.emit(job)
		if job.ViewMode != protocol.ViewHeaded {
			s.enqueue(jobID, queued{kind: "continue", text: protocol.ContinuePrompt()})
		}
	case protocol.MarkerNeedsInput:
		job.State = protocol.StateNeedsInput
		job.LastAction = "Needs input"
		s.touch(&job)
		_ = s.put(job, 0, rec.RecoverFails)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateNeedsInput), ts.Summary, s.clock.Now())
		s.emit(job)
	case protocol.MarkerCompleted:
		job.State = protocol.StateCompleted
		job.LastAction = firstNonEmpty(res.LastAction, "Completed")
		s.touch(&job)
		_ = s.put(job, 0, rec.RecoverFails)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateCompleted), ts.Summary, s.clock.Now())
		s.emit(job)
	case protocol.MarkerBlocked:
		job.State = protocol.StateBlocked
		job.LastAction = "Blocked"
		s.touch(&job)
		_ = s.put(job, 0, rec.RecoverFails)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateBlocked), ts.Summary, s.clock.Now())
		s.emit(job)
	}
}

func (s *Service) Followup(_ context.Context, req protocol.FollowupRequest) (protocol.Job, error) {
	job, err := s.load(req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if job.State == protocol.StateCancelled {
		return s.decorate(job), nil
	}
	if job.State == protocol.StatePlanReady {
		return s.decorate(job), errPlanPending
	}
	if job.State == protocol.StateNeedsInput || job.State == protocol.StateBlocked {
		job.State = protocol.StateExecuting
	}
	s.touch(&job)
	s.save(job)
	s.enqueue(req.JobID, queued{kind: "followup", text: req.Prompt})
	return s.snapshot(req.JobID), nil
}

func (s *Service) Continue(_ context.Context, jobID string) (protocol.Job, error) {
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if !acceptsTurnControl(job) {
		return protocol.Job{}, errJobInactive
	}
	if job.State == protocol.StatePlanReady {
		return s.decorate(job), errPlanPending
	}
	if job.State == protocol.StateNeedsInput || job.State == protocol.StateBlocked {
		job.State = protocol.StateExecuting
		s.touch(&job)
		s.save(job)
	}
	s.enqueue(jobID, queued{kind: "continue", text: protocol.ContinuePrompt()})
	return s.snapshot(jobID), nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
