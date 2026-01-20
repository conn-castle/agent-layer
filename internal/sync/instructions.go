package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/fsutil"
)

const instructionHeader = "<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n  Regenerate: ./al sync\n-->\n\n"

// WriteInstructionShims generates instruction shims for supported clients.
func WriteInstructionShims(root string, instructions []config.InstructionFile) error {
	if err := writeInstructionFile(filepath.Join(root, "AGENTS.md"), instructions); err != nil {
		return err
	}
	if err := writeInstructionFile(filepath.Join(root, "CLAUDE.md"), instructions); err != nil {
		return err
	}
	if err := writeInstructionFile(filepath.Join(root, "GEMINI.md"), instructions); err != nil {
		return err
	}

	githubDir := filepath.Join(root, ".github")
	if err := os.MkdirAll(githubDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", githubDir, err)
	}
	if err := writeInstructionFile(filepath.Join(githubDir, "copilot-instructions.md"), instructions); err != nil {
		return err
	}

	return nil
}

// WriteCodexInstructions generates the Codex-specific instruction shim.
func WriteCodexInstructions(root string, instructions []config.InstructionFile) error {
	codexDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", codexDir, err)
	}
	return writeInstructionFile(filepath.Join(codexDir, "AGENTS.md"), instructions)
}

func writeInstructionFile(path string, instructions []config.InstructionFile) error {
	content := buildInstructionShim(instructions)
	if err := fsutil.WriteFileAtomic(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func buildInstructionShim(instructions []config.InstructionFile) string {
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
