//go:build windows

package ownedprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 在 CreateProcess 内原子加入 Job，避免先启动再 Assign 时子进程逃逸。
const procThreadAttributeJobList = 0x0002000d

type Group struct {
	mu        sync.Mutex
	job       windows.Handle
	processes []*Process
}

func New() (*Group, error) {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	return &Group{job: h}, nil
}

func (g *Group) Start(spec Spec) (*Process, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.job == 0 {
		return nil, errors.New("process group closed")
	}
	path, err := exec.LookPath(spec.Path)
	if err != nil {
		return nil, err
	}
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	line, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, spec.Args...)))
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if spec.Dir != "" {
		dir, err = windows.UTF16PtrFromString(spec.Dir)
		if err != nil {
			return nil, err
		}
	}
	attrs, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, err
	}
	defer attrs.Delete()
	jobs := []windows.Handle{g.job}
	if err = attrs.Update(procThreadAttributeJobList, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
		return nil, err
	}
	si := windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = attrs.List()
	if spec.Title != "" {
		si.Title, err = windows.UTF16PtrFromString(spec.Title)
		if err != nil {
			return nil, err
		}
	}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)
	inherit := false
	var inherited []windows.Handle
	if spec.Console != nil {
		si.Flags = windows.STARTF_USESTDHANDLES
		if spec.Visible {
			return nil, errors.New("后台 ConPTY 不能创建可见窗口")
		}
		spec.Console.platform.state.mu.Lock()
		defer spec.Console.platform.state.mu.Unlock()
		if err := spec.Console.attach(attrs); err != nil {
			return nil, err
		}
	} else if spec.Visible {
		flags |= windows.CREATE_NEW_CONSOLE
	} else {
		flags |= windows.CREATE_NO_WINDOW
		si.Flags = windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW
		si.ShowWindow = windows.SW_HIDE
		null, e := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if e != nil {
			return nil, e
		}
		defer null.Close()
		for _, f := range []*os.File{spec.Stdin, spec.Stdout, spec.Stderr} {
			if f == nil {
				f = null
			}
			var h windows.Handle
			err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(f.Fd()), windows.CurrentProcess(), &h, 0, true, windows.DUPLICATE_SAME_ACCESS)
			if err != nil {
				return nil, err
			}
			defer windows.CloseHandle(h)
			inherited = append(inherited, h)
		}
		si.StdInput, si.StdOutput, si.StdErr = inherited[0], inherited[1], inherited[2]
		if err = attrs.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&inherited[0]), uintptr(len(inherited))*unsafe.Sizeof(inherited[0])); err != nil {
			return nil, err
		}
		inherit = true
	}
	env := spec.Env
	if env == nil {
		env = os.Environ()
	}
	env = append([]string(nil), env...)
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	for _, s := range env {
		if strings.ContainsRune(s, 0) {
			return nil, errors.New("invalid environment")
		}
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	var info windows.ProcessInformation
	err = windows.CreateProcess(app, line, nil, nil, inherit, flags, &block[0], dir, &si.StartupInfo, &info)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(inherited)
	runtime.KeepAlive(block)
	if err != nil {
		return nil, err
	}
	windows.CloseHandle(info.Thread)
	p := &Process{PID: int(info.ProcessId), done: make(chan struct{})}
	var pmu sync.Mutex
	h := info.Process
	p.kill = func() error {
		pmu.Lock()
		defer pmu.Unlock()
		if h == 0 {
			return nil
		}
		return windows.TerminateProcess(h, 1)
	}
	go func() {
		_, err := windows.WaitForSingleObject(info.Process, windows.INFINITE)
		var code uint32
		if err == nil {
			err = windows.GetExitCodeProcess(info.Process, &code)
		}
		if err == nil && code != 0 {
			err = fmt.Errorf("process exited with code %d", code)
		}
		pmu.Lock()
		windows.CloseHandle(h)
		h = 0
		pmu.Unlock()
		p.err = err
		close(p.done)
	}()
	g.processes = append(g.processes, p)
	return p, nil
}

func (g *Group) Close() error {
	g.mu.Lock()
	h := g.job
	g.job = 0
	ps := append([]*Process(nil), g.processes...)
	g.mu.Unlock()
	var err error
	if h != 0 {
		err = windows.CloseHandle(h)
	}
	for _, p := range ps {
		_ = p.Wait()
	}
	return err
}
