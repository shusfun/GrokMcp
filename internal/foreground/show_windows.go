//go:build windows

package foreground

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ShowExplicit 把用户要求打开的原生窗口显示出来。
// 后台创建的窗口没有 WS_VISIBLE。Wails 的 Show 使用 SW_SHOW，这次不会给它补上该样式。
func ShowExplicit(ptr unsafe.Pointer) {
	hwnd := uintptr(ptr)
	if hwnd == 0 {
		return
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	user32.NewProc("ShowWindow").Call(hwnd, uintptr(windows.SW_SHOWNORMAL))
	user32.NewProc("SetForegroundWindow").Call(hwnd)
}
