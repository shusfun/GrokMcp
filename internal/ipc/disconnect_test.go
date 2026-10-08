package ipc

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"grokmcp/internal/protocol"
)

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub" }

type writeScriptConn struct {
	n      int
	err    error
	closed chan struct{}
	once   sync.Once
}

func newWriteScriptConn(n int, err error) *writeScriptConn {
	return &writeScriptConn{n: n, err: err, closed: make(chan struct{})}
}

func (c *writeScriptConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *writeScriptConn) Write([]byte) (int, error) {
	select {
	case <-c.closed:
		return 0, net.ErrClosed
	default:
		return c.n, c.err
	}
}

func (c *writeScriptConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *writeScriptConn) LocalAddr() net.Addr              { return stubAddr{} }
func (c *writeScriptConn) RemoteAddr() net.Addr             { return stubAddr{} }
func (c *writeScriptConn) SetDeadline(time.Time) error      { return nil }
func (c *writeScriptConn) SetReadDeadline(time.Time) error  { return nil }
func (c *writeScriptConn) SetWriteDeadline(time.Time) error { return nil }

func TestClassifyWriteZeroByteDisconnect(t *testing.T) {
	sent, err := classifyWrite(0, net.ErrClosed)
	if sent {
		t.Fatal("zero-byte disconnect marked sent")
	}
	if !IsDisconnected(err) || IsUncertain(err) {
		t.Fatalf("err=%v disconnected=%v uncertain=%v", err, IsDisconnected(err), IsUncertain(err))
	}
}

func TestClassifyWritePartialDisconnect(t *testing.T) {
	sent, err := classifyWrite(4, net.ErrClosed)
	if !sent {
		t.Fatal("partial write marked unsent")
	}
	if !IsUncertain(err) || IsDisconnected(err) {
		t.Fatalf("err=%v disconnected=%v uncertain=%v", err, IsDisconnected(err), IsUncertain(err))
	}
}

func TestClassifyWriteFullThenDisconnect(t *testing.T) {
	sent, err := classifyWrite(32, net.ErrClosed)
	if !sent || !IsUncertain(err) || IsDisconnected(err) {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}

func TestClassifyWriteZeroByteUnknownError(t *testing.T) {
	sent, err := classifyWrite(0, errors.New("write: device error"))
	if !sent || !IsUncertain(err) || IsDisconnected(err) {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}

func TestIsDisconnectErrorNetErrClosed(t *testing.T) {
	if !isDisconnectError(net.ErrClosed) {
		t.Fatal("net.ErrClosed not recognized")
	}
	if !isDisconnectError(errors.Join(errors.New("wrap"), net.ErrClosed)) {
		t.Fatal("wrapped net.ErrClosed not recognized")
	}
	if isDisconnectError(errors.New("nope")) {
		t.Fatal("generic error treated as disconnect")
	}
}

func TestClientZeroByteDisconnect(t *testing.T) {
	cl := New(newWriteScriptConn(0, net.ErrClosed))
	t.Cleanup(func() { _ = cl.Close() })
	_, err := cl.StatusBar(context.Background())
	if !IsDisconnected(err) || IsUncertain(err) {
		t.Fatalf("err=%v disconnected=%v uncertain=%v", err, IsDisconnected(err), IsUncertain(err))
	}
}

func TestClientPartialWriteDisconnect(t *testing.T) {
	cl := New(newWriteScriptConn(3, net.ErrClosed))
	t.Cleanup(func() { _ = cl.Close() })
	_, err := cl.Dispatch(context.Background(), protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if !IsUncertain(err) || IsDisconnected(err) {
		t.Fatalf("err=%v disconnected=%v uncertain=%v", err, IsDisconnected(err), IsUncertain(err))
	}
}
