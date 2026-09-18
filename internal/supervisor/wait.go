package supervisor

import (
	"context"
	"time"

	"grokmcp/internal/protocol"
)

func (s *Service) Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	if len(req.JobIDs) == 0 {
		return protocol.WaitResult{}, nil
	}
	mode := req.Mode
	if mode == "" {
		mode = "any"
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	if req.TimeoutSec <= 0 {
		timeout = 21600 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if jobs, ok := s.boundaryJobs(req.JobIDs, mode); ok {
		return protocol.WaitResult{Jobs: jobs}, nil
	}

	ch := make(chan struct{}, 1)
	off := s.Subscribe(func(protocol.Event) {
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	defer off()

	for {
		select {
		case <-ctx.Done():
			jobs := s.currentJobs(req.JobIDs)
			return protocol.WaitResult{Jobs: jobs}, ctx.Err()
		case <-ch:
			if jobs, ok := s.boundaryJobs(req.JobIDs, mode); ok {
				return protocol.WaitResult{Jobs: jobs}, nil
			}
		}
	}
}

func (s *Service) boundaryJobs(ids []string, mode string) ([]protocol.Job, bool) {
	jobs := s.currentJobs(ids)
	if len(jobs) == 0 {
		return jobs, false
	}
	hit := 0
	var matched []protocol.Job
	for _, j := range jobs {
		if j.State.IsBoundary() {
			hit++
			matched = append(matched, j)
		}
	}
	if mode == "all" {
		if hit == len(jobs) {
			return jobs, true
		}
		return nil, false
	}
	if hit > 0 {
		return matched, true
	}
	return nil, false
}

func (s *Service) currentJobs(ids []string) []protocol.Job {
	out := make([]protocol.Job, 0, len(ids))
	for _, id := range ids {
		if j, err := s.load(id); err == nil {
			out = append(out, s.decorate(j))
		}
	}
	return out
}
