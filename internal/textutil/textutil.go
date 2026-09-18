package textutil

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func TruncateTitle(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return "未命名任务"
	}
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func Digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}

func ElapsedSeconds(created, now time.Time) int64 {
	if created.IsZero() || now.Before(created) {
		return 0
	}
	return int64(now.Sub(created).Seconds())
}

func ProjectName(cwd string) string {
	base := filepath.Base(filepath.Clean(cwd))
	if base == "." || base == string(filepath.Separator) || base == "" {
		return cwd
	}
	return base
}

func FormatElapsed(seconds int64) string {
	if seconds < 60 {
		return "1m"
	}
	if seconds < 3600 {
		return itoa(seconds/60) + "m"
	}
	h := seconds / 3600
	m := (seconds % 3600) / 60
	if m == 0 {
		return itoa(h) + "h"
	}
	return itoa(h) + "h" + itoa(m) + "m"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
