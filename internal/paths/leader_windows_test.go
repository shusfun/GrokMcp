//go:build windows

package paths

import (
	"path/filepath"
	"testing"
)

func TestPrivateLeaderAddressOverridesSharedEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GROK_SUPERVISOR_HOME", home)
	t.Setenv("GROK_LEADER_SOCKET", `C:\unrelated\leader.sock`)
	for _, tail := range [][]string{{"agent", "leader", "--no-exit-on-disconnect"}, {"agent", "--leader", "stdio"}, {"--resume", "original-id"}, {"dashboard"}} {
		args, err := GrokArgs(tail...)
		if err != nil {
			t.Fatal(err)
		}
		if args[0] != "--leader-socket" || args[1] != filepath.Join(home, "grok-leader.sock") {
			t.Fatalf("shared address leaked: %v", args)
		}
		if len(args) != len(tail)+2 {
			t.Fatal(args)
		}
	}
}
