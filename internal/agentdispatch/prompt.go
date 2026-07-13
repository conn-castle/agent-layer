package agentdispatch

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// MaxStdinPromptBytes caps stdin reads when resolving the dispatch prompt so
// a runaway producer cannot exhaust process memory. 10 MiB is well above any
// realistic agent prompt and below typical container memory budgets.
const MaxStdinPromptBytes = 10 * 1024 * 1024

// ResolvePrompt returns the caller prompt. Positional args win over piped stdin.
func ResolvePrompt(args []string, stdin io.Reader, readStdin bool) (string, error) {
	if len(args) > 0 {
		prompt := strings.Join(args, " ")
		if len(prompt) > MaxStdinPromptBytes {
			return "", exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(prompt), MaxStdinPromptBytes))
		}
		return prompt, nil
	}
	if !readStdin || stdin == nil {
		return "", nil
	}
	data, err := io.ReadAll(io.LimitReader(stdin, MaxStdinPromptBytes+1))
	if err != nil {
		return "", wrapExitError(ExitUsage, "read dispatch prompt from stdin", err)
	}
	if len(data) > MaxStdinPromptBytes {
		return "", exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(data), MaxStdinPromptBytes))
	}
	return string(data), nil
}

// BuildChildPrompt validates the optional skill and returns exact target-native
// prompt bytes for the selected target.
func BuildChildPrompt(project *config.ProjectConfig, target string, prompt string, skill string) ([]byte, error) {
	normalizedSkill := strings.TrimSpace(skill)
	if strings.TrimSpace(prompt) == "" && normalizedSkill == "" {
		return nil, exitError(ExitUsage, messages.DispatchPromptOrSkillRequired)
	}
	if normalizedSkill == "" {
		return []byte(prompt), nil
	}
	targetInfo, ok := lookupTarget(target)
	if !ok {
		return nil, exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, target))
	}
	if !projectHasSkill(project, normalizedSkill) {
		return nil, exitError(ExitConfig, fmt.Sprintf(messages.DispatchMissingSkillFmt, normalizedSkill))
	}
	reference := targetInfo.SkillPrefix + normalizedSkill
	if prompt == "" {
		return []byte(reference), nil
	}
	return []byte(reference + "\n" + prompt), nil
}

func projectHasSkill(project *config.ProjectConfig, skill string) bool {
	if project == nil {
		return false
	}
	for _, candidate := range project.Skills {
		if candidate.Name == skill {
			return true
		}
	}
	return false
}

func validateSkillProjection(root string, target targetMeta, skill string) error {
	// Trim before path construction so a templated `--skill " foo "` does
	// not produce a path like ".claude/skills/ foo /SKILL.md" that fails a
	// projection lookup the caller never intended. BuildChildPrompt already
	// trims before its own validation; mirror that here for consistency.
	normalized := strings.TrimSpace(skill)
	if normalized == "" {
		return nil
	}
	path := skillProjectionPath(root, target, normalized)
	// Use Lstat so a symlink does not silently route the projection check
	// elsewhere on disk. Agent Layer sync writes regular files; anything
	// else (symlink, directory, device, ...) means the projection tree has
	// been tampered with or corrupted and dispatch must refuse.
	info, err := os.Lstat(path)
	if err != nil {
		return exitError(ExitConfig, fmt.Sprintf(messages.DispatchMissingSkillProjectionFmt, normalized, target.Name, path))
	}
	if !info.Mode().IsRegular() {
		return exitError(ExitConfig, fmt.Sprintf(messages.DispatchSkillProjectionNotRegularFmt, path, info.Mode().String()))
	}
	return nil
}

func skillProjectionPath(root string, target targetMeta, skill string) string {
	if target.SharedSkillProject {
		return filepath.Join(root, ".agents", "skills", skill, "SKILL.md")
	}
	return filepath.Join(root, ".claude", "skills", skill, "SKILL.md")
}
