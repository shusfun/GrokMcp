package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"grokmcp/internal/protocol"
)

var (
	errArchiveActive = errors.New("cannot archive active job")
	errDeleteActive  = errors.New("cannot delete active job")
)

func (s *Service) ListJobsPage(_ context.Context, q protocol.ListJobsQuery) (protocol.JobPage, error) {
	recs, next, more, err := s.store.ListJobsPage(q)
	if err != nil {
		return protocol.JobPage{}, err
	}
	out := make([]protocol.Job, 0, len(recs))
	for _, rec := range recs {
		out = append(out, s.decorate(rec.Job))
	}
	if out == nil {
		out = []protocol.Job{}
	}
	return protocol.JobPage{Jobs: out, NextCursor: next, HasMore: more}, nil
}

func (s *Service) ArchiveJob(_ context.Context, jobID string) (protocol.Job, error) {
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if protocol.DashboardActive(job.State) {
		return protocol.Job{}, errArchiveActive
	}
	if job.ArchivedAt.IsZero() {
		job.ArchivedAt = s.clock.Now()
		s.touch(&job)
		s.save(job)
	}
	return s.snapshot(jobID), nil
}

func (s *Service) UnarchiveJob(_ context.Context, jobID string) (protocol.Job, error) {
	rt := s.runtime(jobID)
	rt.coord.Lock()
	defer rt.coord.Unlock()
	job, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	if !job.ArchivedAt.IsZero() {
		job.ArchivedAt = protocol.UnixTime(0)
		s.touch(&job)
		s.save(job)
	}
	return s.snapshot(jobID), nil
}

func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	rec, err := s.store.GetJob(jobID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.cleanupDeleted(jobID)
		}
		return err
	}
	if protocol.DashboardActive(rec.Job.State) {
		return errDeleteActive
	}
	if rec.Job.ViewMode == protocol.ViewHeaded || rec.Job.ViewMode == protocol.ViewAttaching || rec.Job.ViewMode == protocol.ViewDetaching {
		_, _ = s.detach(ctx, rec.Job, false)
	}
	if err := s.store.DeleteJob(jobID); err != nil {
		return err
	}
	s.emitDeleted(jobID)
	if err := s.cleanupDeleted(jobID); err != nil {
		return fmt.Errorf("job deleted but failed to remove traces: %w", err)
	}
	return nil
}

func (s *Service) emitDeleted(jobID string) {
	s.mu.Lock()
	fns := make([]func(protocol.Event), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	ev := protocol.Event{Type: "job.deleted", JobID: jobID}
	for _, fn := range fns {
		fn(ev)
	}
}

func (s *Service) cleanupDeleted(jobID string) error {
	s.dropRuntime(jobID)
	if s.traces == nil {
		return nil
	}
	return s.traces.Remove(jobID)
}

func (s *Service) dropRuntime(jobID string) {
	s.mu.Lock()
	rt := s.rt[jobID]
	var h interface{ Close() error }
	if rt != nil {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.waitCancel != nil {
			rt.waitCancel()
			rt.waitCancel = nil
		}
		if rt.term != nil {
			h = rt.term
			rt.term = nil
		}
		rt.attachGen++
		delete(s.rt, jobID)
	}
	s.mu.Unlock()
	if h != nil {
		_ = h.Close()
	}
}
