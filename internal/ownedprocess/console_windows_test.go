//go:build windows

package ownedprocess

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestPlainConsoleHelper(t *testing.T) {
	dir := os.Getenv("GS_CONPTY_FIXTURE")
	if dir == "" {
		return
	}
	if os.Getenv("GS_CONPTY_CHILD") == "1" {
		hwnd, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		class := windowClass(hwnd)
		_ = os.WriteFile(filepath.Join(dir, "child.txt"), []byte(strconv.Itoa(os.Getpid())+" "+class), 0600)
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
	cmd := exec.Command("cmd", "/c", "node", filepath.Join(dir, "child.cjs"))
	// 刻意不给 child 设置 windowsHide/CREATE_NO_WINDOW，验证控制台继承。
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func windowClass(hwnd uintptr) string {
	var text [256]uint16
	windows.NewLazySystemDLL("user32.dll").NewProc("GetClassNameW").Call(hwnd, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)))
	return windows.UTF16ToString(text[:])
}

func visibleTerminals() map[uintptr]bool {
	result := map[uintptr]bool{}
	visible := windows.NewLazySystemDLL("user32.dll").NewProc("IsWindowVisible")
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		class := windowClass(hwnd)
		if class == "ConsoleWindowClass" || class == "CASCADIA_HOSTING_WINDOW_CLASS" {
			v, _, _ := visible.Call(hwnd)
			if v != 0 {
				result[hwnd] = true
			}
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return result
}

func TestConPTYDescendantsDoNotCreateVisibleTerminal(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatal("Node fixture runtime required:", err)
	}
	dir := t.TempDir()
	script := `const {spawn}=require('node:child_process');const c=spawn(process.env.GS_HELPER_EXE,['-test.run=^TestPlainConsoleHelper$'],{env:{...process.env,GS_CONPTY_CHILD:'1'},stdio:'inherit'});c.on('exit',code=>process.exit(code??1));`
	if err := os.WriteFile(filepath.Join(dir, "child.cjs"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	before := visibleTerminals()
	c, err := NewConsole(100, 25)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	g, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	p, err := g.Start(Spec{Path: os.Args[0], Args: []string{"-test.run=^TestPlainConsoleHelper$"}, Console: c, Env: append(os.Environ(), "GS_CONPTY_FIXTURE="+dir, "GS_HELPER_EXE="+os.Args[0])})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.Done():
			if err := p.Wait(); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(dir, "child.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if len(strings.Fields(string(b))) != 2 {
				t.Fatalf("child escaped ConPTY: %s", b)
			}
			for hwnd := range visibleTerminals() {
				if !before[hwnd] {
					t.Fatalf("new visible terminal %#x", hwnd)
				}
			}
			return
		case <-tick.C:
			for hwnd := range visibleTerminals() {
				if !before[hwnd] {
					t.Fatalf("new visible terminal %#x", hwnd)
				}
			}
		case <-deadline.C:
			t.Fatal("fixture did not exit")
		}
	}
}
