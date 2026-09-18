//go:build unix

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
	out, err := exec.Command("ps", "-ax", "-o", "pid=,command=").Output()
	if err != nil {
		return 0
	}
	needle := "--resume " + sessionID
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		return pid
	}
	return 0
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func terminatePID(pid int, wait time.Duration) error {
	if pid <= 0 {
		return nil
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
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
