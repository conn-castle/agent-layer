package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

type skillDirEntry struct {
	name  string
	isDir bool
	mode  os.FileMode
}

type skillReadDir func(dir string) ([]skillDirEntry, error)

type skillReadTree func(dir string) (skilltree.Tree, error)

type skillSource struct {
	path  string
	skill Skill
}

// LoadSkills reads .agent-layer/skills from disk.
// Supported source format:
// - .agent-layer/skills/<name>/SKILL.md
// Flat-format .agent-layer/skills/<name>.md files are rejected with actionable errors.
// Directories without a supported skill file also fail loudly.
func LoadSkills(dir string) ([]Skill, error) {
	return loadSkills(dir,
		func(path string) ([]skillDirEntry, error) {
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			out := make([]skillDirEntry, 0, len(entries))
			for _, entry := range entries {
				info, err := entry.Info()
				if err != nil {
					return nil, err
				}
				out = append(out, skillDirEntry{name: entry.Name(), isDir: entry.IsDir(), mode: info.Mode()})
			}
			return out, nil
		},
		func(path string) (skilltree.Tree, error) {
			return skilltree.Read(skilltree.OSFS{}, path)
		},
	)
}

func loadSkills(dir string, readDir skillReadDir, readTree skillReadTree) ([]Skill, error) {
	entries, err := readDir(dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingSkillsDirFmt, dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	byName := make(map[string]skillSource)
	for _, entry := range entries {
		if skilltree.IsIgnoredName(entry.name) {
			continue
		}
		if !entry.isDir && !entry.mode.IsRegular() {
			return nil, fmt.Errorf("%s is a %s; skill source tiers may contain only directories and regular files", filepath.Join(dir, entry.name), sourceNodeType(entry.mode))
		}
		if !entry.isDir && strings.HasSuffix(entry.name, ".md") {
			name := strings.TrimSuffix(entry.name, ".md")
			return nil, fmt.Errorf(messages.ConfigSkillFlatFormatUnsupportedFmt, name, filepath.Join(dir, entry.name))
		}
	}
	for _, entry := range entries {
		if skilltree.IsIgnoredName(entry.name) {
			continue
		}
		if entry.isDir {
			if err := loadDirectorySkill(byName, dir, entry.name, readTree); err != nil {
				return nil, err
			}
			continue
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		skills = append(skills, byName[name].skill)
	}
	return skills, nil
}

func sourceNodeType(mode os.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode&os.ModeNamedPipe != 0:
		return "named pipe"
	case mode&os.ModeSocket != 0:
		return "socket"
	case mode&os.ModeDevice != 0:
		return "device"
	default:
		return "unsupported filesystem node"
	}
}

func loadDirectorySkill(byName map[string]skillSource, root string, dirName string, readTree skillReadTree) error {
	skillDirPath := filepath.Join(root, dirName)
	tree, err := readTree(skillDirPath)
	if err != nil {
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, skillDirPath, err)
	}
	info, err := skilltree.ValidateSkill(tree, filepath.ToSlash(skillDirPath))
	if err != nil {
		return fmt.Errorf(messages.ConfigInvalidSkillFmt, filepath.Join(skillDirPath, skillManifestName), err)
	}
	skillPath := filepath.Join(skillDirPath, skillManifestName)

	skill := Skill{
		Name:        info.Name,
		Description: info.Description,
		SourcePath:  skillPath,
		SourceDir:   skillDirPath,
		Tree:        tree,
	}
	return registerSkill(byName, skill)
}

func registerSkill(byName map[string]skillSource, skill Skill) error {
	if existing, ok := byName[skill.Name]; ok {
		return fmt.Errorf(messages.ConfigSkillDuplicateNameFmt, skill.Name, existing.path, skill.SourcePath)
	}
	byName[skill.Name] = skillSource{path: skill.SourcePath, skill: skill}
	return nil
}
