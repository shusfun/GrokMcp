package trace

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type DebugOpts struct {
	Enabled  bool
	Payloads bool
}

type Snapshot struct {
	JobID  string  `json:"job_id"`
	Cursor int64   `json:"cursor"`
	Events []Event `json:"events"`
}

type Sink interface {
	Emit(Event)
}

type Log struct {
	root string
	now  func() time.Time

	mu         sync.Mutex
	seq        map[string]int64
	mem        map[string][]Event
	dbg        map[string]DebugOpts
	override   map[string]bool
	global     DebugOpts
	subs       map[int]func(Event)
	subSeq     int
	writes     int
	writeFails int
	closed     bool
}

func Open(root string, now func() time.Time) (*Log, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(filepath.Join(root, "logs"), 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "traces"), 0o700); err != nil {
		return nil, err
	}
	return &Log{
		root:     root,
		now:      now,
		seq:      map[string]int64{},
		mem:      map[string][]Event{},
		dbg:      map[string]DebugOpts{},
		override: map[string]bool{},
		subs:     map[int]func(Event){},
	}, nil
}

func (l *Log) SetDebug(jobID string, enabled, payloads bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	opts := DebugOpts{Enabled: enabled, Payloads: payloads}
	if jobID == "" {
		l.setGlobalLocked(opts)
		return
	}
	l.override[jobID] = true
	l.dbg[jobID] = opts
}

func (l *Log) SetGlobal(enabled, payloads bool) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.setGlobalLocked(DebugOpts{Enabled: enabled, Payloads: payloads})
}

func (l *Log) setGlobalLocked(opts DebugOpts) {
	l.global = opts
	if !opts.Enabled {
		l.dbg = map[string]DebugOpts{}
		l.override = map[string]bool{}
	}
}

func (l *Log) Global() DebugOpts {
	if l == nil {
		return DebugOpts{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.global
}

func (l *Log) Debug(jobID string) DebugOpts {
	if l == nil {
		return DebugOpts{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.debugLocked(jobID)
}

func (l *Log) debugLocked(jobID string) DebugOpts {
	if l.override[jobID] {
		return l.dbg[jobID]
	}
	return l.global
}

func (l *Log) Cursor(jobID string) int64 {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if n := l.seq[jobID]; n > 0 {
		return n
	}
	return l.fileCursorLocked(jobID)
}

func (l *Log) WriteFails() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.writeFails
}

func (l *Log) Subscribe(fn func(Event)) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	l.subSeq++
	id := l.subSeq
	l.subs[id] = fn
	l.mu.Unlock()
	return func() {
		l.mu.Lock()
		delete(l.subs, id)
		l.mu.Unlock()
	}
}

func (l *Log) Emit(ev Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	dbg := l.debugLocked(ev.JobID)
	if ev.Level == "" {
		ev.Level = LevelInfo
	}
	if ev.Level == LevelDebug && !dbg.Enabled {
		l.mu.Unlock()
		return
	}
	if ev.Time.IsZero() {
		ev.Time = l.now().UTC()
	}
	key := ev.JobID
	if key == "" {
		key = "_"
	}
	l.seq[key]++
	ev.Seq = l.seq[key]
	ev.Fields = SanitizeFields(ev.Fields, dbg.Payloads)
	l.mem[key] = append(l.mem[key], ev)
	if len(l.mem[key]) > 5000 {
		l.mem[key] = l.mem[key][len(l.mem[key])-4000:]
	}
	fns := make([]func(Event), 0, len(l.subs))
	for _, fn := range l.subs {
		fns = append(fns, fn)
	}
	l.writes++
	rotate := l.writes%64 == 0
	l.mu.Unlock()

	l.appendFile(key, ev)
	if rotate {
		l.rotate()
	}
	for _, fn := range fns {
		fn(ev)
	}
}

func (l *Log) Snapshot(jobID string, cursor int64, limit int, levels, sources []string) Snapshot {
	if l == nil {
		return Snapshot{JobID: jobID, Events: []Event{}}
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	events := l.eventsAfter(jobID, cursor)
	out := make([]Event, 0, limit)
	bytes := 0
	for _, ev := range events {
		if !matchLevel(ev.Level, levels) || !matchSource(ev.Source, sources) {
			continue
		}
		raw, _ := json.Marshal(ev)
		if bytes+len(raw) > MaxSnapshotBytes && len(out) > 0 {
			break
		}
		out = append(out, ev)
		bytes += len(raw) + 1
		if len(out) >= limit {
			break
		}
	}
	cur := cursor
	if len(out) > 0 {
		cur = out[len(out)-1].Seq
	} else {
		if n := l.Cursor(jobID); n > cursor {
			cur = n
		}
	}
	if out == nil {
		out = []Event{}
	}
	return Snapshot{JobID: jobID, Cursor: cur, Events: out}
}

func (l *Log) Wait(ctx context.Context, jobID string, cursor int64, until string, timeout time.Duration) Snapshot {
	if until == "" {
		until = UntilAny
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if snap := l.Snapshot(jobID, cursor, DefaultLimit, nil, nil); hasUntil(snap.Events, until) {
		return filterUntil(snap, until)
	}
	ch := make(chan struct{}, 1)
	off := l.Subscribe(func(ev Event) {
		if ev.JobID != jobID && jobID != "" {
			return
		}
		select {
		case ch <- struct{}{}:
		default:
		}
	})
	defer off()
	for {
		select {
		case <-ctx.Done():
			return l.Snapshot(jobID, cursor, DefaultLimit, nil, nil)
		case <-ch:
			snap := l.Snapshot(jobID, cursor, DefaultLimit, nil, nil)
			if hasUntil(snap.Events, until) {
				return filterUntil(snap, until)
			}
		}
	}
}

func (l *Log) Export(jobID string) (string, error) {
	src := l.tracePath(jobID)
	dst := filepath.Join(l.root, "traces", jobID+"-export-"+l.now().UTC().Format("20060102T150405")+".jsonl")
	b, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.WriteFile(dst, nil, 0o600); err != nil {
				return "", err
			}
			return dst, nil
		}
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

func (l *Log) Remove(jobID string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	delete(l.seq, jobID)
	delete(l.mem, jobID)
	delete(l.dbg, jobID)
	delete(l.override, jobID)
	path := l.tracePath(jobID)
	l.mu.Unlock()
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (l *Log) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}

func (l *Log) eventsAfter(jobID string, cursor int64) []Event {
	key := jobID
	if key == "" {
		key = "_"
	}
	l.mu.Lock()
	mem := append([]Event{}, l.mem[key]...)
	l.mu.Unlock()
	if len(mem) > 0 && mem[0].Seq <= cursor+1 {
		out := make([]Event, 0, len(mem))
		for _, ev := range mem {
			if ev.Seq > cursor {
				out = append(out, ev)
			}
		}
		return out
	}
	return l.readFile(jobID, cursor)
}

func (l *Log) readFile(jobID string, cursor int64) []Event {
	f, err := os.Open(l.tracePath(jobID))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Seq > cursor {
			out = append(out, ev)
		}
	}
	return out
}

func (l *Log) appendFile(key string, ev Event) {
	raw, err := json.Marshal(ev)
	if err != nil {
		l.noteWriteFail()
		return
	}
	raw = append(raw, '\n')
	if key != "_" {
		if err := appendFile(l.tracePath(key), raw); err != nil {
			l.noteWriteFail()
		}
	}
	day := ev.Time.UTC().Format("2006-01-02")
	if err := appendFile(filepath.Join(l.root, "logs", "supervisor-"+day+".jsonl"), raw); err != nil {
		l.noteWriteFail()
	}
}

func (l *Log) noteWriteFail() {
	l.mu.Lock()
	l.writeFails++
	l.mu.Unlock()
}

func (l *Log) tracePath(jobID string) string {
	if jobID == "" {
		jobID = "_"
	}
	return filepath.Join(l.root, "traces", jobID+".jsonl")
}

func (l *Log) fileCursorLocked(jobID string) int64 {
	events := l.readFile(jobID, 0)
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

func appendFile(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	cerr := f.Close()
	if err != nil {
		return err
	}
	return cerr
}

func matchLevel(level string, levels []string) bool {
	if len(levels) == 0 {
		return true
	}
	for _, l := range levels {
		if l == level {
			return true
		}
	}
	return false
}

func matchSource(source string, sources []string) bool {
	if len(sources) == 0 {
		return true
	}
	for _, s := range sources {
		if s == source {
			return true
		}
	}
	return false
}

func hasUntil(events []Event, until string) bool {
	for _, ev := range events {
		if matchUntil(ev, until) {
			return true
		}
	}
	return false
}

func matchUntil(ev Event, until string) bool {
	switch until {
	case UntilError:
		return ev.Level == LevelError
	case UntilWarning:
		return ev.Level == LevelWarn || ev.Level == LevelError
	case UntilBoundary:
		return ev.Boundary()
	default:
		return true
	}
}

func filterUntil(snap Snapshot, until string) Snapshot {
	if until == UntilAny {
		return snap
	}
	out := make([]Event, 0, len(snap.Events))
	for _, ev := range snap.Events {
		out = append(out, ev)
		if matchUntil(ev, until) {
			snap.Events = out
			snap.Cursor = ev.Seq
			return snap
		}
	}
	return snap
}
