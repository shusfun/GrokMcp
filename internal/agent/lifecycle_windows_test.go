//go:build windows

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grokmcp/internal/grokbin"
)

// 子进程运行的是测试二进制，绝不调用真实 Grok 或 MCP。
func TestMain(m *testing.M) {
	if os.Getenv("GS_FAKE_ACP") == "1" && len(os.Args) > 1 && os.Args[1] == "--leader-socket" {
		fakeACPProcess()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeACPProcess() {
	dir := os.Getenv("GS_FAKE_ACP_DIR")
	log, _ := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%d.txt", os.Getpid())), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	defer log.Close()
	fmt.Fprintln(log, strings.Join(os.Args[1:], "|"))
	if strings.Contains(strings.Join(os.Args, " "), "agent leader") {
		time.Sleep(time.Hour)
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	for scan.Scan() {
		var req struct {
			ID     any             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil {
			continue
		}
		fmt.Fprintf(log, "%s %s\n", req.Method, req.Params)
		if req.ID == nil {
			continue
		}
		if req.Method == "initialize" && os.Getenv("GS_FAKE_ACP_STALL") == "1" {
			continue
		}
		var result any = map[string]any{}
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true}}
		case "session/new":
			result = map[string]any{"sessionId": "fixture-session"}
		case "session/prompt":
			result = map[string]any{"stopReason": "end_turn"}
		}
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		fmt.Fprintln(os.Stdout, string(b))
	}
}

func fixtureGrok(t *testing.T) *Grok {
	t.Helper()
	t.Setenv("GS_FAKE_ACP", "1")
	t.Setenv("GS_FAKE_ACP_DIR", t.TempDir())
	t.Setenv("GROK_SUPERVISOR_HOME", t.TempDir())
	g := NewGrok(os.Args[0], grokbin.New())
	t.Cleanup(func() { g.Close() })
	return g
}

func TestConcurrentInitializeSingleFlightAndCallerIndependentLifetime(t *testing.T) {
	g := fixtureGrok(t)
	g.connectTimeout = 700 * time.Millisecond
	var starts atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	g.startConnection = func(ctx context.Context) (*connectionResources, error) {
		starts.Add(1)
		close(entered)
		<-release
		return g.connect(ctx)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- g.EnsureLeader(context.Background()) }()
	}
	<-entered
	start := time.Now()
	if !g.ConnectionState().Connecting {
		t.Fatal("expected connecting snapshot")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("snapshot blocked")
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if starts.Load() != 1 {
		t.Fatalf("initializations=%d", starts.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := g.EnsureLeader(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(750 * time.Millisecond)
	if !g.ConnectionState().ACPOK {
		t.Fatal("handshake/caller context killed healthy client")
	}
	files, _ := os.ReadDir(os.Getenv("GS_FAKE_ACP_DIR"))
	if len(files) != 2 {
		t.Fatalf("expected one leader and one client, files=%d", len(files))
	}
}

func TestInitializeTimeoutAndCloseReapOwnedProcesses(t *testing.T) {
	for _, closeEarly := range []bool{false, true} {
		t.Run(fmt.Sprint(closeEarly), func(t *testing.T) {
			g := fixtureGrok(t)
			t.Setenv("GS_FAKE_ACP_STALL", "1")
			g.connectTimeout = 400 * time.Millisecond
			var resources *connectionResources
			g.startConnection = func(ctx context.Context) (*connectionResources, error) {
				r, err := g.connect(ctx)
				resources = r
				return r, err
			}
			done := make(chan error, 1)
			go func() { done <- g.EnsureLeader(context.Background()) }()
			if closeEarly {
				time.Sleep(120 * time.Millisecond)
				if err := g.Close(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("expected timeout/cancel")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("initialization did not terminate")
			}
			if resources == nil || resources.process == nil {
				t.Fatal("fixture never started ACP")
			}
			if resources.process.Alive() {
				t.Fatal("failed ACP still alive")
			}
			if g.ConnectionState().ACPOK || g.ConnectionState().Connecting {
				t.Fatal("failed connection published")
			}
		})
	}
}

func TestIdleReleaseReloadsOriginalSessionWithoutNewSession(t *testing.T) {
	g := fixtureGrok(t)
	id, _, err := g.NewSession(context.Background(), t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "fixture-session" {
		t.Fatal(id)
	}
	if err = g.Release(); err != nil {
		t.Fatal(err)
	}
	if g.ConnectionState().ACPOK {
		t.Fatal("release left ACP connected")
	}
	if err = g.LoadSession(context.Background(), id, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(os.Getenv("GS_FAKE_ACP_DIR"))
	var text string
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(os.Getenv("GS_FAKE_ACP_DIR"), f.Name()))
		text += string(b)
	}
	if strings.Count(text, "session/new ") != 1 || strings.Count(text, "session/load ") != 1 {
		t.Fatalf("unexpected sessions: %s", text)
	}
	if !strings.Contains(text, `"sessionId":"fixture-session"`) {
		t.Fatal("session identity changed")
	}
	if strings.Contains(text, "probe") {
		t.Fatal("startup must not create probe sessions")
	}
}

func TestLostClientNotifiesWithoutRestarting(t *testing.T) {
	g := fixtureGrok(t)
	lost := make(chan struct{}, 1)
	g.SetDisconnectListener(func() { lost <- struct{}{} })
	if err := g.EnsureLeader(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	p := g.process
	g.mu.Unlock()
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lost:
	case <-time.After(3 * time.Second):
		t.Fatal("lost connection not published")
	}
	if g.ConnectionState().ACPOK || g.ConnectionState().Connecting {
		t.Fatal("lost client reconnected")
	}
	if _, err := g.Prompt(context.Background(), "existing", "x"); err != ErrDisconnected {
		t.Fatalf("prompt must not reconnect: %v", err)
	}
	files, _ := os.ReadDir(os.Getenv("GS_FAKE_ACP_DIR"))
	if len(files) != 2 {
		t.Fatalf("unexpected retry processes: %d", len(files))
	}
}

func TestLostLeaderReapsClientWithoutRestarting(t *testing.T) {
	g := fixtureGrok(t)
	lost := make(chan struct{}, 1)
	g.SetDisconnectListener(func() { lost <- struct{}{} })
	if err := g.EnsureLeader(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.mu.Lock()
	leader, client := g.leader, g.process
	g.mu.Unlock()
	if leader == nil {
		t.Fatal("missing owned leader")
	}
	if err := leader.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-lost:
	case <-time.After(3 * time.Second):
		t.Fatal("leader exit not handled")
	}
	if client.Alive() || g.ConnectionState().ACPOK {
		t.Fatal("client survived its owned leader")
	}
	files, _ := os.ReadDir(os.Getenv("GS_FAKE_ACP_DIR"))
	if len(files) != 2 {
		t.Fatalf("unexpected automatic restarts: %d", len(files))
	}
}
