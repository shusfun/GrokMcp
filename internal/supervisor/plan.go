package supervisor

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"

	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"
)

func (s *Service) PlanDecide(ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return protocol.Job{}, err
	}
	defer finish()
	job, err := s.load(req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if job.State != protocol.StatePlanReady {
		return protocol.Job{}, errors.New("job is not waiting for a plan decision")
	}
	if req.Decide != protocol.PlanCancel && s.isIdle(job.JobID) {
		if err := s.agent.LoadSession(ctx, job.GrokSessionID, job.Cwd); err != nil {
			return protocol.Job{}, err
		}
	}
	switch req.Decide {
	case protocol.PlanCancel:
		s.bumpGen(job.JobID)
		_ = s.agent.ResolvePlan(ctx, job.GrokSessionID, protocol.PlanCancel, req.Notes)
		job.State = protocol.StateCancelled
		job.UserCancelled = true
		job.LastAction = "Cancelled"
		s.touch(&job)
		_ = s.store.AddEvent(job.JobID, string(protocol.StateCancelled), "plan cancelled", s.clock.Now())
		s.save(job)
		return s.snapshot(job.JobID), nil
	case protocol.PlanRevise:
		s.bumpGen(job.JobID)
		_ = s.agent.ResolvePlan(ctx, job.GrokSessionID, protocol.PlanRevise, req.Notes)
		job.State = protocol.StatePlanning
		job.LastAction = "Revising plan"
		s.touch(&job)
		s.save(job)
		s.enqueue(job.JobID, queued{connect: true, kind: "revise", text: protocol.RevisePrompt(req.Notes)})
		return s.snapshot(job.JobID), nil
	case protocol.PlanApprove:
		s.bumpGen(job.JobID)
		_ = s.agent.ResolvePlan(ctx, job.GrokSessionID, protocol.PlanApprove, req.Notes)
		job, _ = s.load(req.JobID)
		if job.State == protocol.StatePlanReady {
			job.State = protocol.StateExecuting
			job.LastAction = "Implementing"
			s.touch(&job)
			s.save(job)
		}
		s.enqueue(job.JobID, queued{connect: true, kind: "approve", text: protocol.ApprovePrompt(req.Notes)})
		return s.snapshot(job.JobID), nil
	default:
		return protocol.Job{}, errors.New("invalid plan decision")
	}
}

func (s *Service) onPlanReady(sessionID, excerpt string) {
	if sessionID == "" {
		return
	}
	recs, err := s.store.ListJobs()
	if err != nil {
		return
	}
	for _, rec := range recs {
		if rec.Job.GrokSessionID == sessionID {
			s.becomePlanReady(rec.Job, excerpt)
			return
		}
	}
}

func (s *Service) becomePlanReady(job protocol.Job, text string) {
	if job.State == protocol.StatePlanReady && job.PlanSummary != "" {
		return
	}
	if job.State != protocol.StatePlanReady && job.State != protocol.StatePlanning && job.State != protocol.StateStarting {
		return
	}
	summary := readPlanFile(job.Cwd, job.GrokSessionID)
	if summary == "" {
		summary = text
	}
	if summary == "" && job.PlanSummary != "" {
		summary = job.PlanSummary
	}
	if len(summary) > 32*1024 {
		summary = summary[:32*1024]
	}
	job.State = protocol.StatePlanReady
	job.LastAction = "Plan ready"
	job.PlanSummary = summary
	job.LastSummary = textutil.TruncateTitle(summary, 180)
	if job.LastSummary == "未命名任务" || job.LastSummary == "" {
		job.LastSummary = "Plan ready"
	}
	job.PlanDigest = textutil.Digest(summary)
	s.touch(&job)
	_ = s.store.AddEvent(job.JobID, string(protocol.StatePlanReady), job.LastAction, s.clock.Now())
	s.save(job)
	s.emitTrace(job, "info", "acp", "plan.ready", "plan ready", nil)
	s.maybeAttachDesired(job.JobID)
}

func readPlanFile(cwd, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	encoded := url.PathEscape(cwd)
	p := filepath.Join(home, ".grok", "sessions", encoded, sessionID, "plan.md")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}
