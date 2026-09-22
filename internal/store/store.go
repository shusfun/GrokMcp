package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"

	_ "modernc.org/sqlite"
)

type Store struct {
	db      *sql.DB
	writeMu sync.Mutex
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
	if _, err := s.db.Exec(`UPDATE settings SET value='headless' WHERE key='default_view_mode' AND value<>'headless'`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE jobs ADD COLUMN lifecycle TEXT NOT NULL DEFAULT '{}'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS job_notifications (id INTEGER PRIMARY KEY AUTOINCREMENT, job_id TEXT NOT NULL, snapshot TEXT NOT NULL); CREATE INDEX IF NOT EXISTS notification_job ON job_notifications(job_id,id); CREATE TABLE IF NOT EXISTS request_results(job_id TEXT NOT NULL,request_id TEXT NOT NULL,turn_id TEXT NOT NULL,summary TEXT NOT NULL); CREATE TABLE IF NOT EXISTS work_requests(request_id TEXT PRIMARY KEY,job_id TEXT NOT NULL,prompt TEXT NOT NULL,planning INTEGER NOT NULL,state TEXT NOT NULL,turn_id TEXT NOT NULL DEFAULT '',summary TEXT NOT NULL DEFAULT '')`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS turn_results(job_id TEXT NOT NULL,request_id TEXT NOT NULL,turn_id TEXT NOT NULL,body TEXT NOT NULL,stop_reason TEXT NOT NULL,state TEXT NOT NULL,PRIMARY KEY(job_id,turn_id))`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE work_requests ADD COLUMN phase TEXT NOT NULL DEFAULT 'unknown'`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if _, err := s.db.Exec(`UPDATE work_requests SET phase='queued' WHERE phase='unknown' AND state='queued' AND turn_id=''`); err != nil {
		return err
	}
	return s.migrateProjects()
}

func (s *Store) Ping() error {
	return s.db.Ping()
}

type RequestResult struct {
	RequestID, TurnID, Summary, Text, StopReason string
	State                                        protocol.JobState
}
type WorkRequest struct {
	RequestID, Prompt, Phase string
	Planning                 bool
}
type Record struct {
	RequestPhase string
	Accepted     *WorkRequest
	Result       *RequestResult

	Job                protocol.Job
	MissingMarkerCount int
	RecoverFails       []int64
}

const jobSelectCols = `jobs.job_id, jobs.codex_thread_id, jobs.grok_session_id, jobs.cwd, jobs.title, jobs.state, jobs.view_mode, jobs.desired_view_mode, jobs.input_owner,
		jobs.plan_digest, jobs.plan_summary, jobs.last_action, jobs.last_summary, jobs.user_cancelled, jobs.missing_marker_count, jobs.recover_fails, jobs.created_at, jobs.updated_at, jobs.archived_at, jobs.project_id, ifnull(projects.name,''), jobs.lifecycle`

const jobFrom = `jobs LEFT JOIN projects ON projects.project_id = jobs.project_id`

func (s *Store) PutJob(rec Record) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j := rec.Job
	var previous string
	err = tx.QueryRow(`SELECT lifecycle FROM jobs WHERE job_id=?`, j.JobID).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var old protocol.Job
	if previous != "" {
		if err := json.Unmarshal([]byte(previous), &old); err != nil {
			return err
		}
	}
	if rec.RequestPhase != "" {
		j.RequestPhase = rec.RequestPhase
	} else if j.RequestID != old.RequestID {
		j.RequestPhase = "queued"
	}
	j.EventCursor = old.EventCursor
	boundary := j.State.IsBoundary() || j.Stalled
	if boundary && (old.State != j.State || old.RequestID != j.RequestID || old.PlanVersion != j.PlanVersion || old.StalledReason != j.StalledReason || old.LastSummary != j.LastSummary || old.ApprovalID != j.ApprovalID || old.PauseReason != j.PauseReason) {
		b, err := json.Marshal(j)
		if err != nil {
			return err
		}
		result, err := tx.Exec(`INSERT INTO job_notifications(job_id,snapshot) VALUES(?,?)`, j.JobID, string(b))
		if err != nil {
			return err
		}
		j.EventCursor, err = result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO boundary_events(job_id,event_type,summary,created_at) VALUES(?,?,?,?)`, j.JobID, string(j.State), j.LastSummary, j.UpdatedAt.Unix()); err != nil {
			return err
		}
	}
	lifecycle, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
INSERT INTO jobs (
  job_id, codex_thread_id, grok_session_id, cwd, title, state, view_mode, desired_view_mode, input_owner,
  plan_digest, plan_summary, last_action, last_summary, user_cancelled, missing_marker_count, recover_fails,
  created_at, updated_at, archived_at, project_id, lifecycle
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
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
  archived_at=excluded.archived_at,
  project_id=excluded.project_id, lifecycle=excluded.lifecycle
`, j.JobID, j.CodexThreadID, j.GrokSessionID, j.Cwd, j.Title, string(j.State), string(j.ViewMode),
		string(desiredView(j)), string(j.InputOwner), j.PlanDigest, j.PlanSummary, j.LastAction, j.LastSummary, boolToInt(j.UserCancelled),
		rec.MissingMarkerCount, joinInt64(rec.RecoverFails), j.CreatedAt.Unix(), j.UpdatedAt.Unix(), nullUnix(j.ArchivedAt), nullString(j.ProjectID), string(lifecycle))
	if err != nil {
		return err
	}
	if r := rec.Accepted; r != nil {
		if _, err := tx.Exec(`INSERT INTO work_requests(request_id,job_id,prompt,planning,state,phase) VALUES(?,?,?,?, 'queued','queued') ON CONFLICT(request_id) DO NOTHING`, r.RequestID, j.JobID, r.Prompt, boolToInt(r.Planning)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE work_requests SET state=?,turn_id=? WHERE request_id=? AND state<>'completed'`, string(j.State), j.ActiveTurnID, j.RequestID); err != nil {
		return err
	}
	if rec.RequestPhase != "" {
		if _, err := tx.Exec(`UPDATE work_requests SET phase=? WHERE request_id=?`, rec.RequestPhase, j.RequestID); err != nil {
			return err
		}
	}
	if j.UserCancelled {
		if _, err := tx.Exec(`UPDATE work_requests SET state='cancelled' WHERE job_id=? AND state<>'completed'`, j.JobID); err != nil {
			return err
		}
	}
	if r := rec.Result; r != nil {
		resultState := r.State
		if resultState == "" {
			resultState = protocol.StateCompleted
		}
		if _, err := tx.Exec(`INSERT INTO turn_results(job_id,request_id,turn_id,body,stop_reason,state) VALUES(?,?,?,?,?,?)`, j.JobID, r.RequestID, r.TurnID, r.Text, r.StopReason, string(resultState)); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE work_requests SET state=?,summary=?,turn_id=? WHERE request_id=?`, string(resultState), r.Summary, r.TurnID, r.RequestID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO request_results(job_id,request_id,turn_id,summary) VALUES(?,?,?,?)`, j.JobID, r.RequestID, r.TurnID, r.Summary); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO boundary_events(job_id,event_type,summary,created_at) VALUES(?,?,?,?)`, j.JobID, "request_"+string(resultState), r.Summary, j.UpdatedAt.Unix()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetJob(id string) (Record, error) {
	row := s.db.QueryRow(`SELECT `+jobSelectCols+` FROM `+jobFrom+` WHERE jobs.job_id=?`, id)
	return scanJob(row)
}

func (s *Store) ListJobs() ([]Record, error) {
	rows, err := s.db.Query(`SELECT ` + jobSelectCols + ` FROM ` + jobFrom + ` ORDER BY jobs.updated_at DESC`)
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

const activeGroupSQL = `CASE WHEN jobs.state IN ('starting','planning','executing','recovering','plan_ready','needs_input','disconnected') THEN 1 ELSE 0 END`

func (s *Store) ListJobsPage(q protocol.ListJobsQuery) ([]Record, string, bool, error) {
	limit := protocol.ClampJobLimit(q.Limit)
	var (
		where []string
		args  []any
	)
	if q.IncludeArchived {
		where = append(where, `jobs.archived_at IS NOT NULL`)
	} else {
		where = append(where, `jobs.archived_at IS NULL`)
	}
	if st := strings.TrimSpace(q.State); st != "" && st != "all" {
		switch st {
		case "attention":
			where = append(where, `jobs.state IN ('plan_ready','needs_input')`)
		case "working":
			where = append(where, `jobs.state IN ('planning','executing','starting','recovering')`)
		default:
			where = append(where, `jobs.state = ?`)
			args = append(args, st)
		}
	}
	if view := strings.TrimSpace(q.View); view != "" && view != "all" {
		where = append(where, `jobs.view_mode = ?`)
		args = append(args, view)
	}
	if project := strings.TrimSpace(q.Project); project != "" && project != "all" {
		where = append(where, `jobs.project_id = ?`)
		args = append(args, project)
	}
	if query := strings.TrimSpace(q.Query); query != "" {
		like := "%" + strings.ToLower(query) + "%"
		where = append(where, `(lower(ifnull(jobs.title,'')) LIKE ? OR lower(ifnull(jobs.last_action,'')) LIKE ? OR lower(ifnull(jobs.grok_session_id,'')) LIKE ? OR lower(jobs.cwd) LIKE ?)`)
		args = append(args, like, like, like, like)
	}
	if strings.TrimSpace(q.Cursor) != "" {
		cur, err := protocol.DecodeJobCursor(q.Cursor)
		if err != nil {
			return nil, "", false, err
		}
		where = append(where, fmt.Sprintf(`(
  %[1]s < ? OR
  (%[1]s = ? AND jobs.updated_at < ?) OR
  (%[1]s = ? AND jobs.updated_at = ? AND jobs.created_at < ?) OR
  (%[1]s = ? AND jobs.updated_at = ? AND jobs.created_at = ? AND jobs.job_id < ?)
)`, activeGroupSQL))
		args = append(args, cur.G, cur.G, cur.U, cur.G, cur.U, cur.C, cur.G, cur.U, cur.C, cur.I)
	}
	sql := `SELECT ` + jobSelectCols + ` FROM ` + jobFrom + ` WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY ` + activeGroupSQL + ` DESC, jobs.updated_at DESC, jobs.created_at DESC, jobs.job_id DESC LIMIT ?`
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

func (s *Store) DeleteJob(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM boundary_events WHERE job_id=?`, id); err != nil {
		return err
	}
	for _, table := range []string{"job_notifications", "request_results", "work_requests", "turn_results"} {
		if _, err := tx.Exec(`DELETE FROM `+table+` WHERE job_id=?`, id); err != nil {
			return err
		}
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
	st.DefaultViewMode = string(protocol.ViewHeadless)
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
	var projectID, projectName sql.NullString
	var lifecycle string
	err := row.Scan(&rec.Job.JobID, &rec.Job.CodexThreadID, &rec.Job.GrokSessionID, &rec.Job.Cwd, &rec.Job.Title,
		&state, &view, &desired, &owner, &rec.Job.PlanDigest, &rec.Job.PlanSummary, &rec.Job.LastAction, &rec.Job.LastSummary,
		&cancelled, &rec.MissingMarkerCount, &fails, &created, &updated, &archived, &projectID, &projectName, &lifecycle)
	if err != nil {
		return Record{}, err
	}
	var meta protocol.Job
	if err := json.Unmarshal([]byte(lifecycle), &meta); err != nil {
		return Record{}, err
	}
	rec.Job.RequestID, rec.Job.Approved = meta.RequestID, meta.Approved
	rec.Job.RequestPhase = meta.RequestPhase
	rec.Job.ApprovalID, rec.Job.ApprovalConnectionID = meta.ApprovalID, meta.ApprovalConnectionID
	rec.Job.ApprovalSupportsNotes, rec.Job.ApprovalDelivery = meta.ApprovalSupportsNotes, meta.ApprovalDelivery
	rec.Job.PauseReason, rec.Job.ResultTurnID = meta.PauseReason, meta.ResultTurnID
	rec.Job.PlanVersion, rec.Job.PlanTurnID = meta.PlanVersion, meta.PlanTurnID
	rec.Job.EventCursor = meta.EventCursor
	rec.Job.ActiveTurnID = meta.ActiveTurnID
	rec.Job.LastActivityAt, rec.Job.ActivityKind = meta.LastActivityAt, meta.ActivityKind
	rec.Job.Stalled, rec.Job.StalledReason = meta.Stalled, meta.StalledReason
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
	rec.Job.ProjectID = projectID.String
	if projectName.String != "" {
		rec.Job.Project = projectName.String
	} else {
		rec.Job.Project = textutil.ProjectName(rec.Job.Cwd)
	}
	rec.RecoverFails = splitInt64(fails)
	return rec, nil
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
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
