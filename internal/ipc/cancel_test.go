package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"grokmcp/internal/protocol"
)

func TestCancelledBeforeWriteDoesNotSend(t *testing.T) {
	var n atomic.Int32
	cl := holdPeer(t, func(Request) bool {
		n.Add(1)
		return false
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := cl.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if !errors.Is(err, context.Canceled) || IsUncertain(err) {
		t.Fatalf("err=%v uncertain=%v", err, IsUncertain(err))
	}
	time.Sleep(80 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatalf("cancelled context wrote %d requests", n.Load())
	}
}

func TestCancelAfterSendIsUncertainAndNotRetriedByCall(t *testing.T) {
	got := make(chan struct{})
	var n atomic.Int32
	cl := holdPeer(t, func(Request) bool {
		if n.Add(1) == 1 {
			close(got)
		}
		time.Sleep(2 * time.Second)
		return false
	})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := cl.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
		errCh <- err
	}()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("peer did not receive dispatch")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) || !IsUncertain(err) {
			t.Fatalf("err=%v uncertain=%v", err, IsUncertain(err))
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not unblock the call")
	}
	if n.Load() != 1 {
		t.Fatalf("dispatch sent %d times", n.Load())
	}
}

func TestTimeoutAfterSendIsUncertain(t *testing.T) {
	var n atomic.Int32
	cl := holdPeer(t, func(Request) bool {
		n.Add(1)
		time.Sleep(2 * time.Second)
		return true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	_, err := cl.Dispatch(ctx, protocol.DispatchRequest{Tasks: []protocol.DispatchTask{{Prompt: "x"}}})
	if !errors.Is(err, context.DeadlineExceeded) || !IsUncertain(err) {
		t.Fatalf("err=%v uncertain=%v", err, IsUncertain(err))
	}
	if n.Load() != 1 {
		t.Fatalf("dispatch sent %d times", n.Load())
	}
}

func holdPeer(t *testing.T, handle func(Request) bool) *Client {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	cl, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	peer := <-accepted
	go func() {
		defer peer.Close()
		sc := bufio.NewScanner(peer)
		for sc.Scan() {
			var req Request
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				return
			}
			if !handle(req) {
				return
			}
			raw, _ := json.Marshal(Response{ID: req.ID, Result: json.RawMessage(`{}`)})
			_, _ = peer.Write(append(raw, '\n'))
		}
	}()
	return cl
}
