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

// YieldIfCurrent 在本进程已成前台、且用户还没要求显示时，把前台交回其他窗口。
// 别人已经是前台时直接返回。找不到可交回的窗口时，只隐藏自己的前台窗口。
func YieldIfCurrent() {
	if !shouldYieldOwnForeground(userShown()) {
		return
	}
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
	target := visibleWindowOf(parentPID(self))
	if target == 0 {
		target = nextVisibleWindow(fg, self)
	}
	if !shouldYieldOwnForeground(userShown()) {
		return
	}
	if target != 0 {
		setFG.Call(target)
		return
	}
	if shouldHideOwnForeground(userShown(), false) {
		user32.NewProc("ShowWindow").Call(fg, uintptr(windows.SW_HIDE))
	}
}

// StartYield 在后台启动期间交回前台。重复调用不会再开一条循环。
// 用户显式打开时必须先 StopForUserShow，否则循环会把刚显示的窗口藏起来。
func StartYield(d time.Duration) {
	yieldState.mu.Lock()
	if yieldState.userShown || yieldState.stop != nil {
		yieldState.mu.Unlock()
		return
	}
	ch := make(chan struct{})
	yieldState.stop = ch
	yieldState.mu.Unlock()
	go func() {
		deadline := time.Now().Add(d)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ch:
				return
			case <-ticker.C:
				if time.Now().After(deadline) {
					return
				}
				YieldIfCurrent()
			}
		}
	}()
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
