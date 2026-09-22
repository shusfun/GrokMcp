//go:build !windows

package agent

import (
	"grokmcp/internal/ownedprocess"
	"grokmcp/internal/paths"
	"os"
	"os/exec"
)

func (g *Grok) startLeader(_ *ownedprocess.Group, bin string) (*ownedprocess.Process, *ownedprocess.Console, error) {
	if st, err := os.Stat(paths.LeaderSocket()); err == nil && st.Mode()&os.ModeSocket != 0 {
		return nil, nil, nil
	}
	cmd := exec.Command(bin, "agent", "leader", "--no-exit-on-disconnect")
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	go func() { _ = cmd.Wait() }()
	return nil, nil, nil
}
