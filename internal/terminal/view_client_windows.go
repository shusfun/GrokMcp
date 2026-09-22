//go:build windows

package terminal

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// RunViewer 只连接已有后台终端；它不能创建或取消 Grok 任务。
func RunViewer(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("terminal-view", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	pipe := f.String("pipe", "", "")
	job := f.String("job", "", "")
	sid := f.String("session", "", "")
	gen := f.String("generation", "", "")
	demo := f.Bool("demo", false, "")
	if err := f.Parse(args); err != nil {
		return err
	}
	if !*demo && (*pipe == "" || *job == "" || *gen == "") {
		return errors.New("缺少终端查看连接参数")
	}
	in, out, restore, err := viewerConsole()
	if err != nil {
		return err
	}
	defer restore()
	if *demo {
		fmt.Fprintln(out, "终端模板可用。按任意键关闭此测试窗口。")
		var key [1]byte
		_, err := in.Read(key[:])
		return err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := winio.DialPipeContext(dialCtx, *pipe)
	if err != nil {
		return err
	}
	return runViewStream(ctx, c, viewFrame{Job: *job, Session: *sid, Generation: *gen, PID: os.Getpid()}, in, out, func() (int, int, error) { return term.GetSize(int(out.Fd())) })
}

func viewerConsole() (*os.File, *os.File, func(), error) {
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		ok, _, allocErr := windows.NewLazySystemDLL("kernel32.dll").NewProc("AllocConsole").Call()
		if ok == 0 {
			return nil, nil, nil, allocErr
		}
		in, err = os.OpenFile("CONIN$", os.O_RDWR, 0)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		in.Close()
		return nil, nil, nil, err
	}
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		in.Close()
		out.Close()
		return nil, nil, nil, err
	}
	var inMode, outMode uint32
	_ = windows.GetConsoleMode(windows.Handle(in.Fd()), &inMode)
	_ = windows.GetConsoleMode(windows.Handle(out.Fd()), &outMode)
	if err = windows.SetConsoleMode(windows.Handle(in.Fd()), inMode|windows.ENABLE_VIRTUAL_TERMINAL_INPUT); err != nil {
		term.Restore(int(in.Fd()), state)
		return nil, nil, nil, err
	}
	if err = configureViewerOutput(windows.Handle(out.Fd()), outMode); err != nil {
		term.Restore(int(in.Fd()), state)
		return nil, nil, nil, err
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	kernel.NewProc("SetConsoleCP").Call(65001)
	kernel.NewProc("SetConsoleOutputCP").Call(65001)
	// 进程退出时由系统关闭控制台句柄，避免同步键盘读取阻塞退出。
	return in, out, func() {
		_ = term.Restore(int(in.Fd()), state)
		_ = windows.SetConsoleMode(windows.Handle(out.Fd()), outMode)
	}, nil
}

func configureViewerOutput(handle windows.Handle, original uint32) error {
	// ConPTY 已包含光标定位；末列必须延迟换行，避免多滚一行后留下残影。
	return windows.SetConsoleMode(handle, original|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
}
