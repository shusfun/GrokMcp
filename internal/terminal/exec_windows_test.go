//go:build windows

package terminal

import (
	"testing"
)

func TestUserConsoleKeepsNewConsole(t *testing.T) {
	cmd := userConsoleCmd("cmd", "/k", "echo hi")
	if cmd.SysProcAttr == nil {
		t.Fatal("missing SysProcAttr")
	}
	const createNewConsole = 0x00000010
	const createNoWindow = 0x08000000
	flags := cmd.SysProcAttr.CreationFlags
	if flags&createNewConsole == 0 {
		t.Fatalf("flags %#x missing CREATE_NEW_CONSOLE", flags)
	}
	if flags&createNoWindow != 0 || cmd.SysProcAttr.HideWindow {
		t.Fatalf("user console must stay visible: %#v", cmd.SysProcAttr)
	}
}
