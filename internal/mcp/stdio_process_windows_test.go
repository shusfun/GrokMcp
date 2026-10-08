//go:build windows

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	started := time.Now()
	session, err := client.Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("initialize took %s; MCP must answer before the desktop window is ready", elapsed)
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
	desktop := waitDesktopPID(exe, mcpPID, 15*time.Second)
	if desktop == 0 {
		t.Fatal("cold start did not launch a supervisor desktop")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if title := visibleTitleFor(mcpPID); title != "" {
			t.Fatalf("mcp process showed window %q", title)
		}
		if title := visibleTitleFor(desktop); title != "" {
			t.Fatalf("background desktop showed window %q", title)
		}
		if fg := foregroundPID(); fg == mcpPID || fg == desktop {
			t.Fatalf("foreground stolen by pid %d", fg)
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = exec.Command("taskkill", "/PID", strconv.FormatUint(uint64(desktop), 10), "/T", "/F").Run()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "grok_project_list", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("same stdio process after host restart: %v", err)
	}
	if !processAlive(mcpPID) {
		t.Fatal("stdio process exited during host restart")
	}
}

func foregroundPID() uint32 {
	user32 := windows.NewLazySystemDLL("user32.dll")
	fg, _, _ := user32.NewProc("GetForegroundWindow").Call()
	if fg == 0 {
		return 0
	}
	var id uint32
	user32.NewProc("GetWindowThreadProcessId").Call(fg, uintptr(unsafe.Pointer(&id)))
	return id
}

func visibleTitleFor(pid uint32) string {
	user32 := windows.NewLazySystemDLL("user32.dll")
	isVisible := user32.NewProc("IsWindowVisible")
	getText := user32.NewProc("GetWindowTextW")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	found := ""
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var id uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&id)))
		vis, _, _ := isVisible.Call(hwnd)
		if id == pid && vis != 0 {
			buf := make([]uint16, 256)
			n, _, _ := getText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			if n > 0 {
				found = windows.UTF16ToString(buf)
			}
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return found
}

func waitDesktopPID(exe string, parent uint32, d time.Duration) uint32 {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, pid := range childPIDs(parent) {
			if pid != parent {
				return pid
			}
		}
		for _, pid := range pidsForExe(exe) {
			if pid != parent {
				return pid
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return 0
}

func childPIDs(parent uint32) []uint32 {
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
		if entry.ParentProcessID == parent && entry.ProcessID != parent {
			out = append(out, entry.ProcessID)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out
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
	return strings.EqualFold(longPath(a), longPath(b))
}

func longPath(p string) string {
	p = strings.TrimPrefix(p, `\\?\`)
	src, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return filepath.Clean(p)
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(src, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || n > uint32(len(buf)) {
		return filepath.Clean(p)
	}
	return filepath.Clean(windows.UTF16ToString(buf[:n]))
}
