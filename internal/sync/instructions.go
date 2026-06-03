package sync

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const instructionHeader = "<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n  Regenerate: al sync\n-->\n\n"

// WriteInstructionShims generates instruction shims for supported clients.
// agy (Antigravity), Claude, Codex, Copilot, and other shared-tier clients
// all read AGENTS.md (per the agentskills.io standard) or their client-
// specific shim. GEMINI.md is intentionally NOT written: the Gemini CLI was
// retired in 0.10.2 and agy reads AGENTS.md. The v0.10.2 migration's
// `f-delete-orphan-gemini-md` op removes any leftover GEMINI.md from
// pre-0.10.2 repos.
func WriteInstructionShims(sys System, root string, instructions []config.InstructionFile) error {
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

// WriteCodexInstructions generates the Codex-specific instruction shim.
func WriteCodexInstructions(sys System, root string, instructions []config.InstructionFile) error {
	codexDir := filepath.Join(root, ".codex")
	if err := sys.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, codexDir, err)
	}
	return writeInstructionFile(sys, filepath.Join(codexDir, "AGENTS.md"), instructions)
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
