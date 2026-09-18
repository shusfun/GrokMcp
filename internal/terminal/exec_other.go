//go:build !darwin && !windows

package terminal

import (
	"context"
	"os/exec"
)

func (e Exec) OpenDirectory(_ context.Context, cwd string) error {
	return exec.Command("xdg-open", cwd).Start()
}

func (e Exec) spawn(_ context.Context, cwd, command, sessionID string) (Handle, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return procHandle{cmd: cmd, sessionID: sessionID}, nil
}

func (e Exec) ExistingResume(_ context.Context, sessionID string) (Handle, bool, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, false, nil
	}
	pid := FindResumePID(sessionID)
	if pid == 0 {
		return nil, false, nil
	}
	return procHandle{sessionID: sessionID, grokPID: pid}, true, nil
}

func (e Exec) focus(context.Context, string) (bool, error) {
	return false, nil
}
