package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"grokmcp/internal/protocol"
	"net"
	"testing"
	"time"
)

func TestDisconnectReleasesPendingCalls(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); accepted <- c }()
	c, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	peer := <-accepted
	done := make(chan error, 1)
	go func() { _, err := c.StatusBar(context.Background()); done <- err }()
	sc := bufio.NewScanner(peer)
	if !sc.Scan() {
		t.Fatal("no request")
	}
	peer.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("EOF treated as success")
		}
	case <-time.After(time.Second):
		t.Fatal("pending call leaked")
	}
	count := 0
	c.pend.Range(func(any, any) bool { count++; return true })
	if count != 0 {
		t.Fatal("pending map leaked")
	}
}
func TestEventCallbackCanMakeRPC(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() { c, _ := ln.Accept(); accepted <- c }()
	c, err := Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	peer := <-accepted
	defer peer.Close()
	done := make(chan error, 1)
	c.Subscribe(func(protocol.Event) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err := c.StatusBar(ctx)
		done <- err
	})
	go func() {
		sc := bufio.NewScanner(peer)
		if sc.Scan() {
			var req Request
			_ = json.Unmarshal(sc.Bytes(), &req)
			raw, _ := json.Marshal(Response{ID: req.ID, Result: json.RawMessage(`{"db_ok":true}`)})
			_, _ = peer.Write(append(raw, '\n'))
		}
	}()
	raw, _ := json.Marshal(Response{Method: "event", Params: json.RawMessage(`{"type":"job"}`)})
	_, _ = peer.Write(append(raw, '\n'))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("callback blocked read loop")
	}
}
