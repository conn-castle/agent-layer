package sync

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const instructionHeader = "<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n  Regenerate: al sync\n-->\n\n"

// ClaudeInstructionsPath is the generated Claude instruction path relative to the project root.
const ClaudeInstructionsPath = claudeDirectory + "/CLAUDE.md"

// ClaudeInstructionsTarget is the relative link to the canonical project instructions.
const ClaudeInstructionsTarget = "../AGENTS.md"

// writeInstructionShims generates instruction shims for supported clients.
// agy (Antigravity), Codex, Copilot, Grok, and other shared-tier clients
// read AGENTS.md (per the agentskills.io standard) or their client-specific
// shim. Claude Code reads `.claude/CLAUDE.md` (an equivalent project location
// to root CLAUDE.md). GEMINI.md is intentionally NOT written: the Gemini CLI
// was retired in 0.10.2 and agy reads AGENTS.md. The v0.10.2 migration's
// `f-delete-orphan-gemini-md` op removes any leftover GEMINI.md from
// pre-0.10.2 repos.
func writeInstructionShims(sys System, root string, instructions []config.InstructionFile) error {
	if _, err := inspectClaudeInstructionDestination(sys, root); err != nil {
		return err
	}
	if err := writeInstructionFile(sys, filepath.Join(root, "AGENTS.md"), instructions); err != nil {
		return err
	}
	claudeDir := filepath.Join(root, claudeDirectory)
	if err := sys.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, claudeDir, err)
	}
	if err := writeClaudeInstructions(sys, root, instructions); err != nil {
		return err
	}
	// The withdrawn Muse integration put this same content in Claude's rules
	// directory, which Grok also loads. Remove only that generated copy.
	if err := cleanClaudeRulesInstructions(sys, root); err != nil {
		return err
	}

	githubDir := filepath.Join(root, ".github")
	if err := sys.MkdirAll(githubDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, githubDir, err)
	}
	if err := writeInstructionFile(sys, filepath.Join(githubDir, "copilot-instructions.md"), instructions); err != nil {
		return err
	}

	return nil
}

// writeClaudeInstructions shares the canonical file with Claude. Muse warns
// about distinct AGENTS.md and CLAUDE.md files even when their contents match.
func writeClaudeInstructions(sys System, root string, instructions []config.InstructionFile) error {
	path := filepath.Join(root, ClaudeInstructionsPath)
	dir := filepath.Dir(path)
	existingLink, err := inspectClaudeInstructionDestination(sys, root)
	if err != nil {
		return err
	}
	info, err := sys.Lstat(dir)
	if err != nil {
		return fmt.Errorf("inspect Claude instruction directory %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Preserve the existing copy behavior for user-relocated Claude trees:
		// ../AGENTS.md would resolve relative to their external directory.
		return writeInstructionFile(sys, path, instructions)
	}
	if existingLink {
		return nil
	}
	// Stage beside the destination so the relative target has the same meaning
	// before and after an atomic rename. Never write through a previous link.
	tmp := path + ".tmp-" + rand.Text()
	if err := sys.Symlink(ClaudeInstructionsTarget, tmp); err != nil {
		return fmt.Errorf("create Claude instruction link %s: %w", path, err)
	}
	defer func() { _ = sys.Remove(tmp) }()
	canonical, err := sys.Stat(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return fmt.Errorf("inspect canonical instructions: %w", err)
	}
	linked, err := sys.Stat(tmp)
	if err != nil {
		return fmt.Errorf("resolve Claude instruction link %s: %w", path, err)
	}
	if !os.SameFile(canonical, linked) {
		return fmt.Errorf("claude instruction link %s does not resolve to project AGENTS.md", path)
	}
	if err := sys.Rename(tmp, path); err != nil {
		return fmt.Errorf("publish Claude instruction link %s: %w", path, err)
	}
	return nil
}

// inspectClaudeInstructionDestination reports whether .claude/CLAUDE.md is
// already the canonical relative link. It does not create, replace, or remove
// the path. Missing destinations are allowed so callers can abort before
// mutating AGENTS.md when an existing unmanaged file would block the later
// write.
func inspectClaudeInstructionDestination(sys System, root string) (existingLink bool, err error) {
	path := filepath.Join(root, ClaudeInstructionsPath)
	info, err := sys.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect Claude instructions %s: %w", path, err)
	}
	owned := false
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := sys.Readlink(path)
		if err != nil {
			return false, fmt.Errorf("read Claude instruction link %s: %w", path, err)
		}
		existingLink = target == ClaudeInstructionsTarget
		owned = existingLink
	} else if info.Mode().IsRegular() {
		data, err := sys.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("read Claude instructions %s: %w", path, err)
		}
		// Older generated copies are empty when there are no instructions.
		owned = len(data) == 0 || strings.HasPrefix(string(data), instructionHeader)
	}
	if !owned {
		return false, fmt.Errorf("refusing to overwrite unmanaged Claude instructions at %s: move your guidance into .agent-layer/instructions/ and relocate the existing path before syncing", path)
	}
	return existingLink, nil
}

// Legacy cleanup owns only ordinary repository paths, never user symlinks.
func cleanClaudeRulesInstructions(sys System, root string) error {
	path := root
	for _, part := range []string{claudeDirectory, "rules", "agent-layer.md"} {
		path = filepath.Join(path, part)
		info, err := sys.Lstat(path)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect obsolete instruction path %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
	}
	return removeGeneratedInstructionShim(sys, path)
}

func writeInstructionFile(sys System, path string, instructions []config.InstructionFile) error {
	content := buildInstructionShim(instructions)
	if err := sys.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, path, err)
	}
	return nil
}

func buildInstructionShim(instructions []config.InstructionFile) string {
	if len(instructions) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(instructionHeader)
	for _, instruction := range instructions {
		builder.WriteString("<!-- BEGIN: ")
		builder.WriteString(instruction.Name)
		builder.WriteString(" -->\n")
		content := instruction.Content
		builder.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("<!-- END: ")
		builder.WriteString(instruction.Name)
		builder.WriteString(" -->\n\n")
	}
	return strings.TrimRight(builder.String(), "\n") + "\n"
}

// cleanCodexInstructions removes the retired Codex-specific instruction shim.
// Codex reads root AGENTS.md as project instructions. When CODEX_HOME points at
// repo-local .codex, .codex/AGENTS.md is loaded as home-level instructions and
// duplicates the project document.
func cleanCodexInstructions(sys System, root string) error {
	return removeGeneratedInstructionShim(sys, filepath.Join(root, ".codex", "AGENTS.md"))
}

// cleanClaudeRootInstructions removes a generated root CLAUDE.md. Claude Code
// reads `.claude/CLAUDE.md`; a leftover root copy is still loaded by Grok.
func cleanClaudeRootInstructions(sys System, root string) error {
	return removeGeneratedInstructionShim(sys, filepath.Join(root, "CLAUDE.md"))
}

func removeGeneratedInstructionShim(sys System, path string) error {
	isGenerated, err := hasGeneratedMarker(sys, path)
	if err != nil {
		return err
	}
	if !isGenerated {
		return nil
	}
	if err := sys.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf(messages.SyncRemoveFailedFmt, path, err)
	}
	return nil
}

// hasGeneratedMarker reports whether path is a regular generated instruction
// shim. Symlinks and other non-regular paths are never classified as generated.
func hasGeneratedMarker(sys System, path string) (bool, error) {
	info, err := sys.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("inspect instruction shim %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := sys.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf(messages.SyncReadFailedFmt, path, err)
	}
	return strings.HasPrefix(string(data), instructionHeader), nil
}
