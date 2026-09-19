package runtime

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/integration"
	"grokmcp/internal/paths"
	"grokmcp/internal/protocol"
	"grokmcp/internal/terminal"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestConnectDoesNotBecomeHost(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Connect(ctx, ConnectOptions{Home: home, DisableStart: true, Timeout: 50 * time.Millisecond})
	if err == nil {
		t.Fatal("Connect succeeded without a supervisor")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error = %v, want not running", err)
	}
	if Probe(ctx) {
		t.Fatal("Connect without start left a live supervisor")
	}
}

func TestConnectDoesNotStartIfDialSucceeds(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, c1, err := Open(ctx, Options{Home: home, Agent: agent.NewFake(), Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c1)
	starts := 0
	_, c2, err := Connect(ctx, ConnectOptions{
		Home: home,
		Starter: func() error {
			starts++
			return errors.New("should not start")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c2)
	if starts != 0 {
		t.Fatalf("starter called %d times, want 0", starts)
	}
}

func TestConnectClientCloseLeavesHostJobs(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	a := agent.NewFake()
	a.PromptFn = func(string, string) agent.PromptResult {
		return agent.PromptResult{PlanReady: true, Text: "plan"}
	}
	host, hostCleanup, err := Open(ctx, Options{Home: home, Agent: a, Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hostCleanup)
	res, err := host.Dispatch(ctx, protocol.DispatchRequest{
		Cwd: t.TempDir(), Tasks: []protocol.DispatchTask{{Prompt: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Jobs[0].JobID
	cl, clCleanup, err := Connect(ctx, ConnectOptions{Home: home, DisableStart: true})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := cl.ListJobs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) == 0 {
		t.Fatal("mcp client did not see host jobs")
	}
	clCleanup()
	got, err := host.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.JobID != id {
		t.Fatalf("host job after client close = %+v", got)
	}
}

func TestConnectStartsThenDials(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	hostReady := make(chan func(), 1)
	starts := 0
	starter := func() error {
		starts++
		go func() {
			a := agent.NewFake()
			a.PromptFn = func(string, string) agent.PromptResult {
				return agent.PromptResult{PlanReady: true, Text: "plan"}
			}
			_, cleanup, err := Open(ctx, Options{Home: home, Agent: a, Term: terminal.NewFake()})
			if err != nil {
				t.Errorf("host open: %v", err)
				close(hostReady)
				return
			}
			hostReady <- cleanup
		}()
		return nil
	}
	cl, clCleanup, err := Connect(ctx, ConnectOptions{Home: home, Starter: starter, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clCleanup)
	if starts != 1 {
		t.Fatalf("starts = %d, want 1", starts)
	}
	select {
	case cleanup := <-hostReady:
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
	case <-ctx.Done():
		t.Fatal("host did not start")
	}
	if _, err := cl.StatusBar(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConnectStartFailureIncludesPath(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := Connect(ctx, ConnectOptions{
		Home: home,
		Starter: func() error {
			return errors.New("boom")
		},
		Timeout: 50 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected start error")
	}
	if !strings.Contains(err.Error(), "desktop") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want desktop path and boom", err)
	}
}

func TestDesktopCommandIsolatesStdio(t *testing.T) {
	cmd := desktopCommand("/tmp/GrokMcp")
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Fatal("desktop command must not inherit MCP stdio")
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "desktop" {
		t.Fatalf("args = %v, want [exe desktop]", cmd.Args)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("missing detach SysProcAttr")
	}
}

func TestConnectConcurrentStartsOnce(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var starts atomic.Int32
	hostReady := make(chan func(), 8)
	starter := func() error {
		starts.Add(1)
		go func() {
			a := agent.NewFake()
			a.PromptFn = func(string, string) agent.PromptResult {
				return agent.PromptResult{PlanReady: true, Text: "plan"}
			}
			_, cleanup, err := Open(ctx, Options{Home: home, Agent: a, Term: terminal.NewFake()})
			if err != nil {
				t.Errorf("host open: %v", err)
				return
			}
			select {
			case hostReady <- cleanup:
			case <-ctx.Done():
				cleanup()
			}
		}()
		return nil
	}
	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	cleanups := make([]func(), 0, n)
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cl, cleanup, err := Connect(ctx, ConnectOptions{Home: home, Starter: starter, Timeout: 8 * time.Second})
			if err != nil {
				errCh <- err
				return
			}
			mu.Lock()
			cleanups = append(cleanups, cleanup)
			mu.Unlock()
			if _, err := cl.StatusBar(ctx); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for _, cleanup := range cleanups {
		t.Cleanup(cleanup)
	}
	select {
	case cleanup := <-hostReady:
		if cleanup != nil {
			t.Cleanup(cleanup)
		}
	case <-ctx.Done():
		t.Fatal("host did not start")
	}
	for err := range errCh {
		t.Errorf("connect: %v", err)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("starter called %d times, want 1", got)
	}
}

func TestConnectDoesNotStartWhenOwnerLockHeldWithoutIPC(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = os.Setenv("GROK_SUPERVISOR_HOME", home)
	lp, err := paths.LockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := tryLockFile(lp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	var starts atomic.Int32
	_, _, err = Connect(ctx, ConnectOptions{
		Home: home,
		Starter: func() error {
			starts.Add(1)
			return nil
		},
		Timeout: 300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected wait timeout while owner lock is held")
	}
	if starts.Load() != 0 {
		t.Fatalf("starter called %d times, want 0", starts.Load())
	}
}

func TestFormatMCPConfigUnixPath(t *testing.T) {
	exe := `/Applications/Grok Supervisor.app/Contents/MacOS/GrokMcp`
	got := formatMCPConfig(exe, false)
	cmd, toml, ok := splitMCPConfig(got)
	if !ok {
		t.Fatalf("missing toml block:\n%s", got)
	}
	if !strings.Contains(cmd, `codex mcp add grok_supervisor -- "`+exe+`" mcp`) {
		t.Fatalf("missing posix mcp add:\n%s", cmd)
	}
	if strings.Contains(cmd, `\\`) {
		t.Fatalf("posix add command escaped backslashes:\n%s", cmd)
	}
	if !strings.Contains(toml, `command = `+tomlQuote(exe)) {
		t.Fatalf("missing toml command:\n%s", toml)
	}
	if !strings.Contains(toml, `args = ["mcp"]`) {
		t.Fatalf("missing args:\n%s", toml)
	}
}

func TestFormatMCPConfigWindowsPath(t *testing.T) {
	exe := `C:\Users\foo\AppData\Local\Grok Supervisor\GrokMcp.exe`
	got := formatMCPConfig(exe, true)
	cmd, toml, ok := splitMCPConfig(got)
	if !ok {
		t.Fatalf("missing toml block:\n%s", got)
	}
	if strings.Contains(cmd, `\\`) {
		t.Fatalf("windows add command used TOML/Go backslash escapes:\n%s", cmd)
	}
	if !strings.Contains(cmd, powershellQuote(exe)) {
		t.Fatalf("missing powershell-quoted path:\n%s", cmd)
	}
	if !strings.Contains(cmd, exe) {
		t.Fatalf("add command lost windows path:\n%s", cmd)
	}
	wantTOML := strconv.Quote(exe)
	if !strings.Contains(wantTOML, `\\`) {
		t.Fatal("test expected TOML to escape backslashes")
	}
	if !strings.Contains(toml, `command = `+wantTOML) {
		t.Fatalf("toml command should use TOML escapes:\n%s", toml)
	}
}

func TestMCPConfigIPCRoundtrip(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	opened := ""
	host, hostCleanup, err := Open(ctx, Options{
		Home:  home,
		Agent: agent.NewFake(),
		Term:  terminal.NewFake(),
		Integration: integration.Options{
			CCSwitchDB: home + "/cc-switch.db",
			LookPath:   func(string) (string, error) { return "", errors.New("no codex") },
			OpenURL: func(_ context.Context, u string) error {
				opened = u
				return nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(hostCleanup)
	bundle, err := host.MCPConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.ServerID != protocol.MCPServerID || bundle.DeepLink == "" {
		t.Fatalf("%#v", bundle)
	}
	st, err := host.MCPStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Generated.JSON == "" || st.CCSwitch.Registered {
		t.Fatalf("status %#v", st.CCSwitch)
	}
	res, err := host.OpenCCSwitchMCPImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != protocol.MCPActionPendingUser || opened == "" || res.LiveEffective {
		t.Fatalf("import %#v opened=%q", res, opened)
	}
	cl, clCleanup, err := Connect(ctx, ConnectOptions{Home: home, DisableStart: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(clCleanup)
	got, err := cl.MCPConfig(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerID != bundle.ServerID || got.DeepLink == "" {
		t.Fatalf("ipc config %#v", got)
	}
	st2, err := cl.MCPStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Generated.ServerID != protocol.MCPServerID {
		t.Fatalf("ipc status %#v", st2)
	}
}

func TestPowershellQuoteDoublesSingleQuotes(t *testing.T) {
	exe := `C:\Users\o'brien\GrokMcp.exe`
	if got, want := powershellQuote(exe), `'C:\Users\o''brien\GrokMcp.exe'`; got != want {
		t.Fatalf("powershellQuote = %q, want %q", got, want)
	}
}

func splitMCPConfig(got string) (cmd, toml string, ok bool) {
	i := strings.Index(got, "[mcp_servers.grok_supervisor]")
	if i < 0 {
		return got, "", false
	}
	return strings.TrimSpace(got[:i]), got[i:], true
}
