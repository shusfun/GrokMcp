package agent

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"sync"
	"time"

	"grokmcp/internal/protocol"
)

type NewCall struct {
	Cwd      string
	Worktree bool
}

type Fake struct {
	planHandler func(PlanRequest) error
	approvals   map[string]PlanRequest

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
	planWait             map[string]chan planChoice
	acpOK                bool
}

func NewFake() *Fake {
	return &Fake{
		Sessions:    map[string]string{},
		PromptBlock: map[string]chan struct{}{},
		LoadBlock:   map[string]chan struct{}{},
		planWait:    map[string]chan planChoice{},
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

func (f *Fake) Prompt(ctx context.Context, sid, text string) (PromptResult, error) {
	f.mu.Lock()
	f.Prompts = append(f.Prompts, text)
	block, fn, err, excerpt := f.PromptBlock[sid], f.PromptFn, f.PromptErr, f.PlanReadyBeforeBlock
	f.PlanReadyBeforeBlock = ""
	f.mu.Unlock()
	if err != nil {
		return PromptResult{}, err
	}
	if excerpt != "" {
		choice, err := f.awaitFakePlan(ctx, sid, excerpt, block)
		if err != nil {
			return PromptResult{}, err
		}
		if choice.decide == protocol.PlanCancel {
			return PromptResult{StopReason: "cancelled"}, nil
		}
		if choice.decide == protocol.PlanRevise {
			text = protocol.RevisePrompt(choice.notes)
		} else {
			text = protocol.ApprovePrompt(choice.notes)
		}
	} else if block != nil {
		select {
		case <-ctx.Done():
			return PromptResult{}, ctx.Err()
		case <-block:
		}
	}
	for {
		if ctx.Err() != nil {
			return PromptResult{}, ctx.Err()
		}
		res := PromptResult{Text: protocol.RenderTaskState(protocol.TaskState{State: protocol.MarkerCompleted, Summary: "done"}), StopReason: "end_turn"}
		if fn != nil {
			res = fn(sid, text)
		}
		if !res.PlanReady {
			return res, nil
		}
		choice, err := f.awaitFakePlan(ctx, sid, res.Text, nil)
		if err != nil {
			return PromptResult{}, err
		}
		if choice.decide == protocol.PlanCancel {
			return PromptResult{StopReason: "cancelled"}, nil
		}
		if choice.decide == protocol.PlanRevise {
			text = protocol.RevisePrompt(choice.notes)
		} else {
			text = protocol.ApprovePrompt(choice.notes)
		}
	}
}
func (f *Fake) awaitFakePlan(ctx context.Context, sid, content string, block <-chan struct{}) (planChoice, error) {
	p := PlanRequest{ID: uuid.NewString(), ConnectionID: "fake-connection", SessionID: sid, RequestID: RequestID(ctx), TurnID: TurnID(ctx), Content: content, SupportsNotes: true, CreatedAt: time.Now().UTC()}
	ch := make(chan planChoice, 1)
	f.mu.Lock()
	if f.approvals == nil {
		f.approvals = map[string]PlanRequest{}
	}
	f.approvals[p.ID] = p
	f.planWait[sid] = ch
	hook := f.planHandler
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		delete(f.approvals, p.ID)
		if f.planWait[sid] == ch {
			delete(f.planWait, sid)
		}
		f.mu.Unlock()
	}()
	if hook == nil {
		return planChoice{}, fmt.Errorf("no plan handler")
	}
	if err := hook(p); err != nil {
		return planChoice{}, err
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return planChoice{}, ctx.Err()
		case <-block:
		}
	}
	select {
	case <-ctx.Done():
		return planChoice{}, ctx.Err()
	case choice := <-ch:
		return choice, nil
	}
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
		case ch <- planChoice{decide: protocol.PlanCancel}:
		default:
		}
	}
	return nil
}

func (f *Fake) resolveChoice(_ context.Context, sessionID string, decide protocol.PlanDecision, notes string) error {
	f.mu.Lock()
	f.Resolves = append(f.Resolves, sessionID+":"+string(decide))
	ch := f.planWait[sessionID]
	f.mu.Unlock()
	if ch == nil {
		return ErrNoPlanPermission
	}
	select {
	case ch <- planChoice{decide: decide, notes: notes}:
	default:
		return fmt.Errorf("plan decision already submitted")
	}
	return nil
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, ch := range f.planWait {
		select {
		case ch <- planChoice{decide: protocol.PlanCancel}:
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

func (f *Fake) PlanSession(context.Context, string) error { return nil }

func (f *Fake) SetPlanHandler(fn func(PlanRequest) error) {
	f.mu.Lock()
	f.planHandler = fn
	f.mu.Unlock()
}
func (f *Fake) SetActivityHandler(func(Activity)) {}
func (f *Fake) SetDisconnectListener(func())      {}
func (f *Fake) PendingApproval(id string) (PlanRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.approvals[id]
	if !ok {
		return p, ErrNoPlanPermission
	}
	return p, nil
}
func (f *Fake) ResolveApproval(ctx context.Context, d PlanDecision) error {
	f.mu.Lock()
	p, ok := f.approvals[d.ID]
	if !ok || p.TurnID != d.TurnID || p.RequestID != d.RequestID || p.ConnectionID != d.ConnectionID || p.SessionID != d.SessionID {
		f.mu.Unlock()
		return ErrNoPlanPermission
	}
	delete(f.approvals, d.ID)
	f.mu.Unlock()
	return f.resolveChoice(ctx, p.SessionID, d.Decide, d.Notes)
}
