package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

type skillDirEntry struct {
	name  string
	isDir bool
}

type skillReadDir func(dir string) ([]skillDirEntry, error)

type skillReadFile func(path string) ([]byte, error)

type parsedSkill struct {
	description string
	body        string
	name        string
}

type skillSource struct {
	path  string
	skill Skill
}

// LoadSkills reads .agent-layer/skills from disk and supports both source formats:
// - .agent-layer/skills/<name>.md
// - .agent-layer/skills/<name>/SKILL.md
func LoadSkills(dir string) ([]Skill, error) {
	return loadSkills(dir,
		func(path string) ([]skillDirEntry, error) {
			entries, err := os.ReadDir(path)
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
			return osReadFileFunc(path)
		},
	)
}

func loadSkills(dir string, readDir skillReadDir, readFile skillReadFile) ([]Skill, error) {
	entries, err := readDir(dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingSkillsDirFmt, dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	byName := make(map[string]skillSource)
	for _, entry := range entries {
		if strings.HasPrefix(entry.name, ".") {
			continue
		}
		if entry.isDir {
			if err := loadDirectorySkill(byName, dir, entry.name, readFile); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(entry.name, ".md") {
			continue
		}
		if err := loadFlatSkill(byName, dir, entry.name, readFile); err != nil {
			return nil, err
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

func loadFlatSkill(byName map[string]skillSource, root string, fileName string, readFile skillReadFile) error {
	path := filepath.Join(root, fileName)
	data, err := readFile(path)
	if err != nil {
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, path, err)
	}

	parsed, err := parseSkill(string(bytes.TrimPrefix(data, utf8BOM)))
	if err != nil {
		return fmt.Errorf(messages.ConfigInvalidSkillFmt, path, err)
	}

	name := strings.TrimSuffix(fileName, ".md")
	if parsed.name != "" && parsed.name != name {
		return fmt.Errorf(messages.ConfigSkillNameMismatchFmt, path, parsed.name, name)
	}

	skill := Skill{
		Name:        name,
		Description: parsed.description,
		Body:        parsed.body,
		SourcePath:  path,
	}
	return registerSkill(byName, skill)
}

func loadDirectorySkill(byName map[string]skillSource, root string, dirName string, readFile skillReadFile) error {
	skillPath := filepath.Join(root, dirName, "SKILL.md")
	data, err := readFile(skillPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
			return fmt.Errorf(messages.ConfigSkillDirMissingSkillFileFmt, filepath.Join(root, dirName))
		}
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, skillPath, err)
	}

	parsed, err := parseSkill(string(bytes.TrimPrefix(data, utf8BOM)))
	if err != nil {
		return fmt.Errorf(messages.ConfigInvalidSkillFmt, skillPath, err)
	}

	name := dirName
	if parsed.name != "" && parsed.name != dirName {
		return fmt.Errorf(messages.ConfigSkillNameMismatchFmt, skillPath, parsed.name, dirName)
	}

	skill := Skill{
		Name:        name,
		Description: parsed.description,
		Body:        parsed.body,
		SourcePath:  skillPath,
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

func parseSkill(content string) (parsedSkill, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillMissingContent)
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillMissingFrontMatter)
	}

	var fmLines []string
	foundEnd := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			foundEnd = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !foundEnd {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillUnterminatedFrontMatter)
	}

	var bodyBuilder strings.Builder
	for scanner.Scan() {
		bodyBuilder.WriteString(scanner.Text())
		bodyBuilder.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillFailedReadContentFmt, err)
	}

	description, err := parseDescription(fmLines)
	if err != nil {
		return parsedSkill{}, err
	}
	name, err := parseName(fmLines)
	if err != nil {
		return parsedSkill{}, err
	}

	body := strings.TrimPrefix(bodyBuilder.String(), "\n")
	body = strings.TrimRight(body, "\n")
	return parsedSkill{description: description, body: body, name: name}, nil
}

func parseName(lines []string) (string, error) {
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "name:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			if value == "" {
				return "", fmt.Errorf(messages.ConfigSkillNameEmpty)
			}
			if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
				return "", fmt.Errorf(messages.ConfigSkillNameInvalidMultiline)
			}
			return strings.Trim(value, "\""), nil
		}
	}
	return "", nil
}

func parseDescription(lines []string) (string, error) {
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		if value == "" {
			return "", fmt.Errorf(messages.ConfigSkillDescriptionEmpty)
		}
		if value == ">-" || value == ">" || value == "|" || value == "|+" || value == "|-" {
			var parts []string
			for j := i + 1; j < len(lines); j++ {
				if strings.HasPrefix(lines[j], "  ") {
					parts = append(parts, strings.TrimSpace(strings.TrimPrefix(lines[j], "  ")))
					continue
				}
				break
			}
			description := strings.TrimSpace(strings.Join(parts, " "))
			if description == "" {
				return "", fmt.Errorf(messages.ConfigSkillDescriptionEmpty)
			}
			return description, nil
		}
		return strings.Trim(value, "\""), nil
	}
	return "", fmt.Errorf(messages.ConfigSkillMissingDescription)
}
