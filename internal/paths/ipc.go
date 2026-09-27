package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"runtime"
	"strings"
)

// IPCAddress 是 Supervisor 的稳定本地地址。
// Windows 用当前用户数据目录的哈希隔离命名管道；其他系统沿用 Unix socket。
func IPCAddress() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dir))))
		return `\\.\pipe\grokmcp-supervisor-` + hex.EncodeToString(sum[:8]), nil
	}
	return filepath.Join(dir, "supervisor.sock"), nil
}

// LegacyPortFile 是旧版 Windows 临时 TCP 端口文件。新版只删除它，不读取、不监听。
func LegacyPortFile() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ipc.port"), nil
}
