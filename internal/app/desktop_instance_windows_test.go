//go:build windows

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestDesktopSecondLaunchAndMCPClients(t *testing.T) {
	// Windows 上 go test ./... 会执行本测试，包括 release.yml 的 windows job。不要用环境变量跳过。
	home := t.TempDir()
	exe := filepath.Join(t.TempDir(), "GrokMcp.exe")
	cmd := exec.Command("go", "build", "-o", exe, "-ldflags", "-H windowsgui -s -w", "./cmd/grokmcp")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOENV=./go.env")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	first := exec.Command(exe)
	first.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { killTree(first.Process.Pid) })
	if !waitVisible(uint32(first.Process.Pid), 25*time.Second) {
		t.Fatal("first desktop window did not become visible")
	}
	hideMain(uint32(first.Process.Pid))
	if waitVisible(uint32(first.Process.Pid), 300*time.Millisecond) {
		t.Fatal("window stayed visible after hide")
	}

	background := exec.Command(exe, "desktop", "--background")
	background.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	if err := background.Start(); err != nil {
		t.Fatal(err)
	}
	bgDone := make(chan error, 1)
	go func() { bgDone <- background.Wait() }()
	select {
	case err := <-bgDone:
		if err != nil {
			t.Fatalf("background desktop exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		killTree(background.Process.Pid)
		t.Fatal("background desktop did not exit")
	}
	if waitVisible(uint32(first.Process.Pid), 500*time.Millisecond) {
		t.Fatal("background relaunch activated the hidden window")
	}

	second := exec.Command(exe)
	second.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- second.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second desktop exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		killTree(second.Process.Pid)
		t.Fatal("second desktop did not exit")
	}
	if !processAlive(first.Process.Pid) {
		t.Fatal("first desktop exited")
	}
	if !waitVisible(uint32(first.Process.Pid), 5*time.Second) {
		t.Fatal("second launch did not show the existing window")
	}

	mcp := exec.Command(exe, "mcp")
	mcp.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	stdin, err := mcp.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := mcp.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		killTree(mcp.Process.Pid)
	})
	time.Sleep(2 * time.Second)
	if !processAlive(mcp.Process.Pid) {
		t.Fatal("mcp client exited; desktop single-instance lock must not apply to mcp")
	}
	if !processAlive(first.Process.Pid) {
		t.Fatal("desktop exited while mcp client was running")
	}
}

func TestMCPBackgroundThenExplicitShow(t *testing.T) {
	homeDuring := t.TempDir()
	homeAfter := t.TempDir()
	exe := filepath.Join(t.TempDir(), "GrokMcp.exe")
	build := exec.Command("go", "build", "-o", exe, "-ldflags", "-H windowsgui -s -w", "./cmd/grokmcp")
	build.Dir = filepath.Join("..", "..")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOENV=./go.env")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Run("during-yield", func(t *testing.T) {
		assertExplicitShowAfterMCP(t, exe, homeDuring, 800*time.Millisecond)
	})
	t.Run("after-yield", func(t *testing.T) {
		assertExplicitShowAfterMCP(t, exe, homeAfter, 5*time.Second)
	})
}

func assertExplicitShowAfterMCP(t *testing.T, exe, home string, wait time.Duration) {
	t.Helper()
	mcp := exec.Command(exe, "mcp")
	mcp.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	stdin, err := mcp.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := mcp.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		for _, pid := range childPIDs(uint32(mcp.Process.Pid)) {
			killTree(int(pid))
		}
		killTree(mcp.Process.Pid)
	})
	time.Sleep(wait)
	desktop := childPID(uint32(mcp.Process.Pid))
	if desktop == 0 {
		t.Fatal("mcp did not start a background desktop")
	}
	openExplicit(t, exe, home)
	if !waitVisible(desktop, 8*time.Second) {
		t.Fatal("explicit launch did not show the background desktop")
	}
	time.Sleep(1500 * time.Millisecond)
	if !visibleTitle(desktop) {
		t.Fatal("window was hidden again while the user had asked to show it")
	}
	hideMain(desktop)
	if waitVisible(desktop, 300*time.Millisecond) {
		t.Fatal("window stayed visible after hide")
	}
	openExplicit(t, exe, home)
	if !waitVisible(desktop, 8*time.Second) {
		t.Fatal("explicit launch after hide did not show the window")
	}
}

func openExplicit(t *testing.T, exe, home string) {
	t.Helper()
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "GROK_SUPERVISOR_HOME="+home)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("explicit launch exit: %v", err)
		}
	case <-time.After(15 * time.Second):
		killTree(cmd.Process.Pid)
		t.Fatal("explicit launch did not exit")
	}
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
		if entry.ParentProcessID == parent {
			out = append(out, entry.ProcessID)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out
}

func childPID(parent uint32) uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return 0
	}
	for {
		if entry.ParentProcessID == parent {
			return entry.ProcessID
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return 0
		}
	}
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// FindProcess on Windows does not check liveness. Open the process instead.
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	_ = p
	return true
}

func killTree(pid int) {
	_ = exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && processAlive(pid) {
		time.Sleep(50 * time.Millisecond)
	}
}

func waitVisible(pid uint32, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if visibleTitle(pid) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func hideMain(pid uint32) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	show := user32.NewProc("ShowWindow")
	isVisible := user32.NewProc("IsWindowVisible")
	getText := user32.NewProc("GetWindowTextW")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var id uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&id)))
		vis, _, _ := isVisible.Call(hwnd)
		if id == pid && vis != 0 && windowText(getText, hwnd) != "" {
			show.Call(hwnd, windows.SW_HIDE)
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
}

func visibleTitle(pid uint32) bool {
	user32 := windows.NewLazySystemDLL("user32.dll")
	isVisible := user32.NewProc("IsWindowVisible")
	getText := user32.NewProc("GetWindowTextW")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	found := false
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var id uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&id)))
		vis, _, _ := isVisible.Call(hwnd)
		if id == pid && vis != 0 && windowText(getText, hwnd) != "" {
			found = true
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return found
}

func windowText(getText *windows.LazyProc, hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := getText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}
