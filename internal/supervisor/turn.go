package supervisor

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"strings"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
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
	if state, err := s.store.RequestState(jobID, item.requestID); err == nil && state == "cancelled" {
		return
	}
	s.mu.Lock()
	rt := s.rt[jobID]
	if rt != nil && ((rt.turnID != "" && rt.turnID != item.turnID) || (rt.busy && rt.requestID != "" && rt.requestID != item.requestID)) {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	rec, err := s.record(jobID)
	if err != nil {
		return
	}
	job := rec.Job
	if job.UserCancelled {
		return
	}
	job.ActiveTurnID = ""
	job.ResultTurnID = item.turnID
	job.PauseReason = ""
	if job.ApprovalDelivery == "submitted" {
		job.ApprovalDelivery = "confirmed"
	}
	s.mu.Lock()
	rt = s.rt[jobID]
	pending := len(rt.queue) > 0
	job.LastActivityAt = rt.lastActivity
	job.ActivityKind = rt.activityKind
	s.mu.Unlock()
	ts, marked := protocol.ParseTaskState(res.Text)
	if !marked {
		job.State = protocol.StateNeedsInput
		job.PauseReason = "review_required"
		job.LastAction = "最终回答待验收"
		job.LastSummary = "本轮已返回最终回答，请读取当前请求结果并验收"
	} else {
		job.LastSummary = ts.Summary
		switch ts.State {
		case protocol.MarkerWorking:
			job.State = protocol.StateExecuting
			job.LastAction = "Working"
		case protocol.MarkerNeedsInput:
			job.State = protocol.StateNeedsInput
			job.LastAction = "Needs input"
		case protocol.MarkerBlocked:
			job.State = protocol.StateBlocked
			job.LastAction = "Blocked"
		case protocol.MarkerCompleted:
			job.State = protocol.StateCompleted
			job.LastAction = "Completed"
		}
	}
	resultState := job.State
	if job.State == protocol.StateCompleted && pending {
		job.State = protocol.StateExecuting
		job.LastAction = "Queued"
		job.LastSummary = "当前请求已完成，等待处理下一请求"
	}
	s.touch(&job)
	rec.Job = job
	rec.RequestPhase = "returned"
	rec.MissingMarkerCount = 0
	rec.Result = &store.RequestResult{RequestID: item.requestID, TurnID: item.turnID, Summary: ts.Summary, Text: res.Text, StopReason: res.StopReason, State: resultState}
	if err := s.store.PutJob(rec); err != nil {
		s.persistenceFailed(jobID, err)
		return
	}
	s.emit(job)
	if marked && ts.State == protocol.MarkerWorking && job.InputOwner == protocol.OwnerSupervisor {
		if err := s.enqueue(jobID, queued{kind: "continue", text: protocol.ContinuePrompt(), requestID: item.requestID}); err != nil {
			s.persistenceFailed(jobID, err)
		}
	}
}

func (s *Service) Followup(ctx context.Context, req protocol.FollowupRequest) (protocol.Job, error) {
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return protocol.Job{}, err
	}
	defer finish()
	if strings.TrimSpace(req.Prompt) == "" {
		return protocol.Job{}, errors.New("prompt is required")
	}
	planning, err := protocol.ResolvePlanning(req.Planning, req.Replan)
	if err != nil {
		return protocol.Job{}, err
	}
	rt := s.runtime(req.JobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	job, err := s.load(req.JobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if job.State == protocol.StateCancelled {
		return s.decorate(job), errJobInactive
	}
	if job.State == protocol.StatePlanReady && job.PauseReason != "approval_expired" {
		return s.decorate(job), errPlanPending
	}
	requestID := uuid.NewString()
	s.mu.Lock()
	busy := rt.busy
	s.mu.Unlock()
	held := s.sessionHeldByTUI(job)
	if !busy && !held {
		job.RequestID = requestID
		job.LastSummary = ""
		job.LastAction = "Accepted"
		job.PauseReason = ""
		job.ResultTurnID = ""
		job.State = protocol.StateExecuting
		if planning {
			job.State = protocol.StatePlanning
			job.Approved = false
			job.PlanSummary = ""
			job.PlanDigest = ""
		}
		s.touch(&job)

	}
	text := protocol.ExecuteContract(job.Cwd, job.Title, req.Prompt)
	if planning {
		text = protocol.TaskContract(job.Cwd, job.Title, req.Prompt)
	}
	accepted := (*protocol.Job)(nil)
	if !busy && !held {
		accepted = &job
	}
	if err := s.enqueue(req.JobID, queued{acceptedJob: accepted, connect: true, kind: "followup", text: text, requestID: requestID, planning: planning}); err != nil {
		return protocol.Job{}, err
	}
	out := s.snapshot(req.JobID)
	out.AcceptedRequestID = requestID
	return out, nil
}

func (s *Service) Continue(ctx context.Context, jobID string) (protocol.Job, error) {
	finish, err := s.beginOperation(ctx)
	if err != nil {
		return protocol.Job{}, err
	}
	defer finish()
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if !acceptsTurnControl(job) {
		return protocol.Job{}, errJobInactive
	}
	if job.State == protocol.StatePlanReady && job.PauseReason != "approval_expired" {
		return s.decorate(job), errPlanPending
	}
	planning := false
	text := protocol.ContinuePrompt()
	if prior, e := s.store.Request(jobID, job.RequestID); e == nil {
		planning = prior.Planning
		if prior.Phase == "queued" || prior.Phase == "preparing" {
			text = prior.Prompt
		}
	}
	if planning && !strings.Contains(text, "exit_plan_mode") {
		text = "Resume this original session. Reconcile work already performed; do not repeat side effects. Present the existing or revised plan using the native exit_plan_mode approval request before implementation.\n" + text
	}
	if s.sessionHeldByTUI(job) {
		if !s.requestQueued(jobID, job.RequestID) {
			if err := s.enqueue(jobID, queued{connect: true, kind: "continue", text: text, requestID: job.RequestID, planning: planning}); err != nil {
				return protocol.Job{}, err
			}
		}
		out := s.snapshot(jobID)
		out.AcceptedRequestID = job.RequestID
		return out, nil
	}
	job.State = protocol.StateExecuting
	if planning {
		job.State = protocol.StatePlanning
		job.Approved = false
	}
	job.PauseReason = ""
	job.ApprovalDelivery = ""
	s.touch(&job)
	s.mu.Lock()
	restore := len(rt.queue) == 0 && (!rt.busy || rt.cancelledTurn)
	s.mu.Unlock()
	pending, err := s.store.QueuedRequests(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if err := s.enqueue(jobID, queued{acceptedJob: &job, connect: true, kind: "continue", text: text, requestID: job.RequestID, planning: planning}); err != nil {
		return protocol.Job{}, err
	}

	if restore {
		for _, r := range pending {
			if r.RequestID != job.RequestID {
				if err := s.enqueue(jobID, queued{connect: true, kind: "followup", text: r.Prompt, requestID: r.RequestID, planning: r.Planning}); err != nil {
					return protocol.Job{}, err
				}
			}
		}
	}
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
