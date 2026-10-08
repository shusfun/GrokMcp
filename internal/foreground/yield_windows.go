//go:build windows

package foreground

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	gwHwndNext = 2
	lsfwLock   = 1
	lsfwUnlock = 2
)

// YieldIfCurrent 在本进程已成前台时，把前台交回父进程或上一扇可见窗口。
// 后台 MCP 和隐藏桌面用它避免抢走聊天窗口。
func YieldIfCurrent() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	getFG := user32.NewProc("GetForegroundWindow")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	setFG := user32.NewProc("SetForegroundWindow")
	fg, _, _ := getFG.Call()
	if fg == 0 {
		return
	}
	var owner uint32
	getPID.Call(fg, uintptr(unsafe.Pointer(&owner)))
	self := windows.GetCurrentProcessId()
	if owner != self {
		return
	}
	if hwnd := visibleWindowOf(parentPID(self)); hwnd != 0 {
		setFG.Call(hwnd)
		return
	}
	if hwnd := nextVisibleWindow(fg, self); hwnd != 0 {
		setFG.Call(hwnd)
		return
	}
	user32.NewProc("ShowWindow").Call(fg, uintptr(windows.SW_HIDE))
}

// YieldLoop 在窗口创建期间反复交回前台。显式打开窗口前应停止调用。
func YieldLoop(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		YieldIfCurrent()
		time.Sleep(50 * time.Millisecond)
	}
}

// LockDuring 在启动子进程期间禁止新进程调用 SetForegroundWindow。
func LockDuring(fn func() error) error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	lock := user32.NewProc("LockSetForegroundWindow")
	lock.Call(lsfwLock)
	err := fn()
	lock.Call(lsfwUnlock)
	return err
}

func parentPID(pid uint32) uint32 {
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
		if entry.ProcessID == pid {
			return entry.ParentProcessID
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return 0
		}
	}
}

func visibleWindowOf(pid uint32) uintptr {
	if pid == 0 {
		return 0
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	isVisible := user32.NewProc("IsWindowVisible")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	var found uintptr
	callback := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var id uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&id)))
		vis, _, _ := isVisible.Call(hwnd)
		if id == pid && vis != 0 && found == 0 {
			found = hwnd
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return found
}

func nextVisibleWindow(start uintptr, skipPID uint32) uintptr {
	user32 := windows.NewLazySystemDLL("user32.dll")
	getNext := user32.NewProc("GetWindow")
	isVisible := user32.NewProc("IsWindowVisible")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	hwnd := start
	for i := 0; i < 32; i++ {
		next, _, _ := getNext.Call(hwnd, gwHwndNext)
		if next == 0 {
			return 0
		}
		vis, _, _ := isVisible.Call(next)
		var id uint32
		getPID.Call(next, uintptr(unsafe.Pointer(&id)))
		if vis != 0 && id != 0 && id != skipPID {
			return next
		}
		hwnd = next
	}
	return 0
}
