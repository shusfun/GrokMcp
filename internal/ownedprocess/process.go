// Package ownedprocess 管理由当前宿主创建的进程，不接管外部进程。
package ownedprocess

import "os"

type Spec struct {
	Path                  string
	Args                  []string
	Dir                   string
	Env                   []string
	Stdin, Stdout, Stderr *os.File
	Visible               bool
	Title                 string
	Console               *Console
}

type Process struct {
	PID  int
	done chan struct{}
	err  error
	kill func() error
}

func (p *Process) Wait() error           { <-p.done; return p.err }
func (p *Process) Kill() error           { return p.kill() }
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}
