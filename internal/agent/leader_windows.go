//go:build windows

package agent

import (
	"grokmcp/internal/ownedprocess"
	"grokmcp/internal/paths"
)

func (g *Grok) startLeader(group *ownedprocess.Group, bin string) (*ownedprocess.Process, *ownedprocess.Console, error) {
	args, err := paths.GrokArgs("agent", "leader", "--no-exit-on-disconnect")
	if err != nil {
		return nil, nil, err
	}
	console, err := ownedprocess.NewConsole(120, 30)
	if err != nil {
		return nil, nil, err
	}
	p, err := group.Start(ownedprocess.Spec{Path: bin, Args: args, Console: console})
	return p, console, err
}
