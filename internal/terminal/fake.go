package terminal

import (
	"context"
	"sync"
)

type Fake struct {
	mu         sync.Mutex
	Resumes    []string
	Dashboards []string
	Dirs       []string
	Tests      []string
	Handles    map[string]chan struct{}
}

func NewFake() *Fake {
	return &Fake{Handles: map[string]chan struct{}{}}
}

func (f *Fake) OpenResume(_ context.Context, _, sessionID, cwd string) (Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Resumes = append(f.Resumes, sessionID+"|"+cwd)
	ch := make(chan struct{})
	f.Handles[sessionID] = ch
	return waitHandle{fake: f, id: sessionID}, nil
}

func (f *Fake) FocusResume(_ context.Context, sessionID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.Handles[sessionID]
	return ok, nil
}

func (f *Fake) OpenDashboard(_ context.Context, _, cwd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Dashboards = append(f.Dashboards, cwd)
	return nil
}

func (f *Fake) OpenDirectory(_ context.Context, cwd string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Dirs = append(f.Dirs, cwd)
	return nil
}

func (f *Fake) TestTemplate(_ context.Context, template, command string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Tests = append(f.Tests, template+"|"+command)
	return nil
}

func (f *Fake) CloseResume(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.Handles[sessionID]; ok {
		close(ch)
		delete(f.Handles, sessionID)
	}
}

func (f *Fake) HasResume(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.Handles[sessionID]
	return ok
}

func (f *Fake) ResumeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Resumes)
}

func (f *Fake) ResumeSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Resumes...)
}

type waitHandle struct {
	fake *Fake
	id   string
}

func (h waitHandle) Wait() error {
	h.fake.mu.Lock()
	ch := h.fake.Handles[h.id]
	h.fake.mu.Unlock()
	if ch == nil {
		return nil
	}
	<-ch
	return nil
}

func (h waitHandle) PID() int { return 1 }

func (h waitHandle) Close() error {
	h.fake.CloseResume(h.id)
	return nil
}
