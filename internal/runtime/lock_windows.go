//go:build windows

package runtime

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	modkernel32    = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx = modkernel32.NewProc("LockFileEx")
)

const (
	lockfileExclusiveLock   = 0x0002
	lockfileFailImmediately = 0x0001
)

func tryLockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var ov syscall.Overlapped
	r1, _, e1 := procLockFileEx.Call(f.Fd(), uintptr(lockfileExclusiveLock|lockfileFailImmediately), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if r1 == 0 {
		_ = f.Close()
		if e1 != syscall.Errno(0) {
			return nil, e1
		}
		return nil, syscall.EWOULDBLOCK
	}
	return f, nil
}
