package supervisor

import (
	"context"
	"encoding/json"
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/trace"
)

func (s *Service) DebugSet(ctx context.Context, req protocol.DebugSetRequest) (protocol.Job, error) {
	if req.JobID == "" {
		st, err := s.store.Settings()
		if err != nil {
			return protocol.Job{}, err
		}
		st.DebugEnabled = req.Enabled
		st.DebugPayloads = req.Payloads
		if err := s.SaveSettings(ctx, st); err != nil {
			return protocol.Job{}, err
		}
		return protocol.Job{DebugEnabled: req.Enabled}, nil
	}
	if _, err := s.load(req.JobID); err != nil {
		return protocol.Job{}, err
	}
	if s.traces != nil {
		s.traces.SetDebug(req.JobID, req.Enabled, req.Payloads)
	}
	return s.snapshot(req.JobID), nil
}

func (s *Service) DebugSnapshot(_ context.Context, req protocol.DebugSnapshotRequest) (protocol.DebugSnapshot, error) {
	if _, err := s.load(req.JobID); err != nil {
		return protocol.DebugSnapshot{}, err
	}
	if s.traces == nil {
		return protocol.DebugSnapshot{JobID: req.JobID, Events: []protocol.TraceEvent{}}, nil
	}
	return toProto(s.traces.Snapshot(req.JobID, req.Cursor, req.Limit, req.Levels, req.Sources)), nil
}

func (s *Service) DebugWait(ctx context.Context, req protocol.DebugWaitRequest) (protocol.DebugSnapshot, error) {
	if _, err := s.load(req.JobID); err != nil {
		return protocol.DebugSnapshot{}, err
	}
	if s.traces == nil {
		return protocol.DebugSnapshot{JobID: req.JobID, Events: []protocol.TraceEvent{}}, nil
	}
	timeout := time.Duration(req.TimeoutSec) * time.Second
	return toProto(s.traces.Wait(ctx, req.JobID, req.Cursor, req.Until, timeout)), nil
}

func (s *Service) TraceSubscribe(fn func(trace.Event)) func() {
	if s.traces == nil {
		return func() {}
	}
	return s.traces.Subscribe(fn)
}

func (s *Service) DebugExport(_ context.Context, jobID string) (protocol.DebugExportResult, error) {
	if _, err := s.load(jobID); err != nil {
		return protocol.DebugExportResult{}, err
	}
	if s.traces == nil {
		return protocol.DebugExportResult{}, nil
	}
	path, err := s.traces.Export(jobID)
	return protocol.DebugExportResult{Path: path}, err
}

func toProto(s trace.Snapshot) protocol.DebugSnapshot {
	evs := make([]protocol.TraceEvent, 0, len(s.Events))
	for _, e := range s.Events {
		raw, _ := json.Marshal(e)
		var ev protocol.TraceEvent
		_ = json.Unmarshal(raw, &ev)
		ev.Event = e.Name
		evs = append(evs, ev)
	}
	return protocol.DebugSnapshot{JobID: s.JobID, Cursor: s.Cursor, Events: evs}
}
