package supervisor

import (
	"context"
	"fmt"
	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"
	"strings"
	"time"
)

const defaultWaitTimeout = 300 * time.Second

func (s *Service) Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	if len(req.JobIDs) == 0 {
		return protocol.WaitResult{}, fmt.Errorf("job_ids is required")
	}
	if req.Mode == "" {
		req.Mode = "any"
	}
	if req.Mode != "any" && req.Mode != "all" {
		return protocol.WaitResult{}, fmt.Errorf("mode must be any or all")
	}
	if req.TimeoutSec < 0 || req.TimeoutSec > 21600 {
		return protocol.WaitResult{}, fmt.Errorf("timeout_sec must be between 0 and 21600")
	}
	ids := map[string]bool{}
	for _, id := range req.JobIDs {
		if id == "" || ids[id] {
			return protocol.WaitResult{}, fmt.Errorf("empty or duplicate job_id")
		}
		ids[id] = true
	}
	for id, cursor := range req.Cursors {
		if !ids[id] || cursor < 0 {
			return protocol.WaitResult{}, fmt.Errorf("invalid cursor for %s", id)
		}
	}
	// 先订阅再读取已提交状态，消除检查与订阅间的漏通知窗口。
	ch := make(chan struct{}, 1)
	off := s.Subscribe(func(ev protocol.Event) {
		if (ev.Job != nil && ids[ev.Job.JobID]) || ids[ev.JobID] {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	})
	defer off()
	duration := defaultWaitTimeout
	if req.TimeoutSec > 0 {
		duration = time.Duration(req.TimeoutSec) * time.Second
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	snapshot := func() (protocol.WaitResult, bool, error) {
		out := protocol.WaitResult{Jobs: make([]protocol.Job, 0, len(req.JobIDs)), Cursors: map[string]int64{}}
		hit := 0
		for _, id := range req.JobIDs {
			cursor, provided := req.Cursors[id]
			rt := s.runtime(id)
			rt.coord.Lock()
			rec, events, err := s.store.WaitSnapshot(id, cursor)
			j := s.decorate(rec.Job)
			rt.coord.Unlock()
			if err != nil {
				return out, false, err
			}
			if strings.HasPrefix(j.StalledReason, "persistence_failed:") {
				return out, false, fmt.Errorf("%s", j.StalledReason)
			}
			actionable := (j.State.IsBoundary() || j.Stalled) && (j.State != protocol.StateCompleted || (!j.Busy && j.QueueLength == 0))
			fresh := !provided || j.EventCursor > cursor
			replay := false
			for _, boundary := range events {
				// 被同一请求后续状态取代的审批/输入不再是可行动事件。
				if boundary.State != j.State || boundary.ApprovalID != j.ApprovalID {
					continue
				}
				if boundary.State != protocol.StatePlanReady {
					boundary.PlanSummary = ""
				}
				out.Boundaries = append(out.Boundaries, boundary)
				replay = true
			}
			if actionable && (fresh || replay) {
				hit++
			}

			out.Cursors[id] = j.EventCursor
			// 普通状态简报不重复方案正文；完成结果只在新的边界返回。
			if j.State != protocol.StatePlanReady {
				j.PlanSummary = ""
			}
			if !actionable || !fresh {
				j.PlanSummary = ""
				if j.State == protocol.StateCompleted {
					j.LastSummary = ""
				}
			}
			out.Jobs = append(out.Jobs, j)
		}
		return out, hit > 0 && (req.Mode == "any" || hit == len(req.JobIDs)), nil
	}
	for {
		out, ready, err := snapshot()
		if err != nil {
			return protocol.WaitResult{}, err
		}
		if ready {
			out.Reason = "boundary"
			return out, nil
		}
		select {
		case <-ctx.Done():
			return protocol.WaitResult{}, ctx.Err()
		case <-s.stopWatch:
			return protocol.WaitResult{}, fmt.Errorf("supervisor closed")
		case <-timer.C:
			out, ready, err := snapshot()
			if err != nil {
				return out, err
			}
			if ready {
				out.Reason = "boundary"
				return out, nil
			}
			out.Reason = "report_due"
			out.Boundaries = nil
			for i := range out.Jobs {
				out.Jobs[i].PlanSummary = ""
				if out.Jobs[i].LastSummary != "" {
					out.Jobs[i].LastSummary = textutil.TruncateTitle(out.Jobs[i].LastSummary, 180)
				}
				if out.Jobs[i].State == protocol.StateCompleted {
					out.Jobs[i].LastSummary = ""
				}
			}
			// all 等待尚未凑齐时，不消费已经到达的单任务边界。
			if req.Mode == "all" {
				for _, id := range req.JobIDs {
					out.Cursors[id] = req.Cursors[id]
				}
			}
			return out, nil
		case <-ch:
		}
	}
}

func (s *Service) currentJobs(ids []string) []protocol.Job {
	out := make([]protocol.Job, 0, len(ids))
	for _, id := range ids {
		if j, err := s.Status(context.Background(), id); err == nil {
			out = append(out, j)
		}
	}
	return out
}
