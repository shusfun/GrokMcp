//go:build windows

package mcp

import (
	"fmt"
	"golang.org/x/sys/windows"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"unsafe"
)

type consoleWatch struct {
	mu     sync.Mutex
	seen   map[uintptr]string
	thread uint32
	done   chan struct{}
}
type nativeMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}

func startConsoleWatch() (*consoleWatch, error) {
	w := &consoleWatch{seen: map[uintptr]string{}, done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(w.done)
		user := windows.NewLazySystemDLL("user32.dll")
		enum := user.NewProc("EnumWindows")
		class := user.NewProc("GetClassNameW")
		visible := user.NewProc("IsWindowVisible")
		hook := user.NewProc("SetWinEventHook")
		unhook := user.NewProc("UnhookWinEvent")
		get := user.NewProc("GetMessageW")
		peek := user.NewProc("PeekMessageW")
		var msg nativeMessage
		peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0)
		w.thread = windows.GetCurrentThreadId()
		baseline := map[uintptr]bool{}
		cb := syscall.NewCallback(func(hwnd, unused uintptr) uintptr { baseline[hwnd] = true; return 1 })
		enum.Call(cb, 0)
		onShow := syscall.NewCallback(func(unused, event, hwnd, object, child, eventThread, eventTime uintptr) uintptr {
			if object != 0 || child != 0 || baseline[hwnd] {
				return 0
			}
			v, _, _ := visible.Call(hwnd)
			if v == 0 {
				return 0
			}
			var name [256]uint16
			class.Call(hwnd, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
			kind := windows.UTF16ToString(name[:])
			if kind == "ConsoleWindowClass" || kind == "CASCADIA_HOSTING_WINDOW_CLASS" {
				w.mu.Lock()
				w.seen[hwnd] = kind
				w.mu.Unlock()
			}
			return 0
		})
		h, _, err := hook.Call(0x8002, 0x8002, 0, onShow, 0, 0, 0)
		if h == 0 {
			ready <- fmt.Errorf("SetWinEventHook: %w", err)
			return
		}
		defer unhook.Call(h)
		ready <- nil
		for {
			result, _, _ := get.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if result == 0 || result == ^uintptr(0) {
				return
			}
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return w, nil
}
func (w *consoleWatch) stop() []string {
	windows.NewLazySystemDLL("user32.dll").NewProc("PostThreadMessageW").Call(uintptr(w.thread), 0x12, 0, 0)
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for hwnd, kind := range w.seen {
		out = append(out, fmt.Sprintf("%s hwnd=%x", kind, hwnd))
	}
	return out
}

func TestConsoleWatcherCanStartAndStop(t *testing.T) {
	w, err := startConsoleWatch()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.stop()
}
