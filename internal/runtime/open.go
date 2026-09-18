package runtime

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/core"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/ids"
	"grokmcp/internal/ipc"
	"grokmcp/internal/paths"
	"grokmcp/internal/store"
	"grokmcp/internal/supervisor"
	"grokmcp/internal/terminal"
)

type Options struct {
	Home     string
	Agent    agent.Agent
	Term     terminal.Launcher
	GrokPath string
}

func Open(ctx context.Context, opts Options) (core.Backend, func(), error) {
	if opts.Home != "" {
		_ = os.Setenv("GROK_SUPERVISOR_HOME", opts.Home)
	}
	lockPath, err := paths.LockPath()
	if err != nil {
		return nil, nil, err
	}
	for {
		if cl := tryDial(ctx); cl != nil {
			return cl, func() { _ = cl.Close() }, nil
		}
		lock, err := tryLockFile(lockPath)
		if err == nil {
			if cl := tryDial(ctx); cl != nil {
				_ = lock.Close()
				return cl, func() { _ = cl.Close() }, nil
			}
			return openHost(ctx, opts, lock)
		}
		select {
		case <-ctx.Done():
			return nil, nil, fmt.Errorf("ipc lock: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func openHost(ctx context.Context, opts Options, lock *os.File) (core.Backend, func(), error) {
	ln, err := listenIPC()
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	dbPath, err := paths.DBPath()
	if err != nil {
		_ = ln.Close()
		_ = lock.Close()
		return nil, nil, err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		_ = ln.Close()
		_ = lock.Close()
		return nil, nil, err
	}
	ag := opts.Agent
	if ag == nil {
		finder := grokbin.New()
		ag = agent.NewGrok(opts.GrokPath, finder)
	}
	term := opts.Term
	if term == nil {
		stt, _ := st.Settings()
		term = terminal.Exec{Template: stt.TerminalCommandTemplate, Provider: stt.TerminalProvider}
	}
	svc := supervisor.New(st, ag, term, clock.Real{}, ids.UUID{})
	svc.SetGrokPath(func() string {
		stt, _ := st.Settings()
		if stt.GrokBinaryPath != "" {
			return stt.GrokBinaryPath
		}
		if opts.GrokPath != "" {
			return opts.GrokPath
		}
		p, _ := grokbin.New().Resolve("")
		if p == "" {
			return "grok"
		}
		return p
	})
	_ = ag.EnsureLeader(ctx)
	if err := svc.Start(ctx); err != nil {
		_ = ln.Close()
		_ = st.Close()
		_ = lock.Close()
		return nil, nil, err
	}
	srv := ipc.Serve(ln, svc)
	cleanup := func() {
		_ = srv.Close()
		_ = svc.Close()
		_ = st.Close()
		_ = ln.Close()
		_ = lock.Close()
	}
	return svc, cleanup, nil
}

func tryDial(ctx context.Context) *ipc.Client {
	dctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cl, err := dialIPC(dctx)
	if err != nil {
		return nil
	}
	if _, err := cl.StatusBar(dctx); err != nil {
		_ = cl.Close()
		return nil
	}
	return cl
}

func listenIPC() (net.Listener, error) {
	p, err := paths.SocketPath()
	if err != nil {
		return nil, err
	}
	if goruntime.GOOS == "windows" {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		addr := ln.Addr().(*net.TCPAddr)
		if err := os.WriteFile(p, []byte(strconv.Itoa(addr.Port)), 0o600); err != nil {
			_ = ln.Close()
			return nil, err
		}
		return ln, nil
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	ln, err := net.Listen("unix", p)
	if err != nil {
		_ = os.Remove(p)
		return net.Listen("unix", p)
	}
	return ln, nil
}

func dialIPC(ctx context.Context) (*ipc.Client, error) {
	p, err := paths.SocketPath()
	if err != nil {
		return nil, err
	}
	if goruntime.GOOS == "windows" {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		port := strings.TrimSpace(string(b))
		return ipc.Dial(ctx, "tcp", "127.0.0.1:"+port)
	}
	return ipc.Dial(ctx, "unix", p)
}
