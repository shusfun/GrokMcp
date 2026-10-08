package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"grokmcp/internal/ipc"

	"grokmcp/internal/core"
	"grokmcp/internal/foreground"
	"grokmcp/internal/integration"
	"grokmcp/internal/paths"
)

const connectBudget = 25 * time.Second

type ConnectOptions struct {
	Home         string
	DisableStart bool
	Starter      func() error
	Timeout      time.Duration
}

func Connect(ctx context.Context, opts ConnectOptions) (core.Backend, func(), error) {
	cl, err := connectClient(ctx, opts)
	if err != nil {
		return nil, nil, err
	}
	backend, cleanup := finishClient(cl, opts)
	return backend, cleanup, nil
}

// Attach 立刻返回，并在后台拨号或启动隐藏桌面。
// MCP stdio 必须先回答 initialize，不能等窗口创建。
func Attach(ctx context.Context, opts ConnectOptions) (core.Backend, func(), error) {
	if opts.Home != "" {
		_ = os.Setenv("GROK_SUPERVISOR_HOME", opts.Home)
	}
	r := newResilient(nil, func(ctx context.Context) (*ipc.Client, error) {
		return connectClient(ctx, opts)
	})
	go func() {
		if _, err := r.ready(ctx); err != nil {
			r.noteDialError(err)
		}
	}()
	return r, func() { _ = r.Close() }, nil
}

func connectClient(ctx context.Context, opts ConnectOptions) (*ipc.Client, error) {
	if opts.Home != "" {
		_ = os.Setenv("GROK_SUPERVISOR_HOME", opts.Home)
	}
	if cl := tryDial(ctx); cl != nil {
		return cl, nil
	}

	exe, exeErr := MCPExecutable()
	if exeErr != nil {
		exe = "(unknown)"
	}

	if opts.DisableStart {
		return nil, fmt.Errorf("Grok Supervisor is not running (executable %s). Start Grok Supervisor and retry", exe)
	}

	start := opts.Starter
	if start == nil {
		start = startDesktop
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = connectBudget
	}
	deadline := time.Now().Add(timeout)
	backoff := 50 * time.Millisecond
	spawned := false
	for {
		if cl := tryDial(ctx); cl != nil {
			return cl, nil
		}
		if !spawned && !ownerLocked() {
			slock, err := tryStartLock()
			if err == nil {
				if cl := tryDial(ctx); cl != nil {
					_ = slock.Close()
					return cl, nil
				}
				if !ownerLocked() {
					if err := start(); err != nil {
						_ = slock.Close()
						return nil, fmt.Errorf("start Grok Supervisor (%s desktop): %w", exe, err)
					}
					spawned = true
					for {
						if cl := tryDial(ctx); cl != nil {
							_ = slock.Close()
							return cl, nil
						}
						if err := waitConnect(ctx, deadline, &backoff); err != nil {
							_ = slock.Close()
							return nil, connectWaitErr(exe, true, err)
						}
					}
				}
				_ = slock.Close()
			}
		}
		if err := waitConnect(ctx, deadline, &backoff); err != nil {
			return nil, connectWaitErr(exe, spawned, err)
		}
	}
}

func connectWaitErr(exe string, spawned bool, err error) error {
	var base error
	if spawned {
		base = fmt.Errorf("Grok Supervisor did not become ready after starting %s desktop: %w; open Grok Supervisor and retry", exe, err)
	} else {
		base = fmt.Errorf("Grok Supervisor did not become ready (executable %s): %w; open Grok Supervisor and retry", exe, err)
	}
	if goruntime.GOOS == "windows" && ownerLocked() {
		return fmt.Errorf("%w; named pipe is unavailable while the supervisor lock is held. If an older TCP build is still running, quit it. This version does not fall back to TCP", base)
	}
	return base
}

func finishClient(cl *ipc.Client, opts ConnectOptions) (core.Backend, func()) {
	r := newResilient(cl, func(ctx context.Context) (*ipc.Client, error) {
		return connectClient(ctx, opts)
	})
	return r, func() { _ = r.Close() }
}

func waitConnect(ctx context.Context, deadline time.Time, backoff *time.Duration) error {
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		return fmt.Errorf("timeout")
	}
	d := *backoff
	if !deadline.IsZero() {
		if remain := time.Until(deadline); remain < d {
			d = remain
		}
	}
	if d < 0 {
		return fmt.Errorf("timeout")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
	}
	if *backoff < time.Second {
		*backoff *= 2
	}
	return nil
}

func ownerLocked() bool {
	p, err := paths.LockPath()
	if err != nil {
		return false
	}
	f, err := tryLockFile(p)
	if err != nil {
		return true
	}
	_ = f.Close()
	return false
}

func tryStartLock() (*os.File, error) {
	p, err := paths.StartLockPath()
	if err != nil {
		return nil, err
	}
	return tryLockFile(p)
}

func Probe(ctx context.Context) bool {
	cl := tryDial(ctx)
	if cl == nil {
		return false
	}
	_ = cl.Close()
	return true
}

func MCPExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

func FormatMCPConfig(exe string) string {
	return formatMCPConfig(exe, goruntime.GOOS == "windows")
}

func formatMCPConfig(exe string, windows bool) string {
	return integration.FormatCLIConfig(exe, windows)
}

func tomlQuote(s string) string {
	return strconv.Quote(s)
}

func posixQuote(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"'\\$`") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func powershellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func startDesktop() error {
	exe, err := MCPExecutable()
	if err != nil {
		return fmt.Errorf("resolve supervisor executable: %w", err)
	}
	cmd := desktopCommand(exe)
	err = foreground.LockDuring(func() error { return cmd.Start() })
	if err != nil {
		retry := desktopCommand(exe)
		if !relaxDetach(retry) {
			return fmt.Errorf("start %s desktop: %w", exe, err)
		}
		if err2 := foreground.LockDuring(func() error { return retry.Start() }); err2 != nil {
			return fmt.Errorf("start %s desktop: %w", exe, err2)
		}
		cmd = retry
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func desktopCommand(exe string) *exec.Cmd {
	// nil 标准流在当前 Go 接到 NUL，不继承调用方的 MCP 管道。
	cmd := exec.Command(exe, "desktop", "--background")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachProcess(cmd)
	return cmd
}
