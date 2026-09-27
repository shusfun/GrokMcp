//go:build !windows

package runtime

import (
	"context"
	"net"
	"os"
	"path/filepath"

	"grokmcp/internal/ipc"
	"grokmcp/internal/paths"
)

func IPCAddress() (string, error) { return paths.IPCAddress() }

func listenIPC() (net.Listener, error) {
	p, err := paths.IPCAddress()
	if err != nil {
		return nil, err
	}
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	ln, err := net.Listen("unix", p)
	if err != nil {
		_ = os.Remove(p)
		return net.Listen("unix", p)
	}
	return ln, nil
}

func dialIPCConn(ctx context.Context) (net.Conn, error) {
	p, err := paths.IPCAddress()
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", p)
}

func dialIPC(ctx context.Context) (*ipc.Client, error) {
	conn, err := dialIPCConn(ctx)
	if err != nil {
		return nil, err
	}
	return ipc.New(conn), nil
}
