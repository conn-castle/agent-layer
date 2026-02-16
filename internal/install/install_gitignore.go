package install

import (
	"errors"
	"fmt"
	"io/fs"
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
	updated := updateGitignoreContent(content, block)
	if err := sys.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func writeGitignoreBlock(sys System, path string, templatePath string, perm fs.FileMode, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	return writeTemplateFileWithMatch(sys, path, templatePath, perm, shouldOverwrite, recordDiff, fileMatchesTemplateWithInfo)
}

// RepairGitignoreBlockOptions controls gitignore-block repair behavior.
type RepairGitignoreBlockOptions struct {
	System System
}

// RepairGitignoreBlock rewrites `.agent-layer/gitignore.block` from embedded templates
// and then reapplies the managed block to the repository root `.gitignore`.
func RepairGitignoreBlock(root string, opts RepairGitignoreBlockOptions) error {
	if root == "" {
		return fmt.Errorf(messages.InstallRootRequired)
	}
	sys := opts.System
	if sys == nil {
		return fmt.Errorf(messages.InstallSystemRequired)
	}
	blockBytes, err := templates.Read(templateGitignoreBlock)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templateGitignoreBlock, err)
	}
	blockPath := filepath.Join(root, ".agent-layer", templateGitignoreBlock)
	if err := sys.WriteFileAtomic(blockPath, blockBytes, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, blockPath, err)
	}
	block, err := ValidateGitignoreBlock(string(blockBytes), blockPath)
	if err != nil {
		return err
	}
	return EnsureGitignore(sys, filepath.Join(root, ".gitignore"), block)
}
