package terminal

import (
	"context"
	"grokmcp/internal/ownedprocess"
	"sync"
)

type managedTerminals struct {
	mu         sync.Mutex
	closed     bool
	handles    map[string]Handle
	workers    map[string]*terminalWorker
	workerExit func(string, string)
}

func newManagedTerminals() *managedTerminals {
	return &managedTerminals{handles: map[string]Handle{}, workers: map[string]*terminalWorker{}}
}

type terminalWorker struct {
	mu               sync.Mutex
	process          *ownedprocess.Process
	group            *ownedprocess.Group
	console          *ownedprocess.Console
	jobID, sessionID string
	generation       string
	ready            chan struct{}
	once             sync.Once
}

func (w *terminalWorker) alive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.process != nil && w.process.Alive()
}
func (w *terminalWorker) close() {
	w.once.Do(func() {
		w.mu.Lock()
		group := w.group
		w.mu.Unlock()
		if group != nil {
			_ = group.Close()
		}
		if w.console != nil {
			_ = w.console.Close()
		}
	})
}
func (e Exec) WorkerAlive(sessionID string) bool {
	if e.managed == nil {
		return false
	}
	e.managed.mu.Lock()
	w := e.managed.workers[sessionID]
	e.managed.mu.Unlock()
	return w != nil && w.alive()
}
func (e Exec) Active() bool {
	if e.managed == nil {
		return false
	}
	e.managed.mu.Lock()
	defer e.managed.mu.Unlock()
	for _, h := range e.managed.handles {
		if h.PID() > 0 {
			return true
		}
	}
	for _, w := range e.managed.workers {
		if w.alive() {
			return true
		}
	}
	return false
}
func (e Exec) SetWorkerExitListener(fn func(string, string)) {
	if e.managed != nil {
		e.managed.mu.Lock()
		e.managed.workerExit = fn
		e.managed.mu.Unlock()
	}
}
func (e Exec) CloseAll() {
	if e.managed == nil {
		return
	}
	e.managed.mu.Lock()
	e.managed.closed = true
	e.managed.mu.Unlock()
	e.ReleaseWorkers()
}
func (e Exec) ReleaseWorkers() {
	if e.managed == nil {
		return
	}
	e.managed.mu.Lock()
	handles, workers := e.managed.handles, e.managed.workers
	e.managed.handles = map[string]Handle{}
	e.managed.workers = map[string]*terminalWorker{}
	e.managed.mu.Unlock()
	for _, h := range handles {
		_ = h.Close()
	}
	for _, w := range workers {
		w.close()
	}
}

type jobContextKey struct{}

func WithJob(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, jobContextKey{}, jobID)
}
func viewJob(ctx context.Context, sessionID string) string {
	if id, ok := ctx.Value(jobContextKey{}).(string); ok {
		return id
	}
	if sessionID == "" {
		return "dashboard"
	}
	return sessionID
}

func (e Exec) WorkerPID(sessionID string) int {
	if e.managed == nil {
		return 0
	}
	e.managed.mu.Lock()
	w := e.managed.workers[sessionID]
	e.managed.mu.Unlock()
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.process == nil {
		return 0
	}
	return w.process.PID
}

func (e Exec) SessionOutput(sessionID string) []byte {
	if e.managed == nil {
		return nil
	}
	e.managed.mu.Lock()
	w := e.managed.workers[sessionID]
	e.managed.mu.Unlock()
	if w == nil || w.console == nil {
		return nil
	}
	return w.console.Output()
}

func (e Exec) WorkerGeneration(sid string) string {
	if e.managed == nil {
		return ""
	}
	e.managed.mu.Lock()
	defer e.managed.mu.Unlock()
	if w := e.managed.workers[sid]; w != nil {
		return w.generation
	}
	return ""
}
