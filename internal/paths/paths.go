package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "GrokSupervisor"

func AppDir() (string, error) {
	if dir := os.Getenv("GROK_SUPERVISOR_HOME"); dir != "" {
		return dir, os.MkdirAll(dir, 0o700)
	}
	var root string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, "Library", "Application Support", appName)
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		root = filepath.Join(base, appName)
	default:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".local", "share", appName)
	}
	return root, os.MkdirAll(root, 0o700)
}

func DBPath() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "supervisor.db"), nil
}

func SocketPath() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(dir, "ipc.port"), nil
	}
	return filepath.Join(dir, "supervisor.sock"), nil
}

func LockPath() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "supervisor.lock"), nil
}

func LeaderSocket() string {
	if p := os.Getenv("GROK_LEADER_SOCKET"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok", "leader.sock")
}
