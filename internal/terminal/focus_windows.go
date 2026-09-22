//go:build windows

package terminal

import (
	"golang.org/x/sys/windows"
	"syscall"
	"unsafe"
)

// 聚焦只查询已存在窗口；wt --window 会在不存在时新开窗口。
func focusWindowByTitle(title string) bool {
	user32 := windows.NewLazySystemDLL("user32.dll")
	getText := user32.NewProc("GetWindowTextW")
	foreground := user32.NewProc("SetForegroundWindow")
	show := user32.NewProc("ShowWindow")
	found := false
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var buf [1024]uint16
		n, _, _ := getText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n > 0 && windows.UTF16ToString(buf[:]) == title {
			show.Call(hwnd, windows.SW_RESTORE)
			ok, _, _ := foreground.Call(hwnd)
			found = ok != 0
			return 0
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return found
}
