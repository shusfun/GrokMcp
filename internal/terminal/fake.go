package terminal

import (
	"context"
	"errors"
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
	ReadyFail  bool
	NoPID      bool
}

func NewFake() *Fake {
	return &Fake{Handles: map[string]*fakeState{}, CloseBlock: map[string]chan struct{}{}}
}

func (f *Fake) OpenResume(_ context.Context, _, sessionID, cwd string) (Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Resumes = append(f.Resumes, sessionID+"|"+cwd)
	if f.ReadyFail {
		return nil, errors.New("terminal not ready")
	}
	pid := 4242
	if f.NoPID {
		pid = 0
	}
	f.closeLocked(sessionID)
	st := &fakeState{
		wait:     make(chan struct{}),
		process:  true,
		window:   true,
		pid:      pid,
		windowID: "w-" + sessionID,
	}
	f.Handles[sessionID] = st
	return waitHandle{fake: f, id: sessionID, st: st}, nil
}

func (f *Fake) FocusResume(_ context.Context, sessionID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.Handles[sessionID]
	return ok && (st.process || st.window), nil
}

func (f *Fake) ExistingResume(_ context.Context, sessionID string) (Handle, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.Handles[sessionID]
	if !ok || (!st.process && !st.window) {
		return nil, false, nil
	}
	return waitHandle{fake: f, id: sessionID, st: st}, true, nil
}

func (f *Fake) SeedWindow(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.Handles[sessionID]; ok {
		return
	}
	f.Handles[sessionID] = &fakeState{
		wait:     make(chan struct{}),
		process:  true,
		window:   true,
		pid:      4242,
		windowID: "w-" + sessionID,
	}
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
	f.closeLocked(sessionID)
}

func (f *Fake) CloseAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.Handles {
		f.closeLocked(id)
	}
}

func (f *Fake) closeLocked(sessionID string) {
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
	st   *fakeState
}

func (h waitHandle) Wait() error {
	h.fake.mu.Lock()
	st := h.st
	if st == nil {
		st = h.fake.Handles[h.id]
	}
	if st == nil || st.closed || !st.process || !st.window {
		h.fake.mu.Unlock()
		return nil
	}
	ch := st.wait
	h.fake.mu.Unlock()
	<-ch
	return nil
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

func (h waitHandle) TTY() string {
	if h.PID() == 0 && h.WindowID() == "" {
		return ""
	}
	return "/dev/ttys001"
}

func (h waitHandle) Focus() (bool, error) {
	h.fake.mu.Lock()
	defer h.fake.mu.Unlock()
	st := h.fake.Handles[h.id]
	return st != nil && (st.process || st.window), nil
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
