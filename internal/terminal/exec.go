package terminal

import (
	"context"
	"os/exec"
	"runtime"
	"time"
)

type Exec struct {
	Template string
	Provider string
}

func (e Exec) OpenResume(ctx context.Context, grokPath, sessionID, cwd string) (Handle, error) {
	cmdLine := ResumeCommand(grokPath, sessionID)
	return e.spawn(ctx, cwd, cmdLine, sessionID)
}

func (e Exec) FocusResume(ctx context.Context, sessionID string) (bool, error) {
	return e.focus(ctx, SessionTitle(sessionID))
}

func (e Exec) OpenDashboard(ctx context.Context, grokPath, cwd string) error {
	_, err := e.spawn(ctx, cwd, DashboardCommand(grokPath), "")
	return err
}

func (e Exec) TestTemplate(ctx context.Context, template, command string) error {
	line := Render(template, "grok", command, ".", "")
	c := exec.CommandContext(ctx, shellName(), shellFlag(), line)
	return c.Run()
}

type procHandle struct {
	cmd       *exec.Cmd
	sessionID string
	windowID  string
	grokPID   int
}

func (h procHandle) Wait() error {
	if h.sessionID != "" {
		waitResumeOrCmd(h.sessionID, h.cmd)
		return nil
	}
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Wait()
}

func (h procHandle) PID() int {
	if pid := FindResumePID(h.sessionID); pid > 0 {
		return pid
	}
	if h.grokPID > 0 && processAlive(h.grokPID) {
		return h.grokPID
	}
	if h.cmd == nil || h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func (h procHandle) WindowID() string { return h.windowID }

func (h procHandle) Close() error {
	pid := h.PID()
	if pid > 0 {
		_ = terminatePID(pid, 2*time.Second)
	}
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	if h.cmd.ProcessState != nil && h.cmd.ProcessState.Exited() {
		return nil
	}
	return h.cmd.Process.Kill()
}

func shellName() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}

func shellFlag() string {
	if runtime.GOOS == "windows" {
		return "/c"
	}
	return "-c"
}
