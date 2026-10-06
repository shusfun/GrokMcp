//go:build windows

package ipc

import (
	"errors"
	"syscall"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func isPlatformDisconnect(err error) bool {
	if errors.Is(err, winio.ErrFileClosed) {
		return true
	}
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) || errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED) {
		return true
	}
	var werr windows.Errno
	if errors.As(err, &werr) && isWindowsDisconnectErrno(werr) {
		return true
	}
	var serr syscall.Errno
	if errors.As(err, &serr) && isWindowsDisconnectErrno(windows.Errno(serr)) {
		return true
	}
	return false
}

func isWindowsDisconnectErrno(e windows.Errno) bool {
	switch e {
	case windows.ERROR_BROKEN_PIPE, windows.ERROR_NO_DATA, windows.ERROR_PIPE_NOT_CONNECTED:
		return true
	default:
		return false
	}
}
