package ownedprocess

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Console 持续读取后台输出；查看窗口断开不能阻塞任务或关闭控制台。
type Console struct {
	platform    consolePlatform
	mu          sync.Mutex
	history     []byte
	subs        map[int]chan []byte
	next        int
	closed      bool
	done        chan struct{}
	once        sync.Once
	modes       map[int]bool
	controlTail []byte
}

const consoleHistoryLimit = 1024 * 1024

func newConsole(p consolePlatform) *Console {
	c := &Console{platform: p, subs: map[int]chan []byte{}, done: make(chan struct{}), modes: map[int]bool{}}
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
			c.recordModes(part)
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

var privateModeSequence = regexp.MustCompile("\x1b\\[\\?([0-9;]+)([hl])")

// 只恢复终端模式，不回放历史绘图与光标移动。保留尾部以识别跨读取边界的 CSI。
func (c *Console) recordModes(part []byte) {
	data := append(c.controlTail, part...)
	for _, match := range privateModeSequence.FindAllSubmatch(data, -1) {
		for _, value := range strings.Split(string(match[1]), ";") {
			if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 999999 {
				c.modes[n] = match[2][0] == 'h'
			}
		}
	}
	if len(data) > 128 {
		data = data[len(data)-128:]
	}
	c.controlTail = append([]byte(nil), data...)
}

func (c *Console) Modes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]int, 0, len(c.modes))
	for k := range c.modes {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var out bytes.Buffer
	for _, k := range keys {
		action := 'l'
		if c.modes[k] {
			action = 'h'
		}
		fmt.Fprintf(&out, "\x1b[?%d%c", k, action)
	}
	return out.Bytes()
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
func (c *Console) Refresh() error                 { return c.platform.refresh() }
func (c *Console) Close() error {
	c.once.Do(func() { c.platform.closeConsole(); <-c.done; c.platform.closeOutput() })
	return nil
}
func (c *Console) Output() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.history...)
}
