//go:build !windows

package ipc

import (
	"errors"
	"syscall"
)

func isPlatformDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET)
}
