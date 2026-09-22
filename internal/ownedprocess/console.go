package ownedprocess

import "sync"

// Console 持续读取后台输出；查看窗口断开不能阻塞任务或关闭控制台。
type Console struct {
	platform consolePlatform
	mu       sync.Mutex
	history  []byte
	subs     map[int]chan []byte
	next     int
	closed   bool
	done     chan struct{}
	once     sync.Once
}

const consoleHistoryLimit = 1024 * 1024

func newConsole(p consolePlatform) *Console {
	c := &Console{platform: p, subs: map[int]chan []byte{}, done: make(chan struct{})}
	go c.drain()
	return c
}

func (c *Console) drain() {
	defer close(c.done)
	defer func() {
		c.mu.Lock()
		c.closed = true
		for id, ch := range c.subs {
			close(ch)
			delete(c.subs, id)
		}
		c.mu.Unlock()
	}()
	buf := make([]byte, 8192)
	for {
		n, err := c.platform.read(buf)
		if n > 0 {
			part := append([]byte(nil), buf[:n]...)
			c.mu.Lock()
			c.history = append(c.history, part...)
			if len(c.history) > consoleHistoryLimit {
				c.history = append([]byte(nil), c.history[len(c.history)-consoleHistoryLimit:]...)
			}
			for id, ch := range c.subs {
				select {
				case ch <- part:
				default:
					close(ch)
					delete(c.subs, id)
				}
			}
			c.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (c *Console) Subscribe() ([]byte, <-chan []byte, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	ch := make(chan []byte, 64)
	if c.closed {
		close(ch)
	} else {
		c.subs[id] = ch
	}
	return append([]byte(nil), c.history...), ch, func() {
		c.mu.Lock()
		if _, ok := c.subs[id]; ok {
			delete(c.subs, id)
			close(ch)
		}
		c.mu.Unlock()
	}
}
func (c *Console) Write(data []byte) (int, error) { return c.platform.write(data) }
func (c *Console) Resize(cols, rows int) error    { return c.platform.resize(cols, rows) }
func (c *Console) Close() error {
	c.once.Do(func() { c.platform.closeConsole(); <-c.done; c.platform.closeOutput() })
	return nil
}
func (c *Console) Output() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.history...)
}
