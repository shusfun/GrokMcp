package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	acp "github.com/coder/acp-go-sdk"
	"grokmcp/internal/ownedprocess"
	"grokmcp/internal/paths"
)

type connectionAttempt struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}
type connectionResources struct {
	console *ownedprocess.Console
	leader  *ownedprocess.Process
	group   *ownedprocess.Group
	process *ownedprocess.Process
	streams []*os.File
	conn    *acp.ClientSideConnection
	client  *acpClient
}

func (g *Grok) SessionLoaded(sessionID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.conn != nil && g.loaded[sessionID]
}

func (r *connectionResources) close() {
	for _, f := range r.streams {
		_ = f.Close()
	}
	if r.group != nil {
		_ = r.group.Close()
	}
	if r.console != nil {
		_ = r.console.Close()
	}
}

func (g *Grok) ConnectionState() ConnectionState {
	g.mu.Lock()
	defer g.mu.Unlock()
	ready := g.conn != nil && (g.process == nil || g.process.Alive()) && (g.leader == nil || g.leader.Alive())
	return ConnectionState{LeaderRunning: ready, ACPOK: ready, Connecting: g.initializing != nil}
}

func (g *Grok) EnsureLeader(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		return errors.New("Grok 已关闭")
	}
	if g.conn != nil {
		g.mu.Unlock()
		return nil
	}
	a := g.initializing
	if a == nil {
		budget := g.connectTimeout
		if budget <= 0 {
			budget = 15 * time.Second
		}
		initCtx, cancel := context.WithTimeout(context.Background(), budget)
		a = &connectionAttempt{done: make(chan struct{}), cancel: cancel}
		g.initializing = a
		go g.initialize(initCtx, a)
	}
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.done:
		return a.err
	}
}

func (g *Grok) initialize(ctx context.Context, a *connectionAttempt) {
	g.resourceMu.Lock()
	defer g.resourceMu.Unlock()
	start := g.startConnection
	if start == nil {
		start = g.connect
	}
	var r *connectionResources
	err := ctx.Err()
	if err == nil {
		r, err = start(ctx)
	}
	if err == nil {
		err = ctx.Err()
	}
	g.mu.Lock()
	if err == nil && g.closed {
		err = errors.New("Grok 已关闭")
	}
	if err == nil {
		g.group, g.process, g.streams = r.group, r.process, r.streams
		g.leader = r.leader
		g.leaderConsole = r.console
		g.conn, g.client = r.conn, r.client
		g.loaded = map[string]bool{}
	}
	g.mu.Unlock()
	if err != nil && r != nil {
		r.close()
	}
	a.cancel()
	g.mu.Lock()
	a.err = err
	if g.initializing == a {
		g.initializing = nil
	}
	close(a.done)
	g.mu.Unlock()
	if err == nil && r.process != nil {
		go func() {
			var leaderDone <-chan struct{}
			if r.leader != nil {
				leaderDone = r.leader.Done()
			}
			select {
			case <-r.process.Done():
			case <-leaderDone:
			}
			g.resourceMu.Lock()
			g.mu.Lock()
			lost := g.process == r.process
			notify := g.disconnectFn
			if lost {
				g.conn = nil
				g.client = nil
				g.process = nil
				g.leader = nil
				g.leaderConsole = nil
				g.group = nil
				g.streams = nil
				g.loaded = nil
			}
			g.mu.Unlock()
			r.close()
			g.resourceMu.Unlock()
			if lost && notify != nil {
				notify()
			}
		}()
	}
}

func (g *Grok) SetDisconnectListener(fn func()) { g.mu.Lock(); g.disconnectFn = fn; g.mu.Unlock() }

func (g *Grok) connect(ctx context.Context) (*connectionResources, error) {
	g.mu.Lock()
	bin := g.Bin
	g.mu.Unlock()
	if bin == "" {
		var err error
		bin, err = g.Finder.Resolve("")
		if err != nil {
			return nil, err
		}
		g.mu.Lock()
		g.Bin = bin
		g.mu.Unlock()
	}
	group, err := ownedprocess.New()
	if err != nil {
		return nil, err
	}
	r := &connectionResources{group: group}
	if r.leader, r.console, err = g.startLeader(group, bin); err != nil {
		return r, err
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return r, err
	}
	r.streams = append(r.streams, stdinR, stdinW)
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return r, err
	}
	r.streams = append(r.streams, stdoutR, stdoutW)
	stderr := os.Stderr
	if _, e := stderr.Stat(); e != nil {
		stderr = nil
	}
	args, err := paths.GrokArgs("agent", "--leader", "stdio")
	if err != nil {
		return r, err
	}
	p, err := group.Start(ownedprocess.Spec{Path: bin, Args: args, Stdin: stdinR, Stdout: stdoutW, Stderr: stderr})
	if err != nil {
		return r, err
	}
	r.process = p
	_ = stdinR.Close()
	_ = stdoutW.Close()
	client := newACPClient()
	client.onUpdate = func(sid, action string, plan bool) {
		g.mu.Lock()
		if action != "" {
			g.last[sid] = action
		}
		if plan {
			g.plan[sid] = true
		}
		g.mu.Unlock()
	}
	client.onPermission = g.handlePermission
	client.onExitPlan = g.handleExitPlanMode
	r.client = client
	r.conn = acp.NewClientSideConnection(client, stdinW, stdoutR)
	_, err = r.conn.Initialize(ctx, acp.InitializeRequest{
		ProtocolVersion:    acp.ProtocolVersionNumber,
		ClientCapabilities: acp.ClientCapabilities{Fs: acp.FileSystemCapabilities{ReadTextFile: true, WriteTextFile: true}},
		ClientInfo:         &acp.Implementation{Name: "Grok Supervisor", Version: "0.1.0"},
	})
	if err != nil {
		return r, fmt.Errorf("连接专用 Grok leader 失败（不会自动重试）: %w", err)
	}
	if r.leader != nil && !r.leader.Alive() {
		return r, errors.New("专用 leader 已退出，拒绝连接其他 leader")
	}
	return r, nil
}

// Release 只回收本实例资源；会话持久 ID 由 Supervisor 保存。
func (g *Grok) Release() error {
	g.mu.Lock()
	a := g.initializing
	if a != nil {
		a.cancel()
	}
	g.mu.Unlock()
	if a != nil {
		<-a.done
	}
	g.resourceMu.Lock()
	defer g.resourceMu.Unlock()
	g.mu.Lock()
	r := &connectionResources{group: g.group, streams: g.streams, console: g.leaderConsole}
	g.group = nil
	g.streams = nil
	g.process = nil
	g.leader = nil
	g.leaderConsole = nil
	g.conn = nil
	g.client = nil
	g.loaded = nil
	g.attach = "boundary"
	g.mu.Unlock()
	r.close()
	return nil
}

func (g *Grok) Close() error { g.mu.Lock(); g.closed = true; g.mu.Unlock(); return g.Release() }
