//go:build !windows

package silent

import "os/exec"

func Hide(cmd *exec.Cmd) {}
