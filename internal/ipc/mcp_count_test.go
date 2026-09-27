package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"grokmcp/internal/core"
	"grokmcp/internal/protocol"
)

type mcpCount struct {
	core.Backend
	mu    sync.Mutex
	n     int
	hold  chan struct{}
	enter chan struct{}
}

func (m *mcpCount) Subscribe(func(protocol.Event)) func() { return func() {} }

func (m *mcpCount) SetMCPConnected(v bool) {
	if v && m.hold != nil {
		if m.enter != nil {
			select {
			case m.enter <- struct{}{}:
			default:
			}
		}
		<-m.hold
	}
	m.mu.Lock()
	if v {
		m.n++
	} else if m.n > 0 {
		m.n--
	}
	m.mu.Unlock()
}

func (m *mcpCount) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n
}

func TestSetMCPCloseDoesNotLeakCount(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	counter := &mcpCount{hold: make(chan struct{}), enter: make(chan struct{}, 1)}
	srv := Serve(ln, counter)
	defer srv.Close()
	cl, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cl.SetMCPConnectedContext(context.Background(), true) }()
	select {
	case <-counter.enter:
	case <-time.After(time.Second):
		t.Fatal("setMCP did not reach the counter")
	}
	_ = cl.Close()
	close(counter.hold)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("setMCP did not finish after close")
	}
	deadline := time.Now().Add(time.Second)
	for counter.count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := counter.count(); got != 0 {
		t.Fatalf("mcp count leaked: %d", got)
	}
}

func TestTwoMCPClientsDecrementIndependently(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	counter := &mcpCount{}
	srv := Serve(ln, counter)
	defer srv.Close()
	dial := func() *Client {
		t.Helper()
		cl, err := Dial(context.Background(), "tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := cl.SetMCPConnectedContext(context.Background(), true); err != nil {
			t.Fatal(err)
		}
		return cl
	}
	a := dial()
	b := dial()
	waitCount(t, counter, 2)
	_ = a.Close()
	waitCount(t, counter, 1)
	_ = b.Close()
	waitCount(t, counter, 0)
}

func waitCount(t *testing.T, counter *mcpCount, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for counter.count() != want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := counter.count(); got != want {
		t.Fatalf("count=%d want %d", got, want)
	}
}

func TestSetMCPConnectedContextHonorsCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		sc := bufio.NewScanner(c)
		for sc.Scan() {
			var req Request
			if json.Unmarshal(sc.Bytes(), &req) != nil {
				return
			}
			if req.Method == "setMCP" {
				time.Sleep(2 * time.Second)
			}
		}
	}()
	cl, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = cl.SetMCPConnectedContext(ctx, true)
	if time.Since(start) > time.Second {
		t.Fatalf("setMCP ignored cancellation for %s", time.Since(start))
	}
	if !IsUncertain(err) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
