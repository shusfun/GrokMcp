package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/ids"
	"grokmcp/internal/protocol"
	"grokmcp/internal/store"
	"grokmcp/internal/terminal"
	"grokmcp/internal/textutil"
)

var errNotFound = errors.New("job not found")

type queued struct {
	kind   string
	text   string
	repair bool
	gen    uint64
}

type runtime struct {
	busy      bool
	queue     []queued
	cancel    context.CancelFunc
	promptCh  chan struct{}
	term      terminal.Handle
	attachGen uint64
	gen       uint64
}

type Service struct {
	store    *store.Store
	agent    agent.Agent
	term     terminal.Launcher
	clock    clock.Clock
	ids      ids.Generator
	grokPath func() string

	mu       sync.Mutex
	rt       map[string]*runtime
	subs     map[int]func(protocol.Event)
	subSeq   int
	mcpN     int
	closed   bool
	pumps    sync.WaitGroup
	watchers sync.WaitGroup
}

func New(st *store.Store, ag agent.Agent, term terminal.Launcher, clk clock.Clock, idg ids.Generator) *Service {
	if clk == nil {
		clk = clock.Real{}
	}
	if idg == nil {
		idg = ids.UUID{}
	}
	s := &Service{
		store: st, agent: ag, term: term, clock: clk, ids: idg,
		rt: map[string]*runtime{}, subs: map[int]func(protocol.Event){},
		grokPath: func() string { return "grok" },
	}
	if ag != nil {
		ag.SetPlanListener(func(sessionID, excerpt string) { s.onPlanReady(sessionID, excerpt) })
	}
	return s
}

func (s *Service) SetGrokPath(fn func() string) { s.grokPath = fn }

func (s *Service) SetMCPConnected(v bool) {
	s.mu.Lock()
	if v {
		s.mcpN++
	} else if s.mcpN > 0 {
		s.mcpN--
	}
	s.mu.Unlock()
}

func (s *Service) Start(ctx context.Context) error {
	recs, err := s.store.ListJobs()
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Job.UserCancelled {
			continue
		}
		if rec.Job.State.IsActive() || rec.Job.State == protocol.StateDisconnected || rec.Job.State == protocol.StatePlanReady {
			id := rec.Job.JobID
			s.goWatch(func() { s.recoverJob(ctx, id) })
		}
	}
	return nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	var handles []terminal.Handle
	for _, rt := range s.rt {
		if rt.cancel != nil {
			rt.cancel()
		}
		if rt.term != nil {
			handles = append(handles, rt.term)
			rt.term = nil
		}
	}
	s.mu.Unlock()
	for _, h := range handles {
		_ = h.Close()
	}
	var agentErr error
	if s.agent != nil {
		agentErr = s.agent.Close()
	}
	s.pumps.Wait()
	s.watchers.Wait()
	return agentErr
}

func (s *Service) goWatch(fn func()) {
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		fn()
	}()
}

func (s *Service) Dispatch(ctx context.Context, req protocol.DispatchRequest) (protocol.DispatchResult, error) {
	if len(req.Tasks) == 0 {
		return protocol.DispatchResult{}, errors.New("tasks is required")
	}
	if err := s.agent.EnsureLeader(ctx); err != nil {
		return protocol.DispatchResult{}, err
	}
	base := req.Cwd
	if base == "" {
		wd, err := os.Getwd()
		if err != nil {
			return protocol.DispatchResult{}, err
		}
		base = wd
	}
	var jobs []protocol.Job
	for _, task := range req.Tasks {
		cwd := task.Cwd
		if cwd == "" {
			cwd = base
		}
		title := task.Title
		if title == "" {
			title = textutil.TruncateTitle(task.Prompt, 32)
		}
		now := s.clock.Now()
		job := protocol.Job{
			JobID: s.ids.JobID(), CodexThreadID: task.CodexThreadID, Cwd: cwd,
			Project: textutil.ProjectName(cwd), Title: title, State: protocol.StateCreated,
			ViewMode: protocol.ViewHeadless, InputOwner: protocol.OwnerSupervisor,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.put(job, 0, nil); err != nil {
			return protocol.DispatchResult{}, err
		}
		job = s.startJob(ctx, job, task)
		jobs = append(jobs, job)
	}
	return protocol.DispatchResult{Jobs: jobs}, nil
}

func (s *Service) startJob(ctx context.Context, job protocol.Job, task protocol.DispatchTask) protocol.Job {
	job.State = protocol.StateStarting
	s.touch(&job)
	_ = s.put(job, 0, nil)
	sid, cwd, err := s.agent.NewSession(ctx, job.Cwd, task.Worktree)
	if err != nil {
		return s.fail(job, err.Error())
	}
	job.GrokSessionID = sid
	if cwd != "" {
		job.Cwd = cwd
	}
	job.State = protocol.StatePlanning
	job.LastAction = "Planning"
	s.touch(&job)
	_ = s.put(job, 0, nil)
	s.enqueue(job.JobID, queued{kind: "plan", text: protocol.TaskContract(job.Cwd, job.Title, task.Prompt)})
	st, _ := s.store.Settings()
	if st.DefaultViewMode == string(protocol.ViewHeaded) {
		job, _ = s.attach(ctx, job)
	}
	return s.snapshot(job.JobID)
}

func (s *Service) ListJobs(context.Context) ([]protocol.Job, error) {
	recs, err := s.store.ListJobs()
	if err != nil {
		return nil, err
	}
	out := make([]protocol.Job, 0, len(recs))
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range recs {
		j := rec.Job
		if rt := s.rt[j.JobID]; rt != nil {
			j.Busy = rt.busy
		}
		j.Project = textutil.ProjectName(j.Cwd)
		j.ElapsedSeconds = textutil.ElapsedSeconds(j.CreatedAt, now)
		out = append(out, j)
	}
	return out, nil
}

func (s *Service) Status(_ context.Context, jobID string) (protocol.Job, error) {
	j, err := s.load(jobID)
	if err != nil {
		return protocol.Job{}, err
	}
	return s.decorate(j), nil
}

func (s *Service) Events(_ context.Context, jobID string) ([]protocol.BoundaryEvent, error) {
	return s.store.Events(jobID, 30)
}

func (s *Service) StatusBar(ctx context.Context) (protocol.StatusBar, error) {
	jobs, err := s.ListJobs(ctx)
	if err != nil {
		return protocol.StatusBar{}, err
	}
	s.mu.Lock()
	mcpOK := s.mcpN > 0
	s.mu.Unlock()
	bar := protocol.StatusBar{MCPOK: mcpOK}
	diag := s.agent.Diagnose(ctx)
	bar.LeaderOK = diag.LeaderRunning
	for _, j := range jobs {
		switch j.State {
		case protocol.StatePlanning, protocol.StateExecuting, protocol.StateStarting, protocol.StateRecovering:
			bar.Working++
		case protocol.StateNeedsInput, protocol.StatePlanReady:
			bar.NeedsInput++
		case protocol.StateDisconnected:
			bar.Disconnected++
		case protocol.StateFailed:
			bar.Failed++
		}
	}
	return bar, nil
}

func (s *Service) Settings(context.Context) (protocol.Settings, error) {
	return s.store.Settings()
}

func (s *Service) SaveSettings(_ context.Context, st protocol.Settings) error {
	if st.DefaultViewMode == "" {
		st.DefaultViewMode = string(protocol.ViewHeadless)
	}
	return s.store.SaveSettings(st)
}

func (s *Service) Diagnose(ctx context.Context) (protocol.DiagnoseResult, error) {
	d := s.agent.Diagnose(ctx)
	d.AttachMode = s.agent.AttachMode()
	return d, nil
}

func (s *Service) TestTerminal(ctx context.Context, template string) error {
	return s.term.TestTemplate(ctx, template, "echo grok-supervisor-terminal-ok")
}

func (s *Service) Subscribe(fn func(protocol.Event)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subSeq++
	id := s.subSeq
	s.subs[id] = fn
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *Service) load(id string) (protocol.Job, error) {
	rec, err := s.store.GetJob(id)
	if err != nil {
		return protocol.Job{}, fmt.Errorf("%w: %s", errNotFound, id)
	}
	return rec.Job, nil
}

func (s *Service) record(id string) (store.Record, error) {
	return s.store.GetJob(id)
}

func (s *Service) put(job protocol.Job, missing int, fails []int64) error {
	job.Project = textutil.ProjectName(job.Cwd)
	return s.store.PutJob(store.Record{Job: job, MissingMarkerCount: missing, RecoverFails: fails})
}

func (s *Service) save(job protocol.Job) {
	rec, err := s.store.GetJob(job.JobID)
	missing, fails := 0, []int64(nil)
	if err == nil {
		missing, fails = rec.MissingMarkerCount, rec.RecoverFails
	}
	_ = s.put(job, missing, fails)
	s.emit(job)
}

func (s *Service) touch(job *protocol.Job) {
	job.UpdatedAt = s.clock.Now()
	job.Project = textutil.ProjectName(job.Cwd)
	job.ElapsedSeconds = textutil.ElapsedSeconds(job.CreatedAt, job.UpdatedAt)
}

func (s *Service) decorate(job protocol.Job) protocol.Job {
	s.mu.Lock()
	rt := s.rt[job.JobID]
	busy := false
	if rt != nil {
		busy = rt.busy
	}
	s.mu.Unlock()
	job.Busy = busy
	job.Project = textutil.ProjectName(job.Cwd)
	job.ElapsedSeconds = textutil.ElapsedSeconds(job.CreatedAt, s.clock.Now())
	return job
}

func (s *Service) snapshot(id string) protocol.Job {
	j, err := s.load(id)
	if err != nil {
		return protocol.Job{JobID: id, State: protocol.StateFailed}
	}
	return s.decorate(j)
}

func (s *Service) fail(job protocol.Job, reason string) protocol.Job {
	job.State = protocol.StateFailed
	job.LastSummary = reason
	job.LastAction = "Failed"
	s.touch(&job)
	_ = s.store.AddEvent(job.JobID, string(protocol.StateFailed), reason, s.clock.Now())
	s.save(job)
	return s.decorate(job)
}

func (s *Service) emit(job protocol.Job) {
	job = s.decorate(job)
	s.mu.Lock()
	fns := make([]func(protocol.Event), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	ev := protocol.Event{Type: "job", Job: &job}
	for _, fn := range fns {
		fn(ev)
	}
}

func (s *Service) runtime(id string) *runtime {
	s.mu.Lock()
	defer s.mu.Unlock()
	rt := s.rt[id]
	if rt == nil {
		rt = &runtime{}
		s.rt[id] = rt
	}
	return rt
}
