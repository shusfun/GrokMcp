//go:build !windows

package ownedprocess

import (
	"errors"
	"os/exec"
	"sync"
	"syscall"
)

type Group struct {
	mu        sync.Mutex
	closed    bool
	processes []*Process
}

func New() (*Group, error) { return &Group{}, nil }
func (g *Group) Start(spec Spec) (*Process, error) {
	if spec.Console != nil {
		return nil, errConPTY
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, errors.New("process group closed")
	}
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir, cmd.Env = spec.Dir, spec.Env
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	}
	if spec.Stderr != nil {
		cmd.Stderr = spec.Stderr
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{PID: cmd.Process.Pid, done: make(chan struct{})}
	p.kill = func() error { return syscall.Kill(-p.PID, syscall.SIGKILL) }
	go func() { p.err = cmd.Wait(); close(p.done) }()
	g.processes = append(g.processes, p)
	return p, nil
}
func (g *Group) Close() error {
	g.mu.Lock()
	g.closed = true
	ps := append([]*Process(nil), g.processes...)
	g.mu.Unlock()
	for _, p := range ps {
		if p.Alive() {
			_ = p.Kill()
		}
		_ = p.Wait()
	}
	return nil
}
