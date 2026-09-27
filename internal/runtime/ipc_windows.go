//go:build windows

package runtime

import (
	"context"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"grokmcp/internal/ipc"
	"grokmcp/internal/paths"
)

func IPCAddress() (string, error) { return paths.IPCAddress() }

func listenIPC() (net.Listener, error) {
	addr, err := paths.IPCAddress()
	if err != nil {
		return nil, err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	// 只允许当前用户连接。管道名按数据目录隔离，不使用临时 TCP 端口。
	ln, err := winio.ListenPipe(addr, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;" + user.User.Sid.String() + ")",
		InputBufferSize:    65536,
		OutputBufferSize:   65536,
	})
	if err != nil {
		return nil, err
	}
	if legacy, err := paths.LegacyPortFile(); err == nil {
		_ = os.Remove(legacy)
	}
	return ln, nil
}

func dialIPCConn(ctx context.Context) (net.Conn, error) {
	addr, err := paths.IPCAddress()
	if err != nil {
		return nil, err
	}
	// 不读取 ipc.port，也不回退到 localhost TCP。
	return winio.DialPipeContext(ctx, addr)
}

func dialIPC(ctx context.Context) (*ipc.Client, error) {
	conn, err := dialIPCConn(ctx)
	if err != nil {
		return nil, err
	}
	return ipc.New(conn), nil
}
