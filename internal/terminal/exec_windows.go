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

func (e Exec) spawn(_ context.Context, cwd, command, sessionID string) (Handle, error) {
	title := SessionTitle(sessionID)
	if strings.TrimSpace(e.Template) != "" {
		line := Render(e.Template, "grok", command, cwd, sessionID)
		cmd := exec.Command("cmd", "/c", line)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return procHandle{cmd: cmd}, nil
	}
	inner := "title " + title + " && " + CommandInDirWindows(cwd, command)
	cmd := exec.Command("cmd", "/k", inner)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return procHandle{cmd: cmd}, nil
}

func (e Exec) focus(_ context.Context, title string) (bool, error) {
	out, err := exec.Command("wt.exe", "--window", title).CombinedOutput()
	if err == nil {
		return true, nil
	}
	_ = out
	return false, nil
}
