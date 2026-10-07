package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// GitignoreSystem is the minimal interface needed for gitignore operations.
type GitignoreSystem interface {
	ReadFile(name string) ([]byte, error)
	WriteFileAtomic(filename string, data []byte, perm os.FileMode) error
}

func (inst *installer) updateGitignore() error {
	root := inst.root
	blockPath := filepath.Join(root, ".agent-layer", templateGitignoreBlock)
	sys := inst.sys
	blockBytes, err := sys.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadGitignoreBlockFmt, blockPath, err)
	}
	block, err := ValidateGitignoreBlock(string(blockBytes), blockPath)
	if err != nil {
		return err
	}
	return EnsureGitignore(sys, filepath.Join(root, ".gitignore"), block)
}

// EnsureGitignore updates or creates a .gitignore file with the given block.
// It merges the block into existing content, replacing any previous agent-layer block.
// The block should contain only the template content (ignore patterns and comments);
// managed markers and headers are added automatically.
func EnsureGitignore(sys GitignoreSystem, path string, block string) error {
	block = normalizeGitignoreBlock(block)
	// Render and wrap the managed block with markers.
	block = wrapGitignoreBlock(renderGitignoreBlock(block))
	contentBytes, err := sys.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}

	if errors.Is(err, os.ErrNotExist) {
		if err := sys.WriteFileAtomic(path, []byte(block), 0o644); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	content := normalizeGitignoreBlock(string(contentBytes))
	updated, err := updateGitignoreContent(content, block, path)
	if err != nil {
		return err
	}
	if err := sys.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

// mergeGitignoreBlockTemplate returns the current template with tracking
// settings from an existing gitignore.block applied. Match, preview, and
// overwrite all compare or write this merged target.
func mergeGitignoreBlockTemplate(existing []byte, templateData []byte) ([]byte, error) {
	settings, err := ParseGitignoreTrackingSettings(string(existing))
	if err != nil {
		return nil, err
	}
	merged, err := ApplyGitignoreTrackingSettings(string(templateData), settings)
	if err != nil {
		return nil, err
	}
	return []byte(merged), nil
}

// RepairGitignoreBlockOptions controls gitignore-block repair behavior.
type RepairGitignoreBlockOptions struct {
	System System
}

// RepairGitignoreBlock rewrites `.agent-layer/gitignore.block` from embedded templates,
// keeping any tracking choice the existing block states, and then reapplies the
// managed block to the repository root `.gitignore`.
func RepairGitignoreBlock(root string, opts RepairGitignoreBlockOptions) error {
	if root == "" {
		return fmt.Errorf(messages.InstallRootRequired)
	}
	sys := opts.System
	if sys == nil {
		return fmt.Errorf(messages.InstallSystemRequired)
	}
	templateData, err := templates.Read(templateGitignoreBlock)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templateGitignoreBlock, err)
	}
	blockPath := filepath.Join(root, ".agent-layer", templateGitignoreBlock)
	existing, err := sys.ReadFile(blockPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedReadFmt, blockPath, err)
	}
	blockBytes, err := keepGitignoreTrackingChoices(templateData, existing)
	if err != nil {
		return err
	}
	if err := sys.WriteFileAtomic(blockPath, blockBytes, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, blockPath, err)
	}
	block, err := ValidateGitignoreBlock(string(blockBytes), blockPath)
	if err != nil {
		return err
	}
	return EnsureGitignore(sys, filepath.Join(root, ".gitignore"), block)
}

// keepGitignoreTrackingChoices applies each tracking choice that the existing
// block states with exactly one commented or uncommented pattern line to the
// template. A missing or ambiguous pattern keeps the template default, so a
// mangled block never un-ignores .agent-layer/ without evidence.
func keepGitignoreTrackingChoices(templateData []byte, existing []byte) ([]byte, error) {
	block := string(templateData)
	for _, pattern := range []string{AgentLayerGitignorePattern, DocsAgentLayerGitignorePattern} {
		tracked, present, err := gitignorePatternIsTracked(string(existing), pattern)
		if err != nil || !present {
			continue
		}
		block, err = setGitignorePatternTracked(block, pattern, tracked)
		if err != nil {
			return nil, err
		}
	}
	return []byte(block), nil
}
