package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const promptHeaderTemplate = "<!--\n  GENERATED FILE\n  Source: .agent-layer/slash-commands/%s.md\n  Regenerate: al sync\n-->\n"

// WriteVSCodePrompts generates VS Code prompt files for slash commands.
func WriteVSCodePrompts(root string, commands []config.SlashCommand) error {
	promptDir := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, promptDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		wanted[cmd.Name] = struct{}{}
		content := buildVSCodePrompt(cmd)
		path := filepath.Join(promptDir, fmt.Sprintf("%s.prompt.md", cmd.Name))
		if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}

	return removeStalePromptFiles(promptDir, wanted)
}

func buildVSCodePrompt(cmd config.SlashCommand) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("name: ")
	builder.WriteString(cmd.Name)
	builder.WriteString("\n---\n")
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, cmd.Name))
	if cmd.Body != "" {
		builder.WriteString(cmd.Body)
		if !strings.HasSuffix(cmd.Body, "\n") {
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func removeStalePromptFiles(promptDir string, wanted map[string]struct{}) error {
	entries, err := os.ReadDir(promptDir)
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
		isGenerated, err := hasGeneratedMarker(path)
		if err != nil {
			return err
		}
		if isGenerated {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
			}
		}
	}

	return nil
}

// WriteCodexSkills generates Codex skill files for slash commands.
func WriteCodexSkills(root string, commands []config.SlashCommand) error {
	skillsDir := filepath.Join(root, ".codex", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillsDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		wanted[cmd.Name] = struct{}{}
		skillDir := filepath.Join(skillsDir, cmd.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillDir, err)
		}
		path := filepath.Join(skillDir, "SKILL.md")
		content := buildCodexSkill(cmd)
		if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}

	return removeStaleSkillDirs(skillsDir, wanted)
}

// WriteAntigravitySkills generates Antigravity skill files for slash commands.
func WriteAntigravitySkills(root string, commands []config.SlashCommand) error {
	skillsDir := filepath.Join(root, ".agent", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillsDir, err)
	}

	wanted := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		wanted[cmd.Name] = struct{}{}
		skillDir := filepath.Join(skillsDir, cmd.Name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, skillDir, err)
		}
		path := filepath.Join(skillDir, "SKILL.md")
		content := buildAntigravitySkill(cmd)
		if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
		}
	}

	return removeStaleSkillDirs(skillsDir, wanted)
}

func buildCodexSkill(cmd config.SlashCommand) string {
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
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, cmd.Name))
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

// buildAntigravitySkill returns the Antigravity SKILL.md content for a slash command.
func buildAntigravitySkill(cmd config.SlashCommand) string {
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
	builder.WriteString(fmt.Sprintf(promptHeaderTemplate, cmd.Name))
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

func removeStaleSkillDirs(skillsDir string, wanted map[string]struct{}) error {
	entries, err := os.ReadDir(skillsDir)
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
		isGenerated, err := hasGeneratedMarker(skillPath)
		if err != nil {
			return err
		}
		if isGenerated {
			stale = append(stale, filepath.Join(skillsDir, name))
		}
	}

	sort.Strings(stale)
	for _, dir := range stale {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, dir, err)
		}
	}

	return nil
}

func hasGeneratedMarker(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	return strings.Contains(string(data), "GENERATED FILE"), nil
}
