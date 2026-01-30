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

func (inst *installer) updateGitignore() error {
	root := inst.root
	blockPath := filepath.Join(root, ".agent-layer", templateGitignoreBlock)
	sys := inst.sys
	blockBytes, err := sys.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadGitignoreBlockFmt, blockPath, err)
	}
	return ensureGitignore(sys, filepath.Join(root, ".gitignore"), string(blockBytes))
}

func ensureGitignore(sys System, path string, block string) error {
	block = normalizeGitignoreBlock(block)
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
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	rendered := renderGitignoreBlock(templateBlock)

	existingBytes, err := sys.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
		}
		if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
		}
		if err := sys.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	existing := normalizeGitignoreBlock(string(existingBytes))
	if existing == templateBlock || gitignoreBlockMatchesHash(existing) {
		if err := sys.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	if shouldOverwrite != nil {
		overwrite, err := shouldOverwrite(path)
		if err != nil {
			return err
		}
		if overwrite {
			if err := sys.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
				return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
			}
			return nil
		}
	}

	if recordDiff != nil {
		recordDiff(path)
	}
	return nil
}
