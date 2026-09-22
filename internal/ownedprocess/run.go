package ownedprocess

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

// CombinedOutput 也回收已经退出的工具留下的子进程。
func CombinedOutput(ctx context.Context, spec Spec) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" {
		// Unix 保持原来的命令生命周期；限制退出后的管道等待，避免向复用的 PGID 发信号。
		cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
		cmd.Dir, cmd.Env = spec.Dir, spec.Env
		if spec.Stdin != nil {
			cmd.Stdin = spec.Stdin
		}
		cmd.WaitDelay = time.Second
		return cmd.CombinedOutput()
	}
	c, err := NewConsole(120, 30)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	g, err := New()
	if err != nil {
		return nil, err
	}
	defer g.Close()
	spec.Console = c
	p, err := g.Start(spec)
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		_ = g.Close()
		_ = c.Close()
		return c.Output(), ctx.Err()
	case <-p.Done():
	}
	err = p.Wait()
	_ = g.Close()
	_ = c.Close()
	return c.Output(), err
}
