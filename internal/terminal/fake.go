package terminal

import (
	"context"
	"sync"
	"time"
)

type fakeState struct {
	wait     chan struct{}
	closeCh  chan struct{}
	process  bool
	window   bool
	pid      int
	windowID string
	closed   bool
}

type Fake struct {
	mu         sync.Mutex
	Resumes    []string
	Dashboards []string
	Dirs       []string
	Tests      []string
	Handles    map[string]*fakeState
	CloseDelay time.Duration
	CloseBlock map[string]chan struct{}
}

func NewFake() *Fake {
	return &Fake{Handles: map[string]*fakeState{}, CloseBlock: map[string]chan struct{}{}}
}

func (f *Fake) OpenResume(_ context.Context, _, sessionID, cwd string) (Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Resumes = append(f.Resumes, sessionID+"|"+cwd)
	st := &fakeState{
		wait:     make(chan struct{}),
		process:  true,
		window:   true,
		pid:      4242,
		windowID: "w-" + sessionID,
	}
	f.Handles[sessionID] = st
	return waitHandle{fake: f, id: sessionID}, nil
}

func (f *Fake) FocusResume(_ context.Context, sessionID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.Handles[sessionID]
	return ok && (st.process || st.window), nil
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
	f.finishLocked(sessionID, false, false)
}

func (f *Fake) ExitProcess(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishLocked(sessionID, false, true)
}

func (f *Fake) HasWindow(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.Handles[sessionID]
	return st != nil && st.window
}

func (f *Fake) finishLocked(sessionID string, process, window bool) {
	st := f.Handles[sessionID]
	if st == nil {
		return
	}
	st.process = process
	st.window = window
	if !process {
		st.pid = 0
	}
	if !process && !window {
		if !st.closed {
			close(st.wait)
			st.closed = true
		}
		delete(f.Handles, sessionID)
		return
	}
	if !st.closed {
		close(st.wait)
		st.closed = true
	}
}

func (f *Fake) HasResume(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.Handles[sessionID]
	return st != nil && (st.process || st.window)
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
	for {
		h.fake.mu.Lock()
		st := h.fake.Handles[h.id]
		var ch chan struct{}
		if st != nil {
			ch = st.wait
			if !st.process || !st.window {
				h.fake.mu.Unlock()
				return nil
			}
		}
		h.fake.mu.Unlock()
		if st == nil {
			return nil
		}
		<-ch
	}
}

func (h waitHandle) PID() int {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if st := h.fake.Handles[h.id]; st != nil && st.process {
		return st.pid
	}
	return 0
}

func (h waitHandle) WindowID() string {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	if st := h.fake.Handles[h.id]; st != nil {
		return st.windowID
	}
	return ""
}

func (h waitHandle) Close() error {
	h.fake.mu.Lock()
	block := h.fake.CloseBlock[h.id]
	delay := h.fake.CloseDelay
	h.fake.mu.Unlock()
	if block != nil {
		<-block
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	h.fake.CloseResume(h.id)
	return nil
}
