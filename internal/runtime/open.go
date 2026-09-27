package runtime

import (
	"context"
	"fmt"
	"os"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/clock"
	"grokmcp/internal/core"
	"grokmcp/internal/grokbin"
	"grokmcp/internal/ids"
	"grokmcp/internal/integration"
	"grokmcp/internal/ipc"
	"grokmcp/internal/paths"
	"grokmcp/internal/store"
	"grokmcp/internal/supervisor"
	"grokmcp/internal/terminal"
	"grokmcp/internal/trace"
)

type Options struct {
	Home        string
	Agent       agent.Agent
	Term        terminal.Launcher
	GrokPath    string
	Integration integration.Options
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
			backend, cleanup := finishDialClient(cl)
			return backend, cleanup, nil
		}
		lock, err := tryLockFile(lockPath)
		if err == nil {
			if cl := tryDial(ctx); cl != nil {
				_ = lock.Close()
				backend, cleanup := finishDialClient(cl)
				return backend, cleanup, nil
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
		term = terminal.NewExec(stt.TerminalCommandTemplate, stt.TerminalProvider, ag)
	}
	svc := supervisor.New(st, ag, term, clock.Real{}, ids.UUID{})
	if home, err := paths.AppDir(); err == nil {
		if tr, err := trace.Open(home, time.Now); err == nil {
			svc.SetTrace(tr)
		}
	}
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
	if err := svc.Start(ctx); err != nil {
		_ = ln.Close()
		_ = st.Close()
		_ = lock.Close()
		return nil, nil, err
	}
	integOpts := opts.Integration
	if integOpts.Executable == nil {
		integOpts.Executable = MCPExecutable
	}
	h := &host{
		Service: svc,
		integ:   integration.New(integOpts),
	}
	srv := ipc.Serve(ln, h)
	cleanup := func() {
		_ = srv.Close()
		_ = svc.Close()
		_ = st.Close()
		_ = ln.Close()
		_ = lock.Close()
	}
	return h, cleanup, nil
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
