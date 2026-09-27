package paths

import (
	"runtime"
	"strings"
	"testing"
)

func TestIPCAddressIsolatedByHome(t *testing.T) {
	t.Setenv("GROK_SUPERVISOR_HOME", t.TempDir())
	a, err := IPCAddress()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_SUPERVISOR_HOME", t.TempDir())
	b, err := IPCAddress()
	if err != nil {
		t.Fatal(err)
	}
	if a == "" || a == b {
		t.Fatalf("addresses = %q %q", a, b)
	}
	if runtime.GOOS == "windows" && !strings.HasPrefix(a, `\\.\pipe\grokmcp-supervisor-`) {
		t.Fatal(a)
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(a, "supervisor.sock") {
		t.Fatal(a)
	}
}
