package supervisor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"grokmcp/internal/project"
	"grokmcp/internal/protocol"
)

func (s *Service) ImportProject(_ context.Context, path string, installSkill bool) (protocol.Project, error) {
	resolved, err := project.Resolve(path)
	if err != nil {
		return protocol.Project{}, err
	}
	now := s.clock.Now()
	p, err := s.store.GetProjectByCanonical(resolved.CanonicalPath)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return protocol.Project{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		p = protocol.Project{
			ProjectID:     s.ids.ProjectID(),
			Name:          resolved.Name,
			Root:          resolved.Root,
			CanonicalPath: resolved.CanonicalPath,
			GitRoot:       resolved.GitRoot,
			Imported:      true,
			CreatedAt:     now,
			UpdatedAt:     now,
			LastUsedAt:    now,
		}
	} else {
		p.Imported = true
		p.Root = resolved.Root
		p.GitRoot = resolved.GitRoot
		p.CanonicalPath = resolved.CanonicalPath
		if strings.TrimSpace(p.Name) == "" {
			p.Name = resolved.Name
		}
		p.UpdatedAt = now
	}
	if err := s.store.PutProject(p); err != nil {
		return protocol.Project{}, err
	}
	var skillErr error
	if installSkill {
		skillErr = project.Install(p.Root)
	}
	out, err := s.refreshProject(p.ProjectID, true)
	if err != nil {
		return out, err
	}
	if skillErr == nil || errors.Is(skillErr, project.ErrSkillConflict) {
		return out, nil
	}
	if out.SkillStatus != protocol.SkillConflict {
		out.SkillStatus = protocol.SkillError
		out.SkillMessage = skillErr.Error()
		_ = s.store.SaveProjectSkill(out.ProjectID, out.SkillStatus, out.SkillVersion, s.clock.Now())
	}
	return out, nil
}

func (s *Service) ListProjects(context.Context) ([]protocol.Project, error) {
	list, err := s.store.ListProjects()
	if err != nil {
		return nil, err
	}
	for i := range list {
		project.ApplyProbe(&list[i])
	}
	if list == nil {
		list = []protocol.Project{}
	}
	return list, nil
}

func (s *Service) GetProject(_ context.Context, id string) (protocol.Project, error) {
	return s.refreshProject(id, false)
}

func (s *Service) RemoveProject(_ context.Context, id string) (protocol.Project, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.Project{}, fmt.Errorf("%w: %s", project.ErrProjectNotFound, id)
		}
		return protocol.Project{}, err
	}
	if p.Imported {
		if _, err := s.store.DemoteProject(id, s.clock.Now()); err != nil {
			return protocol.Project{}, err
		}
	}
	return s.refreshProject(id, true)
}

func (s *Service) SkillStatus(_ context.Context, id string) (protocol.Project, error) {
	return s.refreshProject(id, false)
}

func (s *Service) SkillInstall(_ context.Context, id string) (protocol.Project, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		return protocol.Project{}, projectNotFound(err, id)
	}
	if err := project.Install(p.Root); err != nil {
		out, _ := s.refreshProject(id, true)
		return out, err
	}
	return s.refreshProject(id, true)
}

func (s *Service) SkillUpdate(_ context.Context, id string) (protocol.Project, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		return protocol.Project{}, projectNotFound(err, id)
	}
	if err := project.Update(p.Root); err != nil {
		out, _ := s.refreshProject(id, true)
		return out, err
	}
	return s.refreshProject(id, true)
}

func (s *Service) SkillRemove(_ context.Context, id string) (protocol.Project, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		return protocol.Project{}, projectNotFound(err, id)
	}
	if err := project.Remove(p.Root); err != nil {
		out, _ := s.refreshProject(id, true)
		return out, err
	}
	_ = s.store.SaveProjectSkill(id, protocol.SkillMissing, "", s.clock.Now())
	return s.refreshProject(id, true)
}

func (s *Service) GeneratePrompt(_ context.Context, req protocol.GeneratePromptRequest) (protocol.PromptResult, error) {
	p, err := s.store.GetProject(req.ProjectID)
	if err != nil {
		return protocol.PromptResult{}, projectNotFound(err, req.ProjectID)
	}
	return project.Generate(p, req.Goal, req.Constraints, req.Acceptance), nil
}

func (s *Service) SavePrompt(_ context.Context, req protocol.SavePromptRequest) (protocol.Project, error) {
	if _, err := s.store.GetProject(req.ProjectID); err != nil {
		return protocol.Project{}, projectNotFound(err, req.ProjectID)
	}
	if err := s.store.SaveProjectPrompt(req.ProjectID, req.Template, s.clock.Now()); err != nil {
		return protocol.Project{}, err
	}
	return s.refreshProject(req.ProjectID, true)
}

func (s *Service) OpenProjectDir(ctx context.Context, id string) error {
	p, err := s.store.GetProject(id)
	if err != nil {
		return projectNotFound(err, id)
	}
	return s.term.OpenDirectory(ctx, p.Root)
}

func (s *Service) bindProject(projectID, cwd string) (protocol.Project, error) {
	if strings.TrimSpace(projectID) != "" {
		p, err := s.store.GetProject(projectID)
		if err != nil {
			return protocol.Project{}, projectNotFound(err, projectID)
		}
		_ = s.store.TouchProject(p.ProjectID, s.clock.Now())
		project.ApplyProbe(&p)
		return p, nil
	}
	return s.ensureDiscovered(cwd)
}

func (s *Service) ensureDiscovered(cwd string) (protocol.Project, error) {
	resolved, err := project.Resolve(cwd)
	if err != nil {
		return protocol.Project{}, err
	}
	existing, err := s.store.GetProjectByCanonical(resolved.CanonicalPath)
	if err == nil {
		_ = s.store.TouchProject(existing.ProjectID, s.clock.Now())
		project.ApplyProbe(&existing)
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return protocol.Project{}, err
	}
	now := s.clock.Now()
	p := protocol.Project{
		ProjectID:     s.ids.ProjectID(),
		Name:          resolved.Name,
		Root:          resolved.Root,
		CanonicalPath: resolved.CanonicalPath,
		GitRoot:       resolved.GitRoot,
		Imported:      false,
		CreatedAt:     now,
		UpdatedAt:     now,
		LastUsedAt:    now,
	}
	if err := s.store.PutProject(p); err != nil {
		return protocol.Project{}, err
	}
	project.ApplyProbe(&p)
	s.emitProject()
	return p, nil
}

func (s *Service) refreshProject(id string, emit bool) (protocol.Project, error) {
	p, err := s.store.GetProject(id)
	if err != nil {
		return protocol.Project{}, projectNotFound(err, id)
	}
	project.ApplyProbe(&p)
	_ = s.store.SaveProjectSkill(p.ProjectID, p.SkillStatus, p.SkillVersion, s.clock.Now())
	if emit {
		s.emitProject()
	}
	return p, nil
}

func (s *Service) emitProject() {
	s.mu.Lock()
	fns := make([]func(protocol.Event), 0, len(s.subs))
	for _, fn := range s.subs {
		fns = append(fns, fn)
	}
	s.mu.Unlock()
	ev := protocol.Event{Type: "project"}
	for _, fn := range fns {
		fn(ev)
	}
}

func projectNotFound(err error, id string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %s", project.ErrProjectNotFound, id)
	}
	return err
}
