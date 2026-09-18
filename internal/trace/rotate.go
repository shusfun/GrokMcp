package trace

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	maxAge  = 7 * 24 * time.Hour
	maxSize = 500 * 1024 * 1024
)

func (l *Log) rotate() {
	if l == nil {
		return
	}
	now := l.now()
	var files []fileInfo
	for _, dir := range []string{filepath.Join(l.root, "logs"), filepath.Join(l.root, "traces")} {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			st, err := d.Info()
			if err != nil {
				return nil
			}
			files = append(files, fileInfo{path: path, mod: st.ModTime(), size: st.Size()})
			return nil
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	var total int64
	for _, f := range files {
		total += f.size
	}
	for _, f := range files {
		if now.Sub(f.mod) > maxAge || total > maxSize {
			if err := os.Remove(f.path); err == nil {
				total -= f.size
			}
			continue
		}
		break
	}
}

type fileInfo struct {
	path string
	mod  time.Time
	size int64
}
