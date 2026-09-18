package store

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(DELETE)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	_, _ = s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	return s.db.Close()
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS jobs (
  job_id TEXT PRIMARY KEY,
  codex_thread_id TEXT,
  grok_session_id TEXT,
  cwd TEXT NOT NULL,
  title TEXT,
  state TEXT NOT NULL,
  view_mode TEXT NOT NULL,
  input_owner TEXT NOT NULL,
  plan_digest TEXT,
  last_action TEXT,
  last_summary TEXT,
  user_cancelled INTEGER NOT NULL DEFAULT 0,
  missing_marker_count INTEGER NOT NULL DEFAULT 0,
  recover_fails TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS boundary_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  summary TEXT,
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE jobs ADD COLUMN plan_summary TEXT`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE jobs ADD COLUMN desired_view_mode TEXT`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return err
		}
	}
	if _, err := s.db.Exec(`ALTER TABLE jobs ADD COLUMN archived_at INTEGER`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return err
		}
	}
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS jobs_list_idx ON jobs(archived_at, updated_at, created_at, job_id)`)
	return nil
}

func (s *Store) Ping() error {
	return s.db.Ping()
}

type Record struct {
	Job                protocol.Job
	MissingMarkerCount int
	RecoverFails       []int64
}

const jobSelectCols = `job_id, codex_thread_id, grok_session_id, cwd, title, state, view_mode, desired_view_mode, input_owner,
		plan_digest, plan_summary, last_action, last_summary, user_cancelled, missing_marker_count, recover_fails, created_at, updated_at, archived_at`

func (s *Store) PutJob(rec Record) error {
	j := rec.Job
	_, err := s.db.Exec(`
INSERT INTO jobs (
  job_id, codex_thread_id, grok_session_id, cwd, title, state, view_mode, desired_view_mode, input_owner,
  plan_digest, plan_summary, last_action, last_summary, user_cancelled, missing_marker_count, recover_fails,
  created_at, updated_at, archived_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(job_id) DO UPDATE SET
  codex_thread_id=excluded.codex_thread_id,
  grok_session_id=excluded.grok_session_id,
  cwd=excluded.cwd,
  title=excluded.title,
  state=excluded.state,
  view_mode=excluded.view_mode,
  desired_view_mode=excluded.desired_view_mode,
  input_owner=excluded.input_owner,
  plan_digest=excluded.plan_digest,
  plan_summary=excluded.plan_summary,
  last_action=excluded.last_action,
  last_summary=excluded.last_summary,
  user_cancelled=excluded.user_cancelled,
  missing_marker_count=excluded.missing_marker_count,
  recover_fails=excluded.recover_fails,
  updated_at=excluded.updated_at,
  archived_at=excluded.archived_at
`, j.JobID, j.CodexThreadID, j.GrokSessionID, j.Cwd, j.Title, string(j.State), string(j.ViewMode),
		string(desiredView(j)), string(j.InputOwner), j.PlanDigest, j.PlanSummary, j.LastAction, j.LastSummary, boolToInt(j.UserCancelled),
		rec.MissingMarkerCount, joinInt64(rec.RecoverFails), j.CreatedAt.Unix(), j.UpdatedAt.Unix(), nullUnix(j.ArchivedAt))
	return err
}

func (s *Store) GetJob(id string) (Record, error) {
	row := s.db.QueryRow(`SELECT `+jobSelectCols+` FROM jobs WHERE job_id=?`, id)
	return scanJob(row)
}

func (s *Store) ListJobs() ([]Record, error) {
	rows, err := s.db.Query(`SELECT ` + jobSelectCols + ` FROM jobs ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

const activeGroupSQL = `CASE WHEN state IN ('starting','planning','executing','recovering','plan_ready','needs_input','disconnected') THEN 1 ELSE 0 END`

func (s *Store) ListJobsPage(q protocol.ListJobsQuery) ([]Record, string, bool, error) {
	limit := protocol.ClampJobLimit(q.Limit)
	var (
		where []string
		args  []any
	)
	if q.IncludeArchived {
		where = append(where, `archived_at IS NOT NULL`)
	} else {
		where = append(where, `archived_at IS NULL`)
	}
	if st := strings.TrimSpace(q.State); st != "" && st != "all" {
		switch st {
		case "attention":
			where = append(where, `state IN ('plan_ready','needs_input')`)
		case "working":
			where = append(where, `state IN ('planning','executing','starting','recovering')`)
		default:
			where = append(where, `state = ?`)
			args = append(args, st)
		}
	}
	if view := strings.TrimSpace(q.View); view != "" && view != "all" {
		where = append(where, `view_mode = ?`)
		args = append(args, view)
	}
	if project := strings.TrimSpace(q.Project); project != "" && project != "all" {
		where = append(where, `(cwd = ? OR cwd LIKE '%/' || ? OR cwd LIKE '%\\' || ?)`)
		args = append(args, project, project, project)
	}
	if query := strings.TrimSpace(q.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		where = append(where, `(lower(ifnull(title,'')) LIKE ? OR lower(ifnull(last_action,'')) LIKE ? OR lower(ifnull(grok_session_id,'')) LIKE ? OR lower(cwd) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	if strings.TrimSpace(q.Cursor) != "" {
		cur, err := protocol.DecodeJobCursor(q.Cursor)
		if err != nil {
			return nil, "", false, err
		}
		where = append(where, fmt.Sprintf(`(
  %[1]s < ? OR
  (%[1]s = ? AND updated_at < ?) OR
  (%[1]s = ? AND updated_at = ? AND created_at < ?) OR
  (%[1]s = ? AND updated_at = ? AND created_at = ? AND job_id < ?)
)`, activeGroupSQL))
		args = append(args, cur.G, cur.G, cur.U, cur.G, cur.U, cur.C, cur.G, cur.U, cur.C, cur.I)
	}
	sql := `SELECT ` + jobSelectCols + ` FROM jobs WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY ` + activeGroupSQL + ` DESC, updated_at DESC, created_at DESC, job_id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, "", false, err
	}
	defer rows.Close()
	var out []Record
	for rows.Next() {
		rec, err := scanJob(rows)
		if err != nil {
			return nil, "", false, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	next := ""
	if hasMore && len(out) > 0 {
		next = protocol.EncodeJobCursor(out[len(out)-1].Job)
	}
	return out, next, hasMore, nil
}

func (s *Store) ListProjects(includeArchived bool) ([]string, error) {
	q := `SELECT cwd FROM jobs`
	if !includeArchived {
		q += ` WHERE archived_at IS NULL`
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]struct{}{}
	var out []string
	for rows.Next() {
		var cwd string
		if err := rows.Scan(&cwd); err != nil {
			return nil, err
		}
		p := textutil.ProjectName(cwd)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	slices.Sort(out)
	return out, rows.Err()
}

func (s *Store) DeleteJob(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM boundary_events WHERE job_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM jobs WHERE job_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddEvent(jobID, eventType, summary string, at time.Time) error {
	_, err := s.db.Exec(`INSERT INTO boundary_events (job_id, event_type, summary, created_at) VALUES (?,?,?,?)`,
		jobID, eventType, summary, at.Unix())
	return err
}

func (s *Store) Events(jobID string, limit int) ([]protocol.BoundaryEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, job_id, event_type, summary, created_at FROM boundary_events
		WHERE job_id=? ORDER BY id DESC LIMIT ?`, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]protocol.BoundaryEvent, 0)
	for rows.Next() {
		var e protocol.BoundaryEvent
		var ts int64
		if err := rows.Scan(&e.ID, &e.JobID, &e.EventType, &e.Summary, &ts); err != nil {
			return nil, err
		}
		e.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) Settings() (protocol.Settings, error) {
	st := protocol.Settings{DefaultViewMode: string(protocol.ViewHeadless)}
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return st, err
		}
		switch k {
		case "grok_binary_path":
			st.GrokBinaryPath = v
		case "terminal_provider":
			st.TerminalProvider = v
		case "terminal_command_template":
			st.TerminalCommandTemplate = v
		case "default_view_mode":
			if v != "" {
				st.DefaultViewMode = v
			}
		case "debug_enabled":
			st.DebugEnabled = parseBool(v)
		case "debug_payloads":
			st.DebugPayloads = parseBool(v)
		}
	}
	return st, rows.Err()
}

func (s *Store) SaveSettings(st protocol.Settings) error {
	pairs := [][2]string{
		{"grok_binary_path", st.GrokBinaryPath},
		{"terminal_provider", st.TerminalProvider},
		{"terminal_command_template", st.TerminalCommandTemplate},
		{"default_view_mode", st.DefaultViewMode},
		{"debug_enabled", fmtBool(st.DebugEnabled)},
		{"debug_payloads", fmtBool(st.DebugPayloads)},
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range pairs {
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, p[0], p[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (Record, error) {
	var rec Record
	var state, view, owner string
	var desired sql.NullString
	var cancelled, created, updated int64
	var archived sql.NullInt64
	var fails string
	err := row.Scan(&rec.Job.JobID, &rec.Job.CodexThreadID, &rec.Job.GrokSessionID, &rec.Job.Cwd, &rec.Job.Title,
		&state, &view, &desired, &owner, &rec.Job.PlanDigest, &rec.Job.PlanSummary, &rec.Job.LastAction, &rec.Job.LastSummary,
		&cancelled, &rec.MissingMarkerCount, &fails, &created, &updated, &archived)
	if err != nil {
		return Record{}, err
	}
	rec.Job.State = protocol.JobState(state)
	rec.Job.ViewMode = protocol.ViewMode(view)
	rec.Job.DesiredViewMode = protocol.ViewMode(desired.String)
	rec.Job.InputOwner = protocol.InputOwner(owner)
	if rec.Job.DesiredViewMode == "" {
		if rec.Job.ViewMode == protocol.ViewHeaded || rec.Job.ViewMode == protocol.ViewAttaching {
			rec.Job.DesiredViewMode = protocol.ViewHeaded
		} else {
			rec.Job.DesiredViewMode = protocol.ViewHeadless
		}
	}
	rec.Job.UserCancelled = cancelled != 0
	rec.Job.CreatedAt = time.Unix(created, 0).UTC()
	rec.Job.UpdatedAt = time.Unix(updated, 0).UTC()
	if archived.Valid && archived.Int64 != 0 {
		rec.Job.ArchivedAt = time.Unix(archived.Int64, 0).UTC()
	}
	rec.Job.Project = textutil.ProjectName(rec.Job.Cwd)
	rec.RecoverFails = splitInt64(fails)
	return rec, nil
}

func parseBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func fmtBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func desiredView(j protocol.Job) protocol.ViewMode {
	if j.DesiredViewMode != "" {
		return j.DesiredViewMode
	}
	if j.ViewMode == protocol.ViewHeaded || j.ViewMode == protocol.ViewAttaching {
		return protocol.ViewHeaded
	}
	return protocol.ViewHeadless
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nullUnix(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

func joinInt64(xs []int64) string {
	if len(xs) == 0 {
		return ""
	}
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprintf("%d", x)
	}
	return strings.Join(parts, ",")
}

func splitInt64(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		var n int64
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}
