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

	"grokmcp/internal/core"
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
	if opts.Home != "" {
		_ = os.Setenv("GROK_SUPERVISOR_HOME", opts.Home)
	}
	if cl := tryDial(ctx); cl != nil {
		return cl, func() { _ = cl.Close() }, nil
	}

	exe, exeErr := MCPExecutable()
	if exeErr != nil {
		exe = "(unknown)"
	}

	if opts.DisableStart {
		return nil, nil, fmt.Errorf("Grok Supervisor is not running (executable %s). Start Grok Supervisor and retry", exe)
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
			return cl, func() { _ = cl.Close() }, nil
		}
		if !spawned && !ownerLocked() {
			slock, err := tryStartLock()
			if err == nil {
				if cl := tryDial(ctx); cl != nil {
					_ = slock.Close()
					return cl, func() { _ = cl.Close() }, nil
				}
				if !ownerLocked() {
					if err := start(); err != nil {
						_ = slock.Close()
						return nil, nil, fmt.Errorf("start Grok Supervisor (%s desktop): %w", exe, err)
					}
					spawned = true
					for {
						if cl := tryDial(ctx); cl != nil {
							_ = slock.Close()
							return cl, func() { _ = cl.Close() }, nil
						}
						if err := waitConnect(ctx, deadline, &backoff); err != nil {
							_ = slock.Close()
							return nil, nil, connectWaitErr(exe, true, err)
						}
					}
				}
				_ = slock.Close()
			}
		}
		if err := waitConnect(ctx, deadline, &backoff); err != nil {
			return nil, nil, connectWaitErr(exe, spawned, err)
		}
	}
}

func connectWaitErr(exe string, spawned bool, err error) error {
	if spawned {
		return fmt.Errorf("Grok Supervisor did not become ready after starting %s desktop: %w; open Grok Supervisor and retry", exe, err)
	}
	return fmt.Errorf("Grok Supervisor did not become ready (executable %s): %w; open Grok Supervisor and retry", exe, err)
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
	var b strings.Builder
	if windows {
		fmt.Fprintf(&b, "codex mcp add grok_supervisor -- %s mcp\n", powershellQuote(exe))
		b.WriteString("# PowerShell quoting; paste the TOML below into config if unsure.\n\n")
	} else {
		fmt.Fprintf(&b, "codex mcp add grok_supervisor -- %s mcp\n\n", posixQuote(exe))
	}
	fmt.Fprintf(&b, "[mcp_servers.grok_supervisor]\n")
	fmt.Fprintf(&b, "command = %s\n", tomlQuote(exe))
	fmt.Fprintf(&b, "args = [\"mcp\"]\n")
	fmt.Fprintf(&b, "startup_timeout_sec = 30\n")
	fmt.Fprintf(&b, "tool_timeout_sec = 21600\n")
	return b.String()
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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s desktop: %w", exe, err)
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}

func desktopCommand(exe string) *exec.Cmd {
	cmd := exec.Command(exe, "desktop")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	detachProcess(cmd)
	return cmd
}
