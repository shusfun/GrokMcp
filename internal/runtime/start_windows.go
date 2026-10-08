//go:build windows

package runtime

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup  = 0x00000200
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
)

func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup | createBreakawayFromJob,
		HideWindow:    true,
	}
}

// relaxDetach 在父作业不允许脱离时去掉 BREAKAWAY，避免后台桌面启动失败。
func relaxDetach(cmd *exec.Cmd) bool {
	if cmd == nil || cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createBreakawayFromJob == 0 {
		return false
	}
	cmd.SysProcAttr.CreationFlags &^= createBreakawayFromJob
	return true
}
