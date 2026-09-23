package terminal

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"grokmcp/internal/paths"
)

type Exec struct {
	Template string
	Provider string
	managed  *managedTerminals
	ensure   func(context.Context) error
}

func NewExec(template, provider string, ag interface{ EnsureLeader(context.Context) error }) Exec {
	return Exec{Template: template, Provider: provider, managed: newManagedTerminals(), ensure: ag.EnsureLeader}
}

func (e Exec) OpenResume(ctx context.Context, grokPath, sessionID, cwd string) (Handle, error) {
	if runtime.GOOS == "windows" && e.managed != nil {
		return e.openViewer(ctx, grokPath, sessionID, cwd)
	}

	cmdLine := ResumeCommand(grokPath, sessionID)
	if runtime.GOOS == "windows" {
		socket, err := paths.SupervisorLeaderSocket()
		if err != nil {
			return nil, err
		}
		cmdLine = shellQuote(grokPath) + " --leader-socket " + shellQuote(socket) + " --resume " + sessionID
	}
	return e.spawn(ctx, cwd, cmdLine, sessionID)
}

func (e Exec) FocusResume(ctx context.Context, sessionID string) (bool, error) {
	return e.focus(ctx, SessionTitle(sessionID))
}

func (e Exec) OpenDashboard(ctx context.Context, grokPath, cwd string) error {
	if runtime.GOOS == "windows" && e.managed != nil {
		_, err := e.openViewer(ctx, grokPath, "", cwd)
		return err
	}

	command := DashboardCommand(grokPath)
	if runtime.GOOS == "windows" {
		socket, err := paths.SupervisorLeaderSocket()
		if err != nil {
			return err
		}
		command = shellQuote(grokPath) + " --leader-socket " + shellQuote(socket) + " dashboard"
	}
	_, err := e.spawn(ctx, cwd, command, "")
	return err
}

func (e Exec) TestTemplate(ctx context.Context, template, command string) error {
	if runtime.GOOS == "windows" && e.managed != nil {
		return e.testViewerTemplate(ctx, template)
	}
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

func (h procHandle) TTY() string { return "" }

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
