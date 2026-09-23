package supervisor

import (
	"fmt"
	"grokmcp/internal/protocol"
)

// 持久化失败不能广播未落盘的成功状态；运行态明确报告调度异常。
func (s *Service) persistenceFailed(id string, err error) {
	rt := s.runtime(id)
	s.mu.Lock()
	rt.stalled = true
	rt.stalledReason = fmt.Sprintf("persistence_failed: %v", err)
	s.mu.Unlock()
	if job, e := s.load(id); e == nil {
		s.emit(job)
	}
}

// 终端连接有自己的长生命周期；提交时仅合并显示字段，不覆盖期间发生的任务边界。
func (s *Service) saveView(view protocol.Job) {
	rt := s.runtime(view.JobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	s.saveViewLocked(view)
}

func (s *Service) saveViewLocked(view protocol.Job) {
	job, err := s.load(view.JobID)
	if err != nil {
		return
	}
	job.ViewMode, job.DesiredViewMode, job.InputOwner = view.ViewMode, view.DesiredViewMode, view.InputOwner
	if job.RequestID == view.RequestID && job.State == view.State {
		job.LastAction = view.LastAction
	}
	s.touch(&job)
	s.save(job)
}
