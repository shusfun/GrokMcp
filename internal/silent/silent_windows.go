//go:build windows

package silent

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW 让控制台子系统子进程不分配可见控制台。
// 不能和 CREATE_NEW_CONSOLE 或 DETACHED_PROCESS 同时使用。
const createNoWindow = 0x08000000

func Hide(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
