//go:build windows

package ownedprocess

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"sync"
	"unsafe"
)

type consolePlatform struct{ state *consoleState }
type consoleState struct {
	mu      sync.Mutex
	hpc     windows.Handle
	in, out *os.File
}

func NewConsole(cols, rows int) (*Console, error) {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return nil, errors.New("invalid console size")
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	var hpc windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, windows.Handle(inR.Fd()), windows.Handle(outW.Fd()), 0, &hpc)
	inR.Close()
	outW.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		return nil, err
	}
	return newConsole(consolePlatform{state: &consoleState{hpc: hpc, in: inW, out: outR}}), nil
}
func (p consolePlatform) read(b []byte) (int, error)  { return p.state.out.Read(b) }
func (p consolePlatform) write(b []byte) (int, error) { return p.state.in.Write(b) }
func (p consolePlatform) resize(cols, rows int) error {
	if cols < 1 || rows < 1 || cols > 1000 || rows > 1000 {
		return errors.New("invalid console size")
	}
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	if p.state.hpc == 0 {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(p.state.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}
func (p consolePlatform) closeConsole() {
	p.state.mu.Lock()
	h := p.state.hpc
	p.state.hpc = 0
	p.state.mu.Unlock()
	if h != 0 {
		windows.ClosePseudoConsole(h)
	}
	_ = p.state.in.Close()
}
func (p consolePlatform) closeOutput() { _ = p.state.out.Close() }

// PSEUDOCONSOLE 的值是原生 HPCON 本身，不是指向 HANDLE 的指针。
func (c *Console) attach(attrs *windows.ProcThreadAttributeListContainer) error {
	if c.platform.state.hpc == 0 {
		return os.ErrClosed
	}
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")
	ok, _, err := proc.Call(uintptr(unsafe.Pointer(attrs.List())), 0, windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, uintptr(c.platform.state.hpc), unsafe.Sizeof(c.platform.state.hpc), 0, 0)
	if ok == 0 {
		return err
	}
	return nil
}
