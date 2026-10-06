package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/ipc"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func TestOpenDoesNotWriteLegacyPortFile(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, cleanup, err := Open(ctx, Options{Home: home, Agent: agent.NewFake(), Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if _, err := os.Stat(filepath.Join(home, "ipc.port")); !os.IsNotExist(err) {
		t.Fatalf("legacy port file = %v", err)
	}
	if _, err := StatusBar(ctx, home); err != nil {
		t.Fatal(err)
	}
}

func StatusBar(ctx context.Context, home string) (protocol.StatusBar, error) {
	cl, cleanup, err := Connect(ctx, ConnectOptions{Home: home, DisableStart: true})
	if err != nil {
		return protocol.StatusBar{}, err
	}
	defer cleanup()
	return cl.StatusBar(ctx)
}

func TestConnectRedialsAfterHostExit(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var mu sync.Mutex
	var hosts []func()
	var starts atomic.Int32
	starter := func() error {
		starts.Add(1)
		_, cleanup, err := Open(context.Background(), Options{Home: home, Agent: agent.NewFake(), Term: terminal.NewFake()})
		if err != nil {
			return err
		}
		mu.Lock()
		hosts = append(hosts, cleanup)
		mu.Unlock()
		return nil
	}
	cl, cleanup, err := Connect(ctx, ConnectOptions{Home: home, Starter: starter, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if _, err := cl.StatusBar(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(hosts) != 1 {
		mu.Unlock()
		t.Fatalf("hosts = %d, want 1", len(hosts))
	}
	hosts[0]()
	mu.Unlock()
	if _, err := cl.StatusBar(ctx); err != nil {
		t.Fatalf("redial: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if starts.Load() != 2 || len(hosts) != 2 {
		t.Fatalf("starts=%d hosts=%d, want 2 and 2", starts.Load(), len(hosts))
	}
	for _, cleanup := range hosts[1:] {
		t.Cleanup(cleanup)
	}
}

func TestEffectCallIsNotReplayed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	first := scriptedClient(t, func(req ipc.Request) (keep bool) {
		calls.Add(1)
		return false
	})
	var dials atomic.Int32
	r := newResilient(first, func(context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return scriptedClient(t, func(ipc.Request) bool {
			calls.Add(1)
			return true
		}), nil
	})
	_, err := r.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Title: "once"}}})
	if !ipc.IsUncertain(err) {
		t.Fatalf("err = %v, want uncertain", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", calls.Load())
	}
	if dials.Load() != 0 {
		t.Fatalf("redial during uncertain effect = %d, want 0", dials.Load())
	}
}

func TestZeroByteDisconnectRedialsEffectOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := ipc.New(newWriteScriptConn(0, net.ErrClosed))
	t.Cleanup(func() { _ = first.Close() })
	var dials atomic.Int32
	var calls atomic.Int32
	r := newResilient(first, func(context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return scriptedClient(t, func(ipc.Request) bool {
			calls.Add(1)
			return true
		}), nil
	})
	_, err := r.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Title: "once"}}})
	if err != nil {
		t.Fatalf("retry dispatch: %v", err)
	}
	if dials.Load() != 1 {
		t.Fatalf("dials = %d, want 1", dials.Load())
	}
	if calls.Load() != 1 {
		t.Fatalf("dispatch calls = %d, want 1", calls.Load())
	}
}

func TestPartialWriteDisconnectDoesNotReplayEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first := ipc.New(newWriteScriptConn(4, net.ErrClosed))
	t.Cleanup(func() { _ = first.Close() })
	var dials atomic.Int32
	r := newResilient(first, func(context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return scriptedClient(t, func(ipc.Request) bool { return true }), nil
	})
	_, err := r.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Title: "once"}}})
	if !ipc.IsUncertain(err) || ipc.IsDisconnected(err) {
		t.Fatalf("err = %v, want uncertain", err)
	}
	if dials.Load() != 0 {
		t.Fatalf("redial during partial write = %d, want 0", dials.Load())
	}
}

func scriptedClient(t *testing.T, handle func(ipc.Request) bool) *ipc.Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	cl, err := ipc.Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	peer := <-accepted
	go func() {
		defer peer.Close()
		sc := bufio.NewScanner(peer)
		if !sc.Scan() {
			return
		}
		var req ipc.Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			return
		}
		if !handle(req) {
			return
		}
		raw, _ := json.Marshal(ipc.Response{ID: req.ID, Result: json.RawMessage(`{"db_ok":true}`)})
		_, _ = peer.Write(append(raw, '\n'))
		time.Sleep(2 * time.Second)
	}()
	return cl
}

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub" }

type writeScriptConn struct {
	n      int
	err    error
	closed chan struct{}
	once   sync.Once
}

func newWriteScriptConn(n int, err error) *writeScriptConn {
	return &writeScriptConn{n: n, err: err, closed: make(chan struct{})}
}

func (c *writeScriptConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *writeScriptConn) Write([]byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
		return c.n, c.err
	}
}

func (c *writeScriptConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *writeScriptConn) LocalAddr() net.Addr              { return stubAddr{} }
func (c *writeScriptConn) RemoteAddr() net.Addr             { return stubAddr{} }
func (c *writeScriptConn) SetDeadline(time.Time) error      { return nil }
func (c *writeScriptConn) SetReadDeadline(time.Time) error  { return nil }
func (c *writeScriptConn) SetWriteDeadline(time.Time) error { return nil }

func TestWaitCursorSurvivesHostRestartWithoutNewJob(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := agent.NewFake()
	a.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	var mu sync.Mutex
	var hosts []func()
	starter := func() error {
		_, cleanup, err := Open(context.Background(), Options{Home: home, Agent: a, Term: terminal.NewFake()})
		if err != nil {
			return err
		}
		mu.Lock()
		hosts = append(hosts, cleanup)
		mu.Unlock()
		return nil
	}
	if err := starter(); err != nil {
		t.Fatal(err)
	}
	cl, cleanup, err := Connect(ctx, ConnectOptions{Home: home, Starter: starter, Timeout: 8 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	res, err := cl.Dispatch(ctx, protocol.DispatchRequest{Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "once"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Jobs) != 1 {
		t.Fatalf("jobs = %+v", res.Jobs)
	}
	id := res.Jobs[0].JobID
	waitCtx, waitCancel := context.WithTimeout(ctx, 3*time.Second)
	got, err := cl.Wait(waitCtx, protocol.WaitRequest{JobIDs: []string{id}, TimeoutSec: 1})
	waitCancel()
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	hosts[0]()
	mu.Unlock()
	again, err := cl.Wait(ctx, protocol.WaitRequest{JobIDs: []string{id}, Cursors: got.Cursors, TimeoutSec: 1})
	if err != nil {
		t.Fatalf("resume wait: %v", err)
	}
	if again.Cursors[id] < got.Cursors[id] {
		t.Fatalf("cursor went backwards: %v -> %v", got.Cursors, again.Cursors)
	}
	jobs, err := cl.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].JobID != id {
		t.Fatalf("jobs after restart = %+v", jobs)
	}
	mu.Lock()
	for _, cleanup := range hosts[1:] {
		t.Cleanup(cleanup)
	}
	mu.Unlock()
}

func TestCancelledDispatchIsNotReplayed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan struct{})
	var calls atomic.Int32
	var dials atomic.Int32
	first := scriptedClient(t, func(ipc.Request) bool {
		if calls.Add(1) == 1 {
			close(received)
		}
		time.Sleep(2 * time.Second)
		return false
	})
	r := newResilient(first, func(context.Context) (*ipc.Client, error) {
		dials.Add(1)
		return scriptedClient(t, func(ipc.Request) bool {
			calls.Add(1)
			return true
		}), nil
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := r.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
		errCh <- err
	}()
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("dispatch was not sent")
	}
	cancel()
	select {
	case err := <-errCh:
		if !ipc.IsUncertain(err) || !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not return")
	}
	if calls.Load() != 1 || dials.Load() != 0 {
		t.Fatalf("calls=%d dials=%d", calls.Load(), dials.Load())
	}
}

func TestReadRetriesAfterDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var calls atomic.Int32
	first := scriptedClient(t, func(ipc.Request) bool {
		calls.Add(1)
		return false
	})
	r := newResilient(first, func(context.Context) (*ipc.Client, error) {
		return scriptedClient(t, func(req ipc.Request) bool {
			calls.Add(1)
			return true
		}), nil
	})
	bar, err := r.StatusBar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bar.DBOK {
		t.Fatalf("bar = %+v", bar)
	}
	if calls.Load() != 2 {
		t.Fatalf("status calls = %d, want 2", calls.Load())
	}
}
