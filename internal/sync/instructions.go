package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const instructionHeader = "<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n  Regenerate: al sync\n-->\n\n"

// writeInstructionShims generates instruction shims for supported clients.
// agy (Antigravity), Claude, Codex, Copilot, and other shared-tier clients
// all read AGENTS.md (per the agentskills.io standard) or their client-
// specific shim. GEMINI.md is intentionally NOT written: the Gemini CLI was
// retired in 0.10.2 and agy reads AGENTS.md. The v0.10.2 migration's
// `f-delete-orphan-gemini-md` op removes any leftover GEMINI.md from
// pre-0.10.2 repos.
func writeInstructionShims(sys System, root string, instructions []config.InstructionFile) error {
	if err := writeInstructionFile(sys, filepath.Join(root, "AGENTS.md"), instructions); err != nil {
		return err
	}
	if err := writeInstructionFile(sys, filepath.Join(root, "CLAUDE.md"), instructions); err != nil {
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
	path := filepath.Join(root, ".codex", "AGENTS.md")
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
