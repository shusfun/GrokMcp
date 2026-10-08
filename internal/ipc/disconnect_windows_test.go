//go:build windows

package ipc

import (
	"fmt"
	"os"
	"syscall"
	"testing"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func TestIsDisconnectErrorWindows(t *testing.T) {
	cases := []error{
		windows.ERROR_BROKEN_PIPE,
		windows.ERROR_NO_DATA,
		windows.ERROR_PIPE_NOT_CONNECTED,
		syscall.Errno(windows.ERROR_BROKEN_PIPE),
		syscall.Errno(windows.ERROR_NO_DATA),
		syscall.Errno(windows.ERROR_PIPE_NOT_CONNECTED),
		&os.PathError{Op: "write", Path: `\\.\pipe\grokmcp-test`, Err: windows.ERROR_BROKEN_PIPE},
		&os.PathError{Op: "write", Path: `\\.\pipe\grokmcp-test`, Err: windows.ERROR_NO_DATA},
		fmt.Errorf("named pipe: %w", windows.ERROR_PIPE_NOT_CONNECTED),
		winio.ErrFileClosed,
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
	if isDisconnectError(windows.ERROR_ACCESS_DENIED) {
		t.Fatal("ERROR_ACCESS_DENIED treated as disconnect")
	}
}
