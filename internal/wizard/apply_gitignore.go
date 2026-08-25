package wizard

import (
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
	agentLayerGitignorePattern     = install.AgentLayerGitignorePattern
	docsAgentLayerGitignorePattern = install.DocsAgentLayerGitignorePattern
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
	settings, err := install.ParseGitignoreTrackingSettings(content)
	if err != nil {
		return err
	}
	choices.TrackAgentLayerDir = settings.TrackAgentLayerDir
	choices.TrackDocsAgentLayerDir = settings.TrackDocsAgentLayerDir
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
	return install.ApplyGitignoreTrackingSettings(content, install.GitignoreTrackingSettings{
		TrackAgentLayerDir:     choices.TrackAgentLayerDir,
		TrackDocsAgentLayerDir: choices.TrackDocsAgentLayerDir,
	})
}
