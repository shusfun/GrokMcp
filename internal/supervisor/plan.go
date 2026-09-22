package supervisor

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"grokmcp/internal/agent"
	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"
)

func (s *Service) PlanDecide(ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
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
	if job.State != protocol.StatePlanReady || job.ApprovalDelivery == "submitted" {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("job is not waiting for a plan decision")
	}
	if req.ApprovalID == "" || req.ApprovalID != job.ApprovalID || req.RequestID != job.RequestID || req.TurnID != job.PlanTurnID || req.PlanVersion != job.PlanVersion {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("stale plan decision")
	}
	if req.Decide != protocol.PlanApprove && req.Decide != protocol.PlanRevise && req.Decide != protocol.PlanCancel {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("invalid plan decision")
	}
	if req.Decide == protocol.PlanApprove && job.PlanSummary == "" {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("current approval has no verified plan content")
	}
	pending, err := s.agent.PendingApproval(req.ApprovalID)
	if err != nil {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("approval expired; resume the original session to request a fresh native approval")
	}
	if pending.ConnectionID != job.ApprovalConnectionID || pending.RequestID != job.RequestID || pending.TurnID != job.PlanTurnID || pending.SessionID != job.GrokSessionID {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("stale approval connection")
	}
	if req.Notes != "" && !pending.SupportsNotes {
		rt.coord.Unlock()
		return protocol.Job{}, errors.New("this ACP permission does not support approval notes")
	}
	job.ApprovalDelivery = "submitted"
	job.PauseReason = "approval_delivery"
	job.LastAction = "审批决定已记录，等待 Grok 响应"
	switch req.Decide {
	case protocol.PlanApprove:
		job.State = protocol.StateExecuting
		job.Approved = true
	case protocol.PlanRevise:
		job.State = protocol.StatePlanning
		job.Approved = false
	case protocol.PlanCancel:
		job.State = protocol.StateCancelled
		job.UserCancelled = true
	}
	s.touch(&job)
	if err := s.save(job); err != nil {
		rt.coord.Unlock()
		return protocol.Job{}, err
	}
	rt.coord.Unlock()
	// 外部权限交付不持有协调锁；原调用及下一份审批仍能推进。
	err = s.agent.ResolveApproval(ctx, agent.PlanDecision{PlanRequest: pending, Decide: req.Decide, Notes: req.Notes})
	rt.coord.Lock()
	current, loadErr := s.load(req.JobID)
	if loadErr == nil && current.ApprovalID == req.ApprovalID && current.ApprovalDelivery == "submitted" && err != nil {
		current.ApprovalDelivery = "unknown"
		current.PauseReason = "approval_delivery_unknown"
		current.State = protocol.StateNeedsInput
		current.Approved = false
		current.LastAction = "审批交付未确认，不会自动重发"
		s.touch(&current)
		if saveErr := s.save(current); saveErr != nil {
			rt.coord.Unlock()
			return protocol.Job{}, saveErr
		}
	}
	if req.Decide == protocol.PlanCancel {
		s.mu.Lock()
		rt.gen++
		rt.cancelledTurn = true
		rt.queue = nil
		if rt.cancel != nil {
			rt.cancel()
		}
		s.mu.Unlock()
	}
	rt.coord.Unlock()
	if req.Decide == protocol.PlanCancel {
		if cancelErr := s.agent.Cancel(ctx, job.GrokSessionID); err == nil {
			err = cancelErr
		}
	}
	return s.snapshot(req.JobID), err
}

func (s *Service) onApproval(p agent.PlanRequest) error {
	if p.ID == "" || p.ConnectionID == "" || p.RequestID == "" || p.TurnID == "" || p.SessionID == "" {
		return errors.New("incomplete native approval identity")
	}
	id := s.jobIDForSession(p.SessionID)
	if id == "" {
		return errors.New("approval for unknown session")
	}
	rt := s.runtime(id)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	job, err := s.load(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	valid := rt.busy && rt.turnID == p.TurnID && rt.requestID == p.RequestID && !rt.cancelledTurn
	started, priorDigest := rt.turnStartedAt, rt.planFileDigest
	s.mu.Unlock()
	if !valid || job.UserCancelled || job.RequestID != p.RequestID {
		return errors.New("approval for stale turn")
	}
	if p.ID == job.ApprovalID {
		return nil
	}
	content := p.Content
	if content == "" {
		path := s.planPath(job.Cwd, job.GrokSessionID)
		if st, e := os.Stat(path); e == nil && !st.ModTime().Before(started) {
			if b, e := os.ReadFile(path); e == nil && textutil.Digest(string(b)) != priorDigest {
				content = string(b)
			}
		}
	}
	job.State = protocol.StatePlanReady
	job.Approved = false
	job.PlanVersion++
	job.PlanTurnID = p.TurnID
	job.ApprovalID = p.ID
	job.ApprovalConnectionID = p.ConnectionID
	job.ApprovalSupportsNotes = p.SupportsNotes
	job.ApprovalDelivery = "pending"
	job.PauseReason = "approval"
	job.PlanSummary = content
	job.PlanDigest = textutil.Digest(content)
	job.LastAction = "Plan ready"
	job.LastSummary = textutil.TruncateTitle(content, 180)
	if content == "" {
		job.LastSummary = "已收到原生审批请求，但本次方案正文缺失"
		job.PauseReason = "plan_content_missing"
	}
	s.mu.Lock()
	rt.lastActivity = p.CreatedAt
	rt.activityKind = "plan_permission"
	s.mu.Unlock()
	s.touch(&job)
	if err := s.save(job); err != nil {
		return err
	}
	s.goWatch(func() { s.maybeAttachDesired(id) })
	return nil
}

func planFilePath(cwd, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	root := filepath.Join(home, ".grok", "sessions")
	return resolvePlanPath(root, cwd, sessionID)
}
func resolvePlanPath(root, cwd, sessionID string) string {
	variants := []string{cwd}
	if len(cwd) > 2 && cwd[1] == ':' {
		variants = append(variants, strings.ReplaceAll(cwd, "\\", "/"), strings.ReplaceAll(cwd, "/", "\\"))
	}
	seen := map[string]bool{}
	found := ""
	for _, variant := range variants {
		path := filepath.Join(root, encodeSessionCwd(variant), sessionID, "plan.md")
		if seen[path] {
			continue
		}
		seen[path] = true
		if info, err := os.Stat(filepath.Dir(path)); err == nil && info.IsDir() {
			if found != "" && found != path {
				return ""
			}
			found = path
		}
	}
	if found != "" {
		return found
	}
	return filepath.Join(root, encodeSessionCwd(cwd), sessionID, "plan.md")
}

func readPlanFile(cwd, sessionID string) string {
	b, err := os.ReadFile(planFilePath(cwd, sessionID))
	if err != nil {
		return ""
	}
	return string(b)
}

// Grok 的会话目录对盘符冒号也编码；PathEscape 会保留冒号，在 Windows 上产生非法目录。
func encodeSessionCwd(cwd string) string { return strings.ReplaceAll(url.QueryEscape(cwd), "+", "%20") }
