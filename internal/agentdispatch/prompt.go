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

// ResolvePrompt returns the caller prompt. Positional args win over piped stdin.
func ResolvePrompt(args []string, stdin io.Reader, readStdin bool) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	if !readStdin || stdin == nil {
		return "", nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", wrapExitError(ExitUsage, "read dispatch prompt from stdin", err)
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
	if strings.TrimSpace(skill) == "" {
		return nil
	}
	path := skillProjectionPath(root, target, skill)
	// Use Lstat so a symlink does not silently route the projection check
	// elsewhere on disk. Agent Layer sync writes regular files; anything
	// else (symlink, directory, device, ...) means the projection tree has
	// been tampered with or corrupted and dispatch must refuse.
	info, err := os.Lstat(path)
	if err != nil {
		return exitError(ExitConfig, fmt.Sprintf(messages.DispatchMissingSkillProjectionFmt, skill, target.Name, path))
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
