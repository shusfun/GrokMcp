package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"grokmcp/internal/project"
	"grokmcp/internal/protocol"
	"grokmcp/internal/textutil"
)

const projectSelectCols = `p.project_id, p.name, p.root, p.canonical_path, p.git_root, p.imported,
		p.skill_status, p.skill_version, p.prompt_template, p.created_at, p.updated_at, p.last_used_at,
		ifnull(sum(CASE WHEN j.job_id IS NOT NULL AND j.archived_at IS NULL AND j.state IN ('starting','planning','executing','recovering','plan_ready','needs_input','disconnected') THEN 1 ELSE 0 END), 0),
		ifnull(sum(CASE WHEN j.job_id IS NOT NULL AND j.archived_at IS NULL AND j.state IN ('plan_ready','needs_input') THEN 1 ELSE 0 END), 0)`

func (s *Store) migrateProjects() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS projects (
  project_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  root TEXT NOT NULL,
  canonical_path TEXT NOT NULL UNIQUE,
  git_root TEXT,
  imported INTEGER NOT NULL DEFAULT 0,
  skill_status TEXT,
  skill_version TEXT,
  prompt_template TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL
);`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`ALTER TABLE jobs ADD COLUMN project_id TEXT`); err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "duplicate column") && !strings.Contains(msg, "already exists") {
			return err
		}
	}
	_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS jobs_project_idx ON jobs(project_id)`)
	return s.backfillProjects()
}

func (s *Store) backfillProjects() error {
	rows, err := s.db.Query(`SELECT job_id, cwd, updated_at FROM jobs WHERE ifnull(project_id,'') = ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id      string
		cwd     string
		updated int64
	}
	var jobs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.cwd, &r.updated); err != nil {
			return err
		}
		jobs = append(jobs, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}
	existing, err := s.canonicalIndex()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		resolved := resolveCwd(j.cwd)
		pid := existing[resolved.CanonicalPath]
		if pid == "" {
			p := protocol.Project{
				ProjectID:     uuid.NewString(),
				Name:          resolved.Name,
				Root:          resolved.Root,
				CanonicalPath: resolved.CanonicalPath,
				GitRoot:       resolved.GitRoot,
				Imported:      false,
				CreatedAt:     protocol.UnixTime(j.updated),
				UpdatedAt:     protocol.UnixTime(j.updated),
				LastUsedAt:    protocol.UnixTime(j.updated),
			}
			if p.CreatedAt.IsZero() {
				now := time.Now().UTC()
				p.CreatedAt, p.UpdatedAt, p.LastUsedAt = now, now, now
			}
			if err := s.PutProject(p); err != nil {
				return err
			}
			pid = p.ProjectID
			existing[resolved.CanonicalPath] = pid
		} else if err := s.bumpLastUsed(pid, j.updated); err != nil {
			return err
		}
		if _, err := s.db.Exec(`UPDATE jobs SET project_id=? WHERE job_id=?`, pid, j.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) canonicalIndex() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT project_id, canonical_path FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		out[key] = id
	}
	return out, rows.Err()
}

func resolveCwd(cwd string) project.Resolved {
	r, err := project.Resolve(cwd)
	if err == nil {
		return r
	}
	name := textutil.ProjectName(cwd)
	return project.Resolved{Root: cwd, CanonicalPath: project.CanonicalKey(cwd), Name: name}
}

func (s *Store) PutProject(p protocol.Project) error {
	_, err := s.db.Exec(`
INSERT INTO projects (
  project_id, name, root, canonical_path, git_root, imported, skill_status, skill_version, prompt_template,
  created_at, updated_at, last_used_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(project_id) DO UPDATE SET
  name=excluded.name,
  root=excluded.root,
  canonical_path=excluded.canonical_path,
  git_root=excluded.git_root,
  imported=excluded.imported,
  skill_status=excluded.skill_status,
  skill_version=excluded.skill_version,
  prompt_template=excluded.prompt_template,
  updated_at=excluded.updated_at,
  last_used_at=excluded.last_used_at
`, p.ProjectID, p.Name, p.Root, p.CanonicalPath, p.GitRoot, boolToInt(p.Imported), string(p.SkillStatus), p.SkillVersion, p.PromptTemplate,
		p.CreatedAt.Unix(), p.UpdatedAt.Unix(), p.LastUsedAt.Unix())
	return err
}

func (s *Store) GetProject(id string) (protocol.Project, error) {
	row := s.db.QueryRow(`SELECT `+projectSelectCols+` FROM projects p LEFT JOIN jobs j ON j.project_id = p.project_id WHERE p.project_id=? GROUP BY p.project_id`, id)
	return scanProject(row)
}

func (s *Store) GetProjectByCanonical(key string) (protocol.Project, error) {
	row := s.db.QueryRow(`SELECT `+projectSelectCols+` FROM projects p LEFT JOIN jobs j ON j.project_id = p.project_id WHERE p.canonical_path=? GROUP BY p.project_id`, key)
	return scanProject(row)
}

func (s *Store) ListProjects() ([]protocol.Project, error) {
	rows, err := s.db.Query(`SELECT ` + projectSelectCols + ` FROM projects p LEFT JOIN jobs j ON j.project_id = p.project_id
GROUP BY p.project_id
ORDER BY (ifnull(sum(CASE WHEN j.job_id IS NOT NULL AND j.archived_at IS NULL AND j.state IN ('starting','planning','executing','recovering','plan_ready','needs_input','disconnected') THEN 1 ELSE 0 END), 0) > 0) DESC,
  p.last_used_at DESC, p.project_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]protocol.Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DemoteProject(id string, at time.Time) (protocol.Project, error) {
	res, err := s.db.Exec(`UPDATE projects SET imported=0, updated_at=? WHERE project_id=?`, at.Unix(), id)
	if err != nil {
		return protocol.Project{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return protocol.Project{}, sql.ErrNoRows
	}
	return s.GetProject(id)
}

func (s *Store) TouchProject(id string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE projects SET last_used_at=?, updated_at=? WHERE project_id=?`, at.Unix(), at.Unix(), id)
	return err
}

func (s *Store) bumpLastUsed(id string, ts int64) error {
	_, err := s.db.Exec(`UPDATE projects SET last_used_at=CASE WHEN last_used_at > ? THEN last_used_at ELSE ? END WHERE project_id=?`, ts, ts, id)
	return err
}

func (s *Store) SaveProjectPrompt(id, template string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE projects SET prompt_template=?, updated_at=? WHERE project_id=?`, template, at.Unix(), id)
	return err
}

func (s *Store) SaveProjectName(id, name string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE projects SET name=?, updated_at=? WHERE project_id=?`, name, at.Unix(), id)
	return err
}

func (s *Store) SaveProjectSkill(id string, status protocol.SkillStatus, version string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE projects SET skill_status=?, skill_version=?, updated_at=? WHERE project_id=?`, string(status), version, at.Unix(), id)
	return err
}

func scanProject(row rowScanner) (protocol.Project, error) {
	var p protocol.Project
	var imported int
	var git, skillStatus, skillVer, prompt sql.NullString
	var created, updated, last int64
	err := row.Scan(&p.ProjectID, &p.Name, &p.Root, &p.CanonicalPath, &git, &imported,
		&skillStatus, &skillVer, &prompt, &created, &updated, &last, &p.ActiveCount, &p.NeedsInputCount)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Project{}, err
		}
		return protocol.Project{}, err
	}
	p.GitRoot = git.String
	p.Imported = imported != 0
	p.SkillStatus = protocol.SkillStatus(skillStatus.String)
	p.SkillVersion = skillVer.String
	p.PromptTemplate = prompt.String
	p.CreatedAt = time.Unix(created, 0).UTC()
	p.UpdatedAt = time.Unix(updated, 0).UTC()
	p.LastUsedAt = time.Unix(last, 0).UTC()
	return p, nil
}
