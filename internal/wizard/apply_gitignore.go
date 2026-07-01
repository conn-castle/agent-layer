package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const gitignoreBlockRelPath = ".agent-layer/gitignore.block"

const (
	agentLayerGitignorePattern     = "/.agent-layer/"
	docsAgentLayerGitignorePattern = "/docs/agent-layer/"
)

type gitignoreBlockChangeSet struct {
	currentContent string
	nextContent    string
}

// initializeGitTrackingChoices derives the wizard's git tracking defaults from
// the managed gitignore block source for root.
func initializeGitTrackingChoices(root string, choices *Choices) error {
	content, _, err := gitignoreBlockSourceContent(root)
	if err != nil {
		return err
	}
	choices.TrackAgentLayerDir = gitignorePatternIsTracked(content, agentLayerGitignorePattern)
	choices.TrackDocsAgentLayerDir = gitignorePatternIsTracked(content, docsAgentLayerGitignorePattern)
	return nil
}

// computeGitignoreBlockChangeSet returns the managed gitignore block rewrite
// needed for the answered git tracking step.
func computeGitignoreBlockChangeSet(root string, choices *Choices) (gitignoreBlockChangeSet, error) {
	if !choices.GitTrackingTouched {
		return gitignoreBlockChangeSet{}, nil
	}
	current, exists, err := gitignoreBlockSourceContent(root)
	if err != nil {
		return gitignoreBlockChangeSet{}, err
	}
	next, err := patchGitignoreBlock(current, choices)
	if err != nil {
		return gitignoreBlockChangeSet{}, err
	}
	if exists && next == current {
		return gitignoreBlockChangeSet{}, nil
	}
	currentPreview := current
	if !exists {
		currentPreview = ""
	}
	return gitignoreBlockChangeSet{currentContent: currentPreview, nextContent: next}, nil
}

// applyGitignoreBlockChanges writes the proposed managed gitignore block source
// to root when the change set is non-empty.
func applyGitignoreBlockChanges(root string, changes gitignoreBlockChangeSet) error {
	if changes.nextContent == "" {
		return nil
	}
	path := filepath.Join(root, filepath.FromSlash(gitignoreBlockRelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(path, []byte(changes.nextContent), 0o644)
}

// buildGitignoreBlockPreview renders the gitignore source diff shown in the
// wizard's existing rewrite preview note.
func buildGitignoreBlockPreview(changes gitignoreBlockChangeSet) string {
	if changes.nextContent == "" {
		return ""
	}
	diff := strings.TrimSpace(udiff.Unified(
		gitignoreBlockRelPath+" (current)",
		gitignoreBlockRelPath+" (proposed)",
		changes.currentContent,
		changes.nextContent,
	))
	return "Gitignore source changes:\n" + diff
}

// gitignoreBlockSourceContent reads the managed gitignore block source. When it
// is missing, it returns the embedded template content and exists=false.
func gitignoreBlockSourceContent(root string) (string, bool, error) {
	path := filepath.Join(root, filepath.FromSlash(gitignoreBlockRelPath))
	exists := true
	data, err := os.ReadFile(path) // #nosec G304 -- path is the caller-resolved managed gitignore source used by the wizard.
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, err
		}
		exists = false
		data, err = templates.Read("gitignore.block")
		if err != nil {
			return "", false, err
		}
	}
	block, err := install.ValidateGitignoreBlock(string(data), path)
	if err != nil {
		return "", false, err
	}
	return block, exists, nil
}

// patchGitignoreBlock updates the two Agent Layer folder ignore entries to
// match the wizard choices while leaving unrelated lines untouched.
func patchGitignoreBlock(content string, choices *Choices) (string, error) {
	next, err := setGitignorePatternTracked(content, agentLayerGitignorePattern, choices.TrackAgentLayerDir)
	if err != nil {
		return "", err
	}
	next, err = setGitignorePatternTracked(next, docsAgentLayerGitignorePattern, choices.TrackDocsAgentLayerDir)
	if err != nil {
		return "", err
	}
	return next, nil
}

// gitignorePatternIsTracked reports whether pattern is commented out or absent
// in the managed gitignore block, meaning git can track that path.
func gitignorePatternIsTracked(content string, pattern string) bool {
	for _, line := range strings.Split(content, "\n") {
		commented, ok := gitignorePatternLineStatus(line, pattern)
		if !ok {
			// An active entry carrying an inline trailing comment (e.g.
			// "/.agent-layer/  # keep ignored") is still an active ignore, not
			// tracked. Mirror setGitignorePatternTracked so the default reflects
			// the current ignore semantics instead of flipping the path to
			// tracked when the user accepts defaults.
			if relatedActiveGitignorePatternLine(strings.TrimSpace(line), pattern) {
				return false
			}
			continue
		}
		return commented
	}
	return true
}

// setGitignorePatternTracked comments or uncomments pattern in content. Missing
// tracked patterns are left absent; missing ignored patterns are appended.
func setGitignorePatternTracked(content string, pattern string, tracked bool) (string, error) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	matches := 0
	for i, line := range lines {
		commented, ok := gitignorePatternLineStatus(line, pattern)
		if !ok {
			if relatedActiveGitignorePatternLine(strings.TrimSpace(line), pattern) {
				matches++
				if tracked {
					lines[i] = "# " + pattern
				} else {
					lines[i] = pattern
				}
			}
			continue
		}
		matches++
		if tracked && !commented {
			lines[i] = "# " + pattern
		} else if !tracked && commented {
			lines[i] = pattern
		}
	}
	if matches > 1 {
		return "", fmt.Errorf("multiple gitignore entries for %s in %s", pattern, gitignoreBlockRelPath)
	}
	if matches == 0 && !tracked {
		lines = append(lines, pattern)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// gitignorePatternLineStatus identifies whether line is an active or commented
// exact match for pattern.
func gitignorePatternLineStatus(line string, pattern string) (bool, bool) {
	trimmed := strings.TrimSpace(line)
	if activeGitignorePatternLine(trimmed, pattern) {
		return false, true
	}
	if strings.HasPrefix(trimmed, "#") && commentedGitignorePatternLine(strings.TrimSpace(strings.TrimPrefix(trimmed, "#")), pattern) {
		return true, true
	}
	return false, false
}

func activeGitignorePatternLine(line string, pattern string) bool {
	return line == pattern
}

func relatedActiveGitignorePatternLine(line string, pattern string) bool {
	after, ok := strings.CutPrefix(line, pattern)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(after), "#")
}

func commentedGitignorePatternLine(line string, pattern string) bool {
	if line == pattern {
		return true
	}
	after, ok := strings.CutPrefix(line, pattern)
	if !ok {
		return false
	}
	trailing := strings.TrimSpace(after)
	return strings.HasPrefix(trailing, "#") || strings.HasPrefix(trailing, "-")
}
