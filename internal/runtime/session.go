package runtime

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"grokmcp/internal/core"
	"grokmcp/internal/ipc"
	"grokmcp/internal/protocol"
)

// resilient 在同一个 MCP stdio 进程内重拨 Supervisor。
// Codex 已经关闭的 stdio transport 无法在本进程内修复，那是宿主边界。
type resilient struct {
	mu          sync.Mutex
	cl          *ipc.Client
	dial        func(context.Context) (*ipc.Client, error)
	subs        map[int]func(protocol.Event)
	unsubs      map[int]func()
	seq         int
	mcpLive     bool
	mcpGen      uint64
	lastDialErr error
	closed      bool
	dialing     chan struct{}
}

func newResilient(cl *ipc.Client, dial func(context.Context) (*ipc.Client, error)) *resilient {
	r := &resilient{cl: cl, dial: dial, subs: map[int]func(protocol.Event){}}
	r.unsubs = make(map[int]func())
	return r
}

func finishDialClient(cl *ipc.Client) (core.Backend, func()) {
	r := newResilient(cl, func(ctx context.Context) (*ipc.Client, error) {
		if c := tryDial(ctx); c != nil {
			return c, nil
		}
		return nil, ipc.ErrDisconnected
	})
	return r, func() { _ = r.Close() }
}

func (r *resilient) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.cl == nil {
		return nil
	}
	err := r.cl.Close()
	r.cl = nil
	return err
}

func (r *resilient) ready(ctx context.Context) (*ipc.Client, error) {
	for {
		r.mu.Lock()
		if r.closed {
			r.mu.Unlock()
			return nil, ipc.ErrDisconnected
		}
		if r.cl != nil && r.cl.Alive() {
			cl := r.cl
			r.mu.Unlock()
			return cl, nil
		}
		if r.dialing != nil {
			ch := r.dialing
			r.mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		ch := make(chan struct{})
		r.dialing = ch
		dial := r.dial
		old := r.cl
		r.cl = nil
		r.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
		cl, err := dial(ctx)
		r.mu.Lock()
		live := false
		if err == nil && !r.closed {
			r.adoptLocked(cl)
			live = r.mcpLive
		} else if cl != nil {
			_ = cl.Close()
			cl = nil
		}
		r.dialing = nil
		close(ch)
		r.mu.Unlock()
		if err != nil {
			return nil, err
		}
		if live && cl != nil {
			if notifyErr := r.notifyConnectedIfCurrent(ctx, cl); notifyErr != nil && ctx.Err() != nil {
				return nil, notifyErr
			}
		}
		if cl == nil {
			return nil, ipc.ErrDisconnected
		}
		return cl, nil
	}
}

func (r *resilient) adoptLocked(cl *ipc.Client) {
	r.cl = cl
	for id, fn := range r.subs {
		r.unsubs[id] = cl.Subscribe(fn)
	}
}

func (r *resilient) invalidate(cl *ipc.Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cl == cl {
		_ = cl.Close()
		r.cl = nil
	}
}

func callClient[T any](r *resilient, ctx context.Context, effect bool, fn func(*ipc.Client) (T, error)) (T, error) {
	var zero T
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		cl, err := r.ready(ctx)
		if err != nil {
			return zero, err
		}
		v, err := fn(cl)
		if err == nil {
			return v, err
		}
		// 已写出但未确认的取消/超时必须保持不确定，并且不能重放写请求。
		if ipc.IsUncertain(err) {
			r.invalidate(cl)
			if effect || ctx.Err() != nil {
				return zero, err
			}
			last = err
			continue
		}
		if ctx.Err() != nil {
			return zero, err
		}
		if ipc.IsDisconnected(err) {
			r.invalidate(cl)
			last = err
			continue
		}
		return v, err
	}
	return zero, last
}

func (r *resilient) Subscribe(fn func(protocol.Event)) func() {
	r.mu.Lock()
	r.seq++
	id := r.seq
	r.subs[id] = fn
	if r.cl != nil && r.cl.Alive() {
		r.unsubs[id] = r.cl.Subscribe(fn)
	}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.subs, id)
		unsub := r.unsubs[id]
		delete(r.unsubs, id)
		r.mu.Unlock()
		if unsub != nil {
			unsub()
		}
	}
}

func (r *resilient) SetMCPConnected(v bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.SetMCPConnectedContext(ctx, v)
}

func (r *resilient) SetMCPConnectedContext(ctx context.Context, v bool) error {
	r.mu.Lock()
	r.mcpLive = v
	r.mcpGen++
	cl := r.cl
	alive := cl != nil && cl.Alive()
	r.mu.Unlock()
	if alive {
		return r.notifyConnectedIfCurrent(ctx, cl)
	}
	if !v {
		return nil
	}
	// 拨号完成后再按当时的世代通知。断开会推进世代，避免迟到的已连接通知。
	go func() {
		cl, err := r.ready(ctx)
		if err != nil || cl == nil {
			if err != nil {
				r.noteDialError(err)
			}
			return
		}
		if err := r.notifyConnectedIfCurrent(ctx, cl); err != nil {
			r.noteDialError(err)
		}
	}()
	return nil
}

func (r *resilient) notifyConnectedIfCurrent(ctx context.Context, cl *ipc.Client) error {
	r.mu.Lock()
	if r.closed || r.cl != cl || !r.mcpLive {
		r.mu.Unlock()
		return nil
	}
	gen := r.mcpGen
	r.mu.Unlock()
	r.mu.Lock()
	if r.closed || r.cl != cl || !r.mcpLive || r.mcpGen != gen {
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()
	return cl.SetMCPConnectedContext(ctx, true)
}

func (r *resilient) noteDialError(err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	r.lastDialErr = err
	r.mu.Unlock()
	fmt.Fprintf(os.Stderr, "grok supervisor background dial: %v\n", err)
}

// LastDialError 返回最近一次被后台路径观察到的拨号或连接通知错误。
func (r *resilient) LastDialError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastDialErr
}

func (r *resilient) Dispatch(ctx context.Context, req protocol.DispatchRequest) (protocol.DispatchResult, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.DispatchResult, error) { return c.Dispatch(ctx, req) })
}
func (r *resilient) Wait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResult, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.WaitResult, error) { return c.Wait(ctx, req) })
}
func (r *resilient) PlanDecide(ctx context.Context, req protocol.PlanDecideRequest) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.PlanDecide(ctx, req) })
}
func (r *resilient) Followup(ctx context.Context, req protocol.FollowupRequest) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.Followup(ctx, req) })
}
func (r *resilient) CancelTurn(ctx context.Context, jobID string, expectedTurnID ...string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.CancelTurn(ctx, jobID, expectedTurnID...) })
}
func (r *resilient) CancelRequest(ctx context.Context, jobID, requestID, turnID string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.CancelRequest(ctx, jobID, requestID, turnID) })
}
func (r *resilient) SetView(ctx context.Context, req protocol.SetViewRequest) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.SetView(ctx, req) })
}
func (r *resilient) Status(ctx context.Context, jobID string, options ...protocol.ResultQuery) (protocol.Job, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.Job, error) { return c.Status(ctx, jobID, options...) })
}
func (r *resilient) ListJobs(ctx context.Context) ([]protocol.Job, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) ([]protocol.Job, error) { return c.ListJobs(ctx) })
}
func (r *resilient) ListJobsPage(ctx context.Context, q protocol.ListJobsQuery) (protocol.JobPage, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.JobPage, error) { return c.ListJobsPage(ctx, q) })
}
func (r *resilient) ImportProject(ctx context.Context, path string, installSkill bool) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.ImportProject(ctx, path, installSkill) })
}
func (r *resilient) ListProjects(ctx context.Context) ([]protocol.Project, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) ([]protocol.Project, error) { return c.ListProjects(ctx) })
}
func (r *resilient) GetProject(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.Project, error) { return c.GetProject(ctx, projectID) })
}
func (r *resilient) RemoveProject(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.RemoveProject(ctx, projectID) })
}
func (r *resilient) SkillStatus(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.Project, error) { return c.SkillStatus(ctx, projectID) })
}
func (r *resilient) SkillInstall(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.SkillInstall(ctx, projectID) })
}
func (r *resilient) SkillUpdate(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.SkillUpdate(ctx, projectID) })
}
func (r *resilient) SkillRemove(ctx context.Context, projectID string) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.SkillRemove(ctx, projectID) })
}
func (r *resilient) GeneratePrompt(ctx context.Context, req protocol.GeneratePromptRequest) (protocol.PromptResult, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.PromptResult, error) { return c.GeneratePrompt(ctx, req) })
}
func (r *resilient) SavePrompt(ctx context.Context, req protocol.SavePromptRequest) (protocol.Project, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Project, error) { return c.SavePrompt(ctx, req) })
}
func (r *resilient) OpenProjectDir(ctx context.Context, projectID string) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) {
		return struct{}{}, c.OpenProjectDir(ctx, projectID)
	})
	return err
}
func (r *resilient) ArchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.ArchiveJob(ctx, jobID) })
}
func (r *resilient) UnarchiveJob(ctx context.Context, jobID string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.UnarchiveJob(ctx, jobID) })
}
func (r *resilient) DeleteJob(ctx context.Context, jobID string) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) { return struct{}{}, c.DeleteJob(ctx, jobID) })
	return err
}
func (r *resilient) OpenTerminal(ctx context.Context, req protocol.OpenTerminalRequest) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) { return struct{}{}, c.OpenTerminal(ctx, req) })
	return err
}
func (r *resilient) OpenProject(ctx context.Context, jobID string) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) { return struct{}{}, c.OpenProject(ctx, jobID) })
	return err
}
func (r *resilient) Continue(ctx context.Context, jobID string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.Continue(ctx, jobID) })
}
func (r *resilient) DetachView(ctx context.Context, jobID string) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.DetachView(ctx, jobID) })
}
func (r *resilient) Settings(ctx context.Context) (protocol.Settings, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.Settings, error) { return c.Settings(ctx) })
}
func (r *resilient) SaveSettings(ctx context.Context, s protocol.Settings) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) { return struct{}{}, c.SaveSettings(ctx, s) })
	return err
}
func (r *resilient) Diagnose(ctx context.Context) (protocol.DiagnoseResult, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.DiagnoseResult, error) { return c.Diagnose(ctx) })
}
func (r *resilient) MCPConfig(ctx context.Context) (protocol.MCPConfigBundle, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.MCPConfigBundle, error) { return c.MCPConfig(ctx) })
}
func (r *resilient) MCPStatus(ctx context.Context) (protocol.MCPInstallStatus, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.MCPInstallStatus, error) { return c.MCPStatus(ctx) })
}
func (r *resilient) OpenCCSwitchMCPImport(ctx context.Context) (protocol.MCPApplyResult, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.MCPApplyResult, error) { return c.OpenCCSwitchMCPImport(ctx) })
}
func (r *resilient) OpenCCSwitchApp(ctx context.Context) (protocol.MCPApplyResult, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.MCPApplyResult, error) { return c.OpenCCSwitchApp(ctx) })
}
func (r *resilient) AddMCPToCodex(ctx context.Context) (protocol.MCPApplyResult, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.MCPApplyResult, error) { return c.AddMCPToCodex(ctx) })
}
func (r *resilient) StatusBar(ctx context.Context) (protocol.StatusBar, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.StatusBar, error) { return c.StatusBar(ctx) })
}
func (r *resilient) Events(ctx context.Context, jobID string) ([]protocol.BoundaryEvent, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) ([]protocol.BoundaryEvent, error) { return c.Events(ctx, jobID) })
}
func (r *resilient) TestTerminal(ctx context.Context, template string) error {
	_, err := callClient(r, ctx, true, func(c *ipc.Client) (struct{}, error) { return struct{}{}, c.TestTerminal(ctx, template) })
	return err
}
func (r *resilient) DebugSet(ctx context.Context, req protocol.DebugSetRequest) (protocol.Job, error) {
	return callClient(r, ctx, true, func(c *ipc.Client) (protocol.Job, error) { return c.DebugSet(ctx, req) })
}
func (r *resilient) DebugSnapshot(ctx context.Context, req protocol.DebugSnapshotRequest) (protocol.DebugSnapshot, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.DebugSnapshot, error) { return c.DebugSnapshot(ctx, req) })
}
func (r *resilient) DebugWait(ctx context.Context, req protocol.DebugWaitRequest) (protocol.DebugSnapshot, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.DebugSnapshot, error) { return c.DebugWait(ctx, req) })
}
func (r *resilient) DebugExport(ctx context.Context, jobID string) (protocol.DebugExportResult, error) {
	return callClient(r, ctx, false, func(c *ipc.Client) (protocol.DebugExportResult, error) { return c.DebugExport(ctx, jobID) })
}
