package ipc

import (
	"context"
	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
	"net"
	"testing"
	"time"
)

type blockedBackend struct {
	core.Backend
	entered, release chan struct{}
}

func (b *blockedBackend) Subscribe(func(protocol.Event)) func() { return func() {} }
func (b *blockedBackend) Dispatch(ctx context.Context, _ protocol.DispatchRequest) (protocol.DispatchResult, error) {
	close(b.entered)
	select {
	case <-ctx.Done():
		return protocol.DispatchResult{}, ctx.Err()
	case <-b.release:
		return protocol.DispatchResult{}, nil
	}
}
func (b *blockedBackend) StatusBar(context.Context) (protocol.StatusBar, error) {
	return protocol.StatusBar{DBOK: true}, nil
}

func TestStatusRespondsWhileSameConnectionDispatchIsBlocked(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	b := &blockedBackend{entered: make(chan struct{}), release: make(chan struct{})}
	srv := Serve(ln, b)
	defer srv.Close()
	client, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	done := make(chan error, 1)
	go func() { _, err := client.Dispatch(context.Background(), protocol.DispatchRequest{}); done <- err }()
	<-b.entered
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	bar, err := client.StatusBar(ctx)
	close(b.release)
	if err != nil || !bar.DBOK {
		t.Fatalf("status blocked behind dispatch: %+v %v", bar, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
