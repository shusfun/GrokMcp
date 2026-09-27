package mcp

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"grokmcp/internal/agent"
	appruntime "grokmcp/internal/runtime"
	"grokmcp/internal/terminal"
)

func TestStdioColdStartAndSameServerHostRestart(t *testing.T) {
	// macOS Unix socket 路径上限约 104 字节。t.TempDir 会带上完整测试名，CI 上会 bind EINVAL。
	home, err := os.MkdirTemp("", "gs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var mu sync.Mutex
	var hosts []func()
	starter := func() error {
		_, cleanup, err := appruntime.Open(context.Background(), appruntime.Options{
			Home: home, Agent: agent.NewFake(), Term: terminal.NewFake(),
		})
		if err != nil {
			return err
		}
		mu.Lock()
		hosts = append(hosts, cleanup)
		mu.Unlock()
		return nil
	}
	backend, disconnect, err := appruntime.Connect(ctx, appruntime.ConnectOptions{
		Home: home, Starter: starter, Timeout: 8 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(disconnect)

	clientRead, serverWrite := io.Pipe()
	serverRead, clientWrite := io.Pipe()
	serveCtx, serveCancel := context.WithCancel(ctx)
	defer serveCancel()
	go func() {
		server := newServer(backend)
		ss, err := server.Connect(serveCtx, &sdk.IOTransport{Reader: serverRead, Writer: serverWrite}, nil)
		if err != nil {
			return
		}
		setMCP(serveCtx, backend, true)
		<-serveCtx.Done()
		stop, stopCancel := context.WithTimeout(context.Background(), time.Second)
		setMCP(stop, backend, false)
		stopCancel()
		_ = ss.Close()
	}()
	client := sdk.NewClient(&sdk.Implementation{Name: "stdio-gate", Version: "test"}, nil)
	session, err := client.Connect(ctx, &sdk.IOTransport{Reader: clientRead, Writer: clientWrite}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "grok_project_list", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("cold start tool: %v", err)
	}
	mu.Lock()
	if len(hosts) != 1 {
		mu.Unlock()
		t.Fatalf("hosts=%d, want 1", len(hosts))
	}
	hosts[0]()
	mu.Unlock()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "grok_project_list", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("same stdio server after host restart: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hosts) < 2 {
		t.Fatalf("host was not restarted, hosts=%d", len(hosts))
	}
	for _, cleanup := range hosts[1:] {
		t.Cleanup(cleanup)
	}
}
