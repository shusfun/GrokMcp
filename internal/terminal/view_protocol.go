package terminal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type viewFrame struct {
	Job        string `json:"job,omitempty"`
	Session    string `json:"session,omitempty"`
	Generation string `json:"generation,omitempty"`
	PID        int    `json:"pid,omitempty"`
	Input      []byte `json:"input,omitempty"`
	Output     []byte `json:"output,omitempty"`
	Cols       int    `json:"cols,omitempty"`
	Rows       int    `json:"rows,omitempty"`
	Ready      bool   `json:"ready,omitempty"`
}

func runViewStream(ctx context.Context, c net.Conn, hello viewFrame, in io.Reader, out io.Writer, size func() (int, int, error)) error {
	defer c.Close()
	var writeMu sync.Mutex
	send := func(f viewFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = c.SetWriteDeadline(time.Now().Add(3 * time.Second))
		return json.NewEncoder(c).Encode(f)
	}
	if err := send(hello); err != nil {
		return err
	}
	var ready atomic.Bool
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			if n > 0 && ready.Load() {
				if send(viewFrame{Input: buf[:n]}) != nil {
					return
				}
			}
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		cols, rows := 0, 0
		for {
			select {
			case <-ctx.Done():
				_ = c.Close()
				return
			case <-stop:
				return
			case <-ticker.C:
				w, h, err := size()
				if err == nil && w > 0 && h > 0 && (w != cols || h != rows) {
					cols, rows = w, h
					if send(viewFrame{Cols: w, Rows: h}) != nil {
						return
					}
				}
			}
		}
	}()
	scan := bufio.NewScanner(c)
	scan.Buffer(make([]byte, 8192), 2*1024*1024)
	for scan.Scan() {
		var f viewFrame
		if err := json.Unmarshal(scan.Bytes(), &f); err != nil {
			return err
		}
		if f.Ready {
			ready.Store(true)
		}
		if len(f.Output) > 0 {
			if _, err := out.Write(f.Output); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := scan.Err(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}
