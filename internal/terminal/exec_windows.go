//go:build windows

package terminal

import (
	"context"
	"os/exec"
	"strings"
	"syscall"
)

func (e Exec) OpenDirectory(_ context.Context, cwd string) error {
	return exec.Command("explorer", cwd).Start()
}

func (e Exec) spawn(ctx context.Context, cwd, command, sessionID string) (Handle, error) {
	title := SessionTitle(sessionID)

	if strings.TrimSpace(e.Template) != "" {
		line := Render(e.Template, "grok", command, cwd, sessionID)
		cmd := exec.Command("cmd", "/c", line)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return procHandle{cmd: cmd, sessionID: sessionID}, nil
	}
	inner := "title " + title + " && " + CommandInDirWindows(cwd, command)
	cmd := userConsoleCmd("cmd", "/k", inner)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return procHandle{cmd: cmd, sessionID: sessionID}, nil
}

func userConsoleCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	return cmd
}

func (e Exec) ExistingResume(_ context.Context, sessionID string) (Handle, bool, error) {
	if e.managed != nil {
		e.managed.mu.Lock()
		defer e.managed.mu.Unlock()
		h := e.managed.handles[sessionID]
		if h != nil && h.PID() > 0 {
			return h, true, nil
		}
		return nil, false, nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, false, nil
	}
	pid := FindResumePID(sessionID)
	focused, _ := e.focus(context.Background(), SessionTitle(sessionID))
	if pid == 0 && !focused {
		return nil, false, nil
	}
	return procHandle{sessionID: sessionID, grokPID: pid}, true, nil
}

func (e Exec) focus(_ context.Context, title string) (bool, error) {
	return focusWindowByTitle(title), nil
}
