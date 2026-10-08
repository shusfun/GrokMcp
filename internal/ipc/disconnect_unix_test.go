//go:build !windows

package ipc

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsDisconnectErrorUnix(t *testing.T) {
	cases := []error{
		syscall.EPIPE,
		syscall.ECONNRESET,
		&os.SyscallError{Syscall: "write", Err: syscall.EPIPE},
		&os.SyscallError{Syscall: "write", Err: syscall.ECONNRESET},
		fmt.Errorf("write: %w", syscall.EPIPE),
	}
	for _, err := range cases {
		if !isDisconnectError(err) {
			t.Errorf("%v not recognized as disconnect", err)
		}
		sent, classified := classifyWrite(0, err)
		if sent || !IsDisconnected(classified) || IsUncertain(classified) {
			t.Errorf("zero-byte %v => sent=%v err=%v", err, sent, classified)
		}
		sent, classified = classifyWrite(8, err)
		if !sent || !IsUncertain(classified) || IsDisconnected(classified) {
			t.Errorf("partial %v => sent=%v err=%v", err, sent, classified)
		}
	}
	if isDisconnectError(syscall.EINVAL) {
		t.Fatal("EINVAL treated as disconnect")
	}
}
