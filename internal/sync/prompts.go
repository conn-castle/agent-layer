package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const promptHeaderTemplate = "<!--\n  GENERATED FILE\n  Source: %s\n  Regenerate: al sync\n-->\n"

const (
	generatedMarkerHeader     = "GENERATED FILE"
	generatedMarkerSource     = "Source: .agent-layer/"
	generatedMarkerRegenerate = "Regenerate: al sync"
)

// WriteVSCodePrompts generates VS Code prompt files for skills.
func WriteVSCodePrompts(sys System, root string, commands []config.Skill) error {
	promptDir := filepath.Join(root, ".vscode", "prompts")
	if err := sys.MkdirAll(promptDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, promptDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		wanted[cmd.Name] = struct{}{}
		content := buildVSCodePrompt(cmd)
		path := filepath.Join(promptDir, fmt.Sprintf("%s.prompt.md", cmd.Name))
		if err := sys.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}

	return removeStalePromptFiles(sys, promptDir, wanted)
}

func buildVSCodePrompt(cmd config.Skill) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(cmd.Name)
	builder.WriteString("\n---\n")
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, generatedSkillSourcePath(cmd)))
	if cmd.Body != "" {
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func removeStalePromptFiles(sys System, promptDir string, wanted map[string]struct{}) error {
	entries, err := sys.ReadDir(promptDir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, promptDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".prompt.md") {
			continue
		}
		base := strings.TrimSuffix(name, ".prompt.md")
		if _, ok := wanted[base]; ok {
			continue
		}
		path := filepath.Join(promptDir, name)
		isGenerated, err := hasGeneratedMarker(sys, path)
		if err != nil {
			return err
		}
		if isGenerated {
			if err := sys.Remove(path); err != nil {
				return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
			}
		}
	}

	return nil
}

// skillContentBuilder builds skill file content for a skill.
type skillContentBuilder func(cmd config.Skill) string

// writeSkillFiles generates skill files in the specified directory using the provided content builder.
func writeSkillFiles(sys System, skillsDir string, commands []config.Skill, buildContent skillContentBuilder) error {
	if err := sys.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillsDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		wanted[cmd.Name] = struct{}{}
		skillDir := filepath.Join(skillsDir, cmd.Name)
		if err := sys.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillDir, err)
		}
		path := filepath.Join(skillDir, "SKILL.md")
		content := buildContent(cmd)
		if err := sys.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}

	return removeStaleSkillDirs(sys, skillsDir, wanted)
}

// WriteCodexSkills generates Codex skill files for skills.
func WriteCodexSkills(sys System, root string, commands []config.Skill) error {
	skillsDir := filepath.Join(root, ".codex", "skills")
	return writeSkillFiles(sys, skillsDir, commands, buildCodexSkill)
}

// WriteAntigravitySkills generates Antigravity skill files for skills.
func WriteAntigravitySkills(sys System, root string, commands []config.Skill) error {
	skillsDir := filepath.Join(root, ".agent", "skills")
	return writeSkillFiles(sys, skillsDir, commands, buildAntigravitySkill)
}

func buildCodexSkill(cmd config.Skill) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(cmd.Name)
	builder.WriteString("\n")
	builder.WriteString("description: >-\n")
	wrapped := wrapDescription(cmd.Description, 72)
	for _, line := range wrapped {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	builder.WriteString("---\n\n")
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, generatedSkillSourcePath(cmd)))
	builder.WriteString("\n# ")
	builder.WriteString(cmd.Name)
	builder.WriteString("\n\n")
	builder.WriteString(cmd.Description)
	builder.WriteString("\n\n")
	if cmd.Body != "" {
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// buildAntigravitySkill returns the Antigravity SKILL.md content for a skill.
func buildAntigravitySkill(cmd config.Skill) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(cmd.Name)
	builder.WriteString("\n")
	builder.WriteString("description: >-\n")
	wrapped := wrapDescription(cmd.Description, 72)
	for _, line := range wrapped {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	builder.WriteString("---\n\n")
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, generatedSkillSourcePath(cmd)))
	if cmd.Body != "" {
		builder.WriteString("\n")
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func wrapDescription(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	var current string
	for _, word := range words {
		if current == "" {
			current = word
			continue
		}
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current = current + " " + word
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func generatedSkillSourcePath(cmd config.Skill) string {
	defaultPath := filepath.ToSlash(filepath.Join(".agent-layer", "skills", cmd.Name+".md"))
	source := strings.TrimSpace(cmd.SourcePath)
	if source == "" {
		return defaultPath
	}
	normalized := filepath.ToSlash(source)
	if strings.HasPrefix(normalized, ".agent-layer/") {
		return normalized
	}
	marker := "/.agent-layer/"
	if idx := strings.Index(normalized, marker); idx >= 0 {
		return normalized[idx+1:]
	}
	return defaultPath
}

func removeStaleSkillDirs(sys System, skillsDir string, wanted map[string]struct{}) error {
	entries, err := sys.ReadDir(skillsDir)
	if err != nil {
		return fmt.Errorf(messages.SyncReadFailedFmt, skillsDir, err)
	}

	var stale []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, ok := wanted[name]; ok {
			continue
		}
		skillPath := filepath.Join(skillsDir, name, "SKILL.md")
		isGenerated, err := hasGeneratedMarker(sys, skillPath)
		if err != nil {
			return err
		}
		if isGenerated {
			stale = append(stale, filepath.Join(skillsDir, name))
		}
	}

	sort.Strings(stale)
	for _, dir := range stale {
		if err := sys.RemoveAll(dir); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err)
		}
	}

	return nil
}

func hasGeneratedMarker(sys System, path string) (bool, error) {
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	content := string(data)
	return strings.Contains(content, generatedMarkerHeader) &&
		strings.Contains(content, generatedMarkerSource) &&
		strings.Contains(content, generatedMarkerRegenerate), nil
}
