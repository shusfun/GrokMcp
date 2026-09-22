package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"grokmcp/internal/protocol"
)

type NewCall struct {
	Cwd      string
	Worktree bool
}

type Fake struct {
	mu                   sync.Mutex
	seq                  int
	Sessions             map[string]string
	Prompts              []string
	Cancels              []string
	Loads                []string
	NewCalls             []NewCall
	Resolves             []string
	PromptFn             func(sessionID, text string) PromptResult
	EnsureErr            error
	PromptErr            error
	NewErr               error
	LoadErr              error
	Mode                 string
	Diag                 protocol.DiagnoseResult
	PromptBlock          map[string]chan struct{}
	LoadBlock            map[string]chan struct{}
	LoadDelay            time.Duration
	PlanReadyBeforeBlock string
	planWait             map[string]chan bool
	planFn               func(string, string)
	acpOK                bool
}

func NewFake() *Fake {
	return &Fake{
		Sessions:    map[string]string{},
		PromptBlock: map[string]chan struct{}{},
		LoadBlock:   map[string]chan struct{}{},
		planWait:    map[string]chan bool{},
		Mode:        "live",
		acpOK:       true,
		Diag: protocol.DiagnoseResult{
			GrokPath: "/usr/bin/grok", GrokVersion: "1.0.34", LoggedIn: true,
			Compatible: true, AttachMode: "live", ACPOK: true,
		},
	}
}

func (f *Fake) Diagnose(context.Context) protocol.DiagnoseResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := f.Diag
	d.AttachMode = f.Mode
	d.ACPOK = f.acpOK
	return d
}

func (f *Fake) EnsureLeader(context.Context) error {
	return f.EnsureErr
}

func (f *Fake) ConnectionState() ConnectionState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return ConnectionState{LeaderRunning: f.acpOK, ACPOK: f.acpOK}
}

func (f *Fake) SessionLoaded(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.Sessions[sessionID]
	return f.acpOK && ok
}

func (f *Fake) Release() error {
	f.mu.Lock()
	f.acpOK = false
	f.mu.Unlock()
	return nil
}

func (f *Fake) AttachMode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Mode == "" {
		return "boundary"
	}
	return f.Mode
}

func (f *Fake) NewSession(_ context.Context, cwd string, worktree bool) (string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.NewCalls = append(f.NewCalls, NewCall{Cwd: cwd, Worktree: worktree})
	if f.NewErr != nil {
		return "", "", f.NewErr
	}
	f.seq++
	id := fmt.Sprintf("sess-%d", f.seq)
	if worktree {
		cwd = cwd + "-wt-" + id
	}
	f.Sessions[id] = cwd
	return id, cwd, nil
}

func (f *Fake) LoadSession(ctx context.Context, sessionID, cwd string) error {
	f.mu.Lock()
	delay := f.LoadDelay
	block := f.LoadBlock[sessionID]
	err := f.LoadErr
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-block:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Loads = append(f.Loads, sessionID+"|"+cwd)
	if err != nil {
		return err
	}
	f.Sessions[sessionID] = cwd
	return nil
}

func (f *Fake) LoadConnectedSession(ctx context.Context, sessionID, cwd string) error {
	return f.LoadSession(ctx, sessionID, cwd)
}
func (f *Fake) InvalidateSession(string) {}

func (f *Fake) Prompt(ctx context.Context, sessionID, text string) (PromptResult, error) {
	f.mu.Lock()
	f.Prompts = append(f.Prompts, text)
	block := f.PromptBlock[sessionID]
	fn := f.PromptFn
	err := f.PromptErr
	f.mu.Unlock()
	if err != nil {
		return PromptResult{}, err
	}
	if block != nil {
		f.mu.Lock()
		hook := f.planFn
		excerpt := f.PlanReadyBeforeBlock
		if excerpt != "" {
			f.PlanReadyBeforeBlock = ""
		}
		f.mu.Unlock()
		if excerpt != "" && hook != nil {
			hook(sessionID, excerpt)
		}
		select {
		case <-ctx.Done():
			return PromptResult{StopReason: "cancelled"}, ctx.Err()
		case <-block:
		}
	}
	if ctx.Err() != nil {
		return PromptResult{StopReason: "cancelled"}, ctx.Err()
	}
	if fn != nil {
		res := fn(sessionID, text)
		f.mu.Lock()
		hook := f.planFn
		f.mu.Unlock()
		if res.PlanReady && hook != nil {
			hook(sessionID, res.Text)
		}
		return res, nil
	}
	return PromptResult{
		Text:       protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"}),
		StopReason: "end_turn",
		LastAction: "Idle",
	}, nil
}

func (f *Fake) Cancel(_ context.Context, sessionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cancels = append(f.Cancels, sessionID)
	if ch, ok := f.PromptBlock[sessionID]; ok {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	if ch, ok := f.planWait[sessionID]; ok {
		select {
		case ch <- false:
		default:
		}
	}
	return nil
}

func (f *Fake) ResolvePlan(_ context.Context, sessionID string, decide protocol.PlanDecision, _ string) error {
	f.mu.Lock()
	f.Resolves = append(f.Resolves, sessionID+":"+string(decide))
	ch := f.planWait[sessionID]
	f.mu.Unlock()
	if ch == nil {
		return ErrNoPlanPermission
	}
	ch <- decide == protocol.PlanApprove
	return nil
}

func (f *Fake) SetPlanListener(fn func(string, string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.planFn = fn
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.planWait {
		select {
		case ch <- false:
		default:
		}
	}
	for _, ch := range f.PromptBlock {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	return nil
}

func (f *Fake) LoadSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Loads...)
}

func (f *Fake) PromptSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Prompts...)
}

func (f *Fake) PromptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Prompts)
}
