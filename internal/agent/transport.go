package agent

import (
	"context"
	"grokmcp/internal/grokbin"
	"io"
)

// NewTransport 使用调用方拥有的字节流，协议初始化与生产子进程连接完全相同。
// Close 接管并关闭两个流；不会启动 leader 或任何进程。
func NewTransport(ctx context.Context, input io.WriteCloser, output io.ReadCloser) (*Grok, error) {
	g := NewGrok("", grokbin.New())
	g.startConnection = func(ctx context.Context) (*connectionResources, error) {
		r := &connectionResources{closers: []io.Closer{input, output}}
		err := g.connectACP(ctx, r, input, output)
		return r, err
	}
	if err := g.EnsureLeader(ctx); err != nil {
		g.Close()
		return nil, err
	}
	return g, nil
}
