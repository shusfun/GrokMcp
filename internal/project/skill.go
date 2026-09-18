package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"grokmcp/internal/protocol"
)

const (
	SkillRel     = ".agents/skills/grok-supervisor-tools/SKILL.md"
	SkillDirRel  = ".agents/skills/grok-supervisor-tools"
	SkillVersion = "1"
	markerPrefix = "<!-- grok-supervisor-tools managed version="
	markerSuffix = " -->"
)

func SkillPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(SkillRel))
}

func SkillDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(SkillDirRel))
}

func Marker(version string) string {
	return markerPrefix + version + markerSuffix
}

func ParseManaged(body string) (version string, ok bool) {
	i := strings.Index(body, markerPrefix)
	if i < 0 {
		return "", false
	}
	rest := body[i+len(markerPrefix):]
	j := strings.Index(rest, markerSuffix)
	if j < 0 {
		j = strings.Index(rest, "-->")
		if j < 0 {
			return "", false
		}
	}
	ver := strings.TrimSpace(rest[:j])
	return ver, ver != ""
}

func Probe(root string) protocol.Project {
	p := SkillPath(root)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return protocol.Project{SkillStatus: protocol.SkillMissing}
		}
		return protocol.Project{
			SkillStatus:  protocol.SkillConflict,
			SkillMessage: err.Error(),
		}
	}
	ver, managed := ParseManaged(string(b))
	if !managed {
		return protocol.Project{
			SkillStatus:  protocol.SkillConflict,
			SkillMessage: "同路径已有用户文件，未覆盖。请改名或移走后再安装。",
		}
	}
	if ver != SkillVersion {
		return protocol.Project{SkillStatus: protocol.SkillOutdated, SkillVersion: ver}
	}
	return protocol.Project{SkillStatus: protocol.SkillInstalled, SkillVersion: ver}
}

func ApplyProbe(p *protocol.Project) {
	got := Probe(p.Root)
	p.SkillStatus = got.SkillStatus
	p.SkillVersion = got.SkillVersion
	p.SkillMessage = got.SkillMessage
}

func ManagedBody() string {
	return Marker(SkillVersion) + "\n" + skillContent
}

func Install(root string) error {
	return writeManaged(root, false)
}

func Update(root string) error {
	return writeManaged(root, true)
}

func writeManaged(root string, requireExistingManaged bool) error {
	p := SkillPath(root)
	b, err := os.ReadFile(p)
	if err == nil {
		_, managed := ParseManaged(string(b))
		if !managed {
			return ErrSkillConflict
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if requireExistingManaged {
		return fmt.Errorf("skill not installed")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(ManagedBody()), 0o644)
}

func Remove(root string) error {
	p := SkillPath(root)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, managed := ParseManaged(string(b)); !managed {
		return ErrSkillNotManaged
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := SkillDir(root)
	_ = os.Remove(dir) // only if empty
	return nil
}
