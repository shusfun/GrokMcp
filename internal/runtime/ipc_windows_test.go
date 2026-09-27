//go:build windows

package runtime

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"grokmcp/internal/agent"
	"grokmcp/internal/paths"
	"grokmcp/internal/terminal"
)

func TestLegacyTCPPortIsNotUsed(t *testing.T) {
	home := testHome(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	port := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(filepath.Join(home, "ipc.port"), []byte(strconv.Itoa(port)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, cleanup, err := Open(ctx, Options{Home: home, Agent: agent.NewFake(), Term: terminal.NewFake()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if _, err := os.Stat(filepath.Join(home, "ipc.port")); !os.IsNotExist(err) {
		t.Fatalf("legacy port file kept: %v", err)
	}
	backend, cleanupClient, err := Connect(ctx, ConnectOptions{Home: home, DisableStart: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanupClient)
	bar, err := backend.StatusBar(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bar.DBOK {
		t.Fatalf("dialed something other than the named pipe: %+v", bar)
	}
}

func TestOldTCPLockDoesNotFallback(t *testing.T) {
	home := testHome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = os.Setenv("GROK_SUPERVISOR_HOME", home)
	lp, err := paths.LockPath()
	if err != nil {
		t.Fatal(err)
	}
	lock, err := tryLockFile(lp)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	_, _, err = Connect(ctx, ConnectOptions{Home: home, Timeout: 200 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "does not fall back to TCP") {
		t.Fatalf("err = %v", err)
	}
}
