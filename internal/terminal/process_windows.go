//go:build windows

package terminal

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func FindResumePID(sessionID string) int {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0
	}
	out, err := exec.Command("wmic", "process", "where", "CommandLine like '%--resume "+sessionID+"%'", "get", "ProcessId").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "ProcessId") {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		if pid > 0 {
			return pid
		}
	}
	return 0
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == 259 // STILL_ACTIVE
}

func terminatePID(pid int, wait time.Duration) error {
	if pid <= 0 {
		return nil
	}
	kill := func() error {
		return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T").Run()
	}
	_ = kill()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

func waitResumeOrCmd(sessionID string, cmd *exec.Cmd) {
	done := make(chan struct{})
	if cmd != nil && cmd.Process != nil {
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
	}
	found := false
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	cmdDone := done
	for {
		if pid := FindResumePID(sessionID); pid > 0 {
			found = true
			if !processAlive(pid) {
				return
			}
		} else if found {
			return
		}
		select {
		case <-cmdDone:
			cmdDone = nil
			if FindResumePID(sessionID) == 0 {
				return
			}
		case <-ticker.C:
		}
	}
}
