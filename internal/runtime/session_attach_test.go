package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"grokmcp/internal/ipc"
)

func TestAttachRecordsBackgroundDialError(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	backend, cleanup, err := Attach(ctx, ConnectOptions{Home: home, DisableStart: true, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	r := backend.(*resilient)
	deadline := time.Now().Add(2 * time.Second)
	var got error
	for time.Now().Before(deadline) {
		got = r.LastDialError()
		if got != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got == nil || !strings.Contains(got.Error(), "not running") {
		t.Fatalf("background dial error = %v", got)
	}
}

func TestDisconnectSkipsLateConnectedNotify(t *testing.T) {
	var mu sync.Mutex
	var notes []bool
	release := make(chan struct{})
	r := newResilient(nil, func(ctx context.Context) (*ipc.Client, error) {
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return recordingSetMCPClient(t, &mu, &notes), nil
	})
	if err := r.SetMCPConnectedContext(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetMCPConnectedContext(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	close(release)
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, live := range notes {
		if live {
			t.Fatalf("late connected notify after disconnect: %v", notes)
		}
	}
}

func recordingSetMCPClient(t *testing.T, mu *sync.Mutex, notes *[]bool) *ipc.Client {
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
	cl, err := ipc.Dial(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	peer := <-accepted
	go func() {
		defer peer.Close()
		sc := bufio.NewScanner(peer)
		for sc.Scan() {
			var req ipc.Request
			if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
				return
			}
			if req.Method == "setMCP" {
				var in struct {
					Live bool `json:"live"`
				}
				_ = json.Unmarshal(req.Params, &in)
				mu.Lock()
				*notes = append(*notes, in.Live)
				mu.Unlock()
			}
			raw, _ := json.Marshal(ipc.Response{ID: req.ID, Result: json.RawMessage(`{}`)})
			if _, err := peer.Write(append(raw, '\n')); err != nil {
				return
			}
		}
	}()
	return cl
}
