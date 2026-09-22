package paths

import (
	"path/filepath"
	"runtime"
)

// Windows 专用地址不读取共享 GROK_LEADER_SOCKET，避免接管别的会话。
func SupervisorLeaderSocket() (string, error) {
	if runtime.GOOS != "windows" {
		return LeaderSocket(), nil
	}
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "grok-leader.sock"), nil
}

func GrokArgs(args ...string) ([]string, error) {
	if runtime.GOOS != "windows" {
		return args, nil
	}
	socket, err := SupervisorLeaderSocket()
	if err != nil {
		return nil, err
	}
	return append([]string{"--leader-socket", socket}, args...), nil
}
