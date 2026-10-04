package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const (
	projectionStageSuffix   = ".al-stage"
	projectionDiscardSuffix = ".al-discard"
)

// WriteAgentSkills replaces the exclusively owned .agents/skills projection
// with the complete canonical skill snapshot.
func WriteAgentSkills(sys System, root string, skills []config.Skill) error {
	return writeSkillRoot(sys, filepath.Join(root, ".agents", "skills"), skills, "", false)
}

// WriteClaudeSkills replaces the exclusively owned .claude/skills projection
// with the complete canonical skill snapshot.
func WriteClaudeSkills(sys System, root string, skills []config.Skill) error {
	return writeSkillRoot(sys, filepath.Join(root, ".claude", "skills"), skills, "", false)
}

func writeClaudeSkillLinksWithDiscard(sys System, root string, skills []config.Skill, retainPrevious bool) error {
	claudeDir := filepath.Join(root, ".claude")
	linkRoot := filepath.Join("..", "..", ".agents", "skills")
	info, err := sys.Lstat(claudeDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect Claude skill directory %s: %w", claudeDir, err)
	}
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		// A relocated .claude directory changes the base of relative links.
		linkRoot, err = filepath.Abs(filepath.Join(root, ".agents", "skills"))
		if err != nil {
			return fmt.Errorf("resolve shared skill directory: %w", err)
		}
	}
	return writeSkillRoot(sys, filepath.Join(claudeDir, "skills"), skills, linkRoot, retainPrevious)
}

// writeLinkedSkillRoots retains the old shared root while Claude links are
// replaced, so a failed Claude write can restore their previous targets.
func writeLinkedSkillRoots(sys System, root string, skills []config.Skill) error {
	sharedRoot := filepath.Join(root, ".agents", "skills")
	if err := writeSkillRoot(sys, sharedRoot, skills, "", true); err != nil {
		return err
	}
	if err := writeClaudeSkillLinksWithDiscard(sys, root, skills, true); err != nil {
		if restoreErr := restoreSkillRoot(sys, sharedRoot); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	if err := sys.RemoveAll(sharedRoot + projectionDiscardSuffix); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, sharedRoot+projectionDiscardSuffix, err)
	}
	claudeRoot := filepath.Join(root, ".claude", "skills")
	if err := sys.RemoveAll(claudeRoot + projectionDiscardSuffix); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, claudeRoot+projectionDiscardSuffix, err)
	}
	return nil
}

func restoreSkillRoot(sys System, liveRoot string) error {
	discardRoot := liveRoot + projectionDiscardSuffix
	_, err := sys.Lstat(discardRoot)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncReadFailedFmt, discardRoot, err)
	}
	if os.IsNotExist(err) {
		if err := sys.RemoveAll(liveRoot); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, liveRoot, err)
		}
		return nil
	}

	stageRoot := liveRoot + projectionStageSuffix
	if err := sys.Rename(liveRoot, stageRoot); err != nil {
		return fmt.Errorf(messages.SyncRenameFailedFmt, liveRoot, stageRoot, err)
	}
	if err := sys.Rename(discardRoot, liveRoot); err != nil {
		restoreErr := fmt.Errorf("restore skill root %s: %w", liveRoot, err)
		if replaceErr := sys.Rename(stageRoot, liveRoot); replaceErr != nil {
			return errors.Join(restoreErr, fmt.Errorf("recover published skill root %s: %w", liveRoot, replaceErr))
		}
		return restoreErr
	}
	if err := sys.RemoveAll(stageRoot); err != nil {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, stageRoot, err)
	}
	return nil
}

// writeSkillRoot builds a complete sibling staging tree before it touches the
// live root. A failed publish restores the previous root so existing Claude links
// remain usable. Retry rebuilds disposable staging and discard directories.
func writeSkillRoot(sys System, liveRoot string, skills []config.Skill, linkRoot string, retainPrevious bool) error {
	validated, err := validateProjectionSkills(skills)
	if err != nil {
		return err
	}

	stageRoot := liveRoot + projectionStageSuffix
	discardRoot := liveRoot + projectionDiscardSuffix
	for _, scratch := range []string{stageRoot, discardRoot} {
		if err := sys.RemoveAll(scratch); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, scratch, err)
		}
	}
	if err := sys.MkdirAll(stageRoot, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, stageRoot, err)
	}
	for _, skill := range validated {
		dest := filepath.Join(stageRoot, skill.Name)
		var err error
		if linkRoot != "" {
			target := filepath.Join(linkRoot, skill.Name)
			if err = sys.Symlink(target, dest); err != nil {
				err = fmt.Errorf("link Claude skill %s to %s: %w", dest, target, err)
			}
		} else {
			err = materializeSkillTree(sys, dest, skill.Tree)
		}
		if err != nil {
			_ = sys.RemoveAll(stageRoot)
			return err
		}
	}

	hadLiveRoot := false
	if _, statErr := sys.Lstat(liveRoot); statErr == nil {
		if err := sys.Rename(liveRoot, discardRoot); err != nil {
			_ = sys.RemoveAll(stageRoot)
			return fmt.Errorf(messages.SyncRenameFailedFmt, liveRoot, discardRoot, err)
		}
		hadLiveRoot = true
	} else if !os.IsNotExist(statErr) {
		_ = sys.RemoveAll(stageRoot)
		return fmt.Errorf(messages.SyncReadFailedFmt, liveRoot, statErr)
	}
	if err := sys.Rename(stageRoot, liveRoot); err != nil {
		publishErr := fmt.Errorf(messages.SyncRenameFailedFmt, stageRoot, liveRoot, err)
		if hadLiveRoot {
			if restoreErr := sys.Rename(discardRoot, liveRoot); restoreErr != nil {
				return errors.Join(publishErr, fmt.Errorf("restore skill root %s: %w", liveRoot, restoreErr))
			}
		}
		return publishErr
	}
	if !retainPrevious {
		if err := sys.RemoveAll(discardRoot); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, discardRoot, err)
		}
	}
	return nil
}

func validateProjectionSkills(skills []config.Skill) ([]config.Skill, error) {
	validated := append([]config.Skill(nil), skills...)
	seen := make(map[string]string, len(validated))
	for i := range validated {
		source := filepath.ToSlash(validated[i].SourceDir)
		if source == "" {
			source = validated[i].Name
		}
		info, err := skilltree.ValidateSkill(validated[i].Tree, source)
		if err != nil {
			return nil, err
		}
		key := skilltree.NormalizeName(info.Name)
		if previous, exists := seen[key]; exists {
			return nil, fmt.Errorf(messages.SyncSkillTierCollisionFmt, info.Name, previous, source)
		}
		seen[key] = source
		validated[i].Name = info.Name
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].Name < validated[j].Name })
	return validated, nil
}

func materializeSkillTree(sys System, dir string, tree skilltree.Tree) error {
	if err := sys.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, dir, err)
	}
	for _, file := range tree.Files() {
		if err := skilltree.ValidateRelativePath(file.Path); err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(file.Path))
		if err := sys.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf(messages.SyncCreateDirFailedFmt, filepath.Dir(target), err)
		}
		if err := sys.WriteFileAtomic(target, file.Data, file.FileMode()); err != nil {
			return fmt.Errorf(messages.SyncWriteFileFailedFmt, target, err)
		}
	}
	return nil
}

// cleanSharedAgentSkills removes every disposable shared-skill projection path.
func cleanSharedAgentSkills(sys System, root string) error {
	return removeSkillRoot(sys, filepath.Join(root, ".agents", "skills"))
}

// cleanClaudeSkills removes every disposable Claude skill projection path.
func cleanClaudeSkills(sys System, root string) error {
	return removeSkillRoot(sys, filepath.Join(root, ".claude", "skills"))
}

func removeSkillRoot(sys System, liveRoot string) error {
	for _, target := range []string{liveRoot, liveRoot + projectionStageSuffix, liveRoot + projectionDiscardSuffix} {
		if err := sys.RemoveAll(target); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, target, err)
		}
	}
	return nil
}

// cleanLegacySkillOutputs removes retired Agent Layer-generated skill
// projection directories. Agent Layer claims exclusive ownership of these
// paths (see docs/SKILL-CLIENT-SPEC.md "Ownership of legacy projection
// paths") and removes them unconditionally.
func cleanLegacySkillOutputs(sys System, root string) error {
	for _, projection := range config.LegacySkillProjections {
		path := filepath.Join(append([]string{root}, projection.Dir...)...)
		if err := sys.RemoveAll(path); err != nil {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
		}
	}
	return nil
}
