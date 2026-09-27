//go:build windows

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows"
)

func TestStdioProcessColdStartAndHostRestart(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "GrokMcp.exe")
	build := exec.Command("go", "build", "-o", exe, "-ldflags", "-H windowsgui -s -w", "./cmd/grokmcp")
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOENV=./go.env")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.Command(exe, "mcp")
	cmd.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	client := sdk.NewClient(&sdk.Implementation{Name: "stdio-process", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		for _, pid := range pidsForExe(exe) {
			_ = exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(pid), 10), "/T", "/F").Run()
		}
	})
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "grok_project_list", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("cold start: %v", err)
	}
	mcpPID := uint32(cmd.Process.Pid)
	desktop := waitOtherPID(exe, mcpPID, 15*time.Second)
	if desktop == 0 {
		t.Fatal("cold start did not launch a supervisor desktop")
	}
	_ = exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(desktop), 10), "/T", "/F").Run()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "grok_project_list", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("same stdio process after host restart: %v", err)
	}
	if !processAlive(mcpPID) {
		t.Fatal("stdio process exited during host restart")
	}
}

func waitOtherPID(exe string, self uint32, d time.Duration) uint32 {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, pid := range pidsForExe(exe) {
			if pid != self {
				return pid
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

func processAlive(pid uint32) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func pidsForExe(exe string) []uint32 {
	want, err := filepath.Abs(exe)
	if err != nil {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil
	}
	var out []uint32
	for {
		if path, err := processPath(entry.ProcessID); err == nil && samePath(path, want) {
			out = append(out, entry.ProcessID)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out
}

func processPath(pid uint32) (string, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
