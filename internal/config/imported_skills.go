package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

// LoadImportedSkillsFS reads .agent-layer/imported-skills from fsys. root
// resolves absolute paths; dir is used for error messages; lock names the
// directories the import machinery owns.
//
// Ownership is checked before contents, so a directory nobody owns is reported
// as an unmanaged directory rather than as a malformed skill. That is the
// difference between telling the user to adopt or remove it and telling them to
// repair a file they never wrote.
func LoadImportedSkillsFS(fsys fs.FS, root string, dir string, lock *SkillImportLock) ([]Skill, error) {
	if err := rejectOrphanImportedDirectories(fsys, root, dir, lock); err != nil {
		return nil, err
	}
	return loadImportedSkills(
		dir,
		func(path string) ([]skillDirEntry, error) {
			entries, err := readDirFS(fsys, root, path)
			if err != nil {
				return nil, err
			}
			out := make([]skillDirEntry, 0, len(entries))
			for _, entry := range entries {
				out = append(out, skillDirEntry{name: entry.Name(), isDir: entry.IsDir()})
			}
			return out, nil
		},
		func(path string) ([]byte, error) {
			return readFileFS(fsys, root, path)
		},
	)
}

func loadImportedSkills(dir string, readDir skillReadDir, readFile skillReadFile) ([]Skill, error) {
	if _, err := readDir(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read imported skills directory %s: %w", dir, err)
	}
	skills, err := loadSkills(dir, readDir, readFile)
	if err != nil {
		return nil, err
	}
	for i := range skills {
		if filepath.Base(skills[i].SourcePath) != skillManifestName {
			return nil, fmt.Errorf("imported skill %s must use canonical %s", skills[i].SourceDir, skillManifestName)
		}
		raw, readErr := readFile(skills[i].SourcePath)
		if readErr != nil {
			return nil, fmt.Errorf("read imported skill %s: %w", skills[i].SourcePath, readErr)
		}
		parsed, parseErr := skillvalidator.ParseSkillSourceBytes(skills[i].SourcePath, raw)
		if parseErr != nil {
			return nil, parseErr
		}
		for _, finding := range skillvalidator.ValidateParsedSkill(parsed) {
			if finding.Code == skillvalidator.FindingCodeSizeRecommendation {
				continue
			}
			return nil, fmt.Errorf("invalid imported skill %s: %s", skills[i].SourcePath, finding.Message)
		}
		skills[i].Imported = true
	}
	return skills, nil
}

// rejectOrphanImportedDirectories fails when the fully managed imported-skills
// root contains a top-level directory that no lock entry owns.
func rejectOrphanImportedDirectories(fsys fs.FS, root string, dir string, lock *SkillImportLock) error {
	entries, err := readDirFS(fsys, root, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read imported skills directory %s: %w", dir, err)
	}
	owned := make(map[string]struct{})
	if lock != nil {
		for _, entry := range lock.Entries {
			owned[NormalizeSkillImportName(entry.SkillName)] = struct{}{}
		}
	}
	var orphans []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if _, ok := owned[NormalizeSkillImportName(entry.Name())]; ok {
			continue
		}
		orphans = append(orphans, filepath.Join(dir, entry.Name()))
	}
	if len(orphans) == 0 {
		return nil
	}
	sort.Strings(orphans)
	return fmt.Errorf(
		"%s is fully managed but %s has no entry in %s; adopt it by moving it into .agent-layer/skills/, or remove it",
		filepath.Join(".agent-layer", ImportedSkillsDirName),
		strings.Join(orphans, ", "),
		filepath.Join(".agent-layer", SkillImportLockFileName),
	)
}

// MergeSkillSources combines user-managed and imported skills into the single
// projection set. A name present in both tiers fails with both paths rather than
// letting one source silently shadow the other.
func MergeSkillSources(userSkills []Skill, importedSkills []Skill) ([]Skill, error) {
	byName := make(map[string]Skill, len(userSkills)+len(importedSkills))
	for _, skill := range userSkills {
		byName[NormalizeSkillImportName(skill.Name)] = skill
	}

	for _, skill := range importedSkills {
		normalized := NormalizeSkillImportName(skill.Name)
		if existing, ok := byName[normalized]; ok {
			return nil, fmt.Errorf(
				"skill name %q exists in both %s and %s; move or rename one source, or narrow the import selector so only one owns the name",
				skill.Name, existing.SourceDir, skill.SourceDir,
			)
		}
		byName[normalized] = skill
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	merged := make([]Skill, 0, len(names))
	for _, name := range names {
		merged = append(merged, byName[name])
	}
	return merged, nil
}
