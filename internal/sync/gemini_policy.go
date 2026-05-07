package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
)

const geminiPolicyDir = "policies"

const geminiPolicyHeader = `# GENERATED FILE
# Source: .agent-layer/commands.allow and .agent-layer/config.toml
# Regenerate: al sync
#
# Replaces the deprecated tools.allowed setting (removed in Gemini 1.0).
# See https://geminicli.com/docs/reference/policy-engine/.

`

// WriteGeminiPolicies generates .gemini/policies/agent-layer.toml, or removes a
// stale copy when the project no longer auto-allows shell commands.
func WriteGeminiPolicies(sys System, root string, project *config.ProjectConfig) error {
	policyPath := filepath.Join(root, geminiDir, geminiPolicyDir, "agent-layer.toml")

	approvals := projection.BuildApprovals(project.Config, project.CommandsAllow)
	if !approvals.AllowCommands || len(approvals.Commands) == 0 {
		if err := sys.Remove(policyPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(messages.SyncRemoveFailedFmt, policyPath, err)
		}
		return nil
	}

	policyDir := filepath.Dir(policyPath)
	if err := sys.MkdirAll(policyDir, 0o755); err != nil {
		return fmt.Errorf(messages.SyncCreateDirFailedFmt, policyDir, err)
	}

	if err := sys.WriteFileAtomic(policyPath, []byte(buildGeminiPolicies(approvals.Commands)), 0o644); err != nil {
		return fmt.Errorf(messages.SyncWriteFileFailedFmt, policyPath, err)
	}

	return nil
}

func buildGeminiPolicies(commands []string) string {
	var b strings.Builder
	b.WriteString(geminiPolicyHeader)
	for i, cmd := range commands {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[[rule]]\n")
		b.WriteString("toolName = \"run_shell_command\"\n")
		fmt.Fprintf(&b, "commandPrefix = %s\n", tomlBasicString(cmd))
		b.WriteString("decision = \"allow\"\n")
		b.WriteString("priority = 100\n")
		// Without this, the policy engine asks for confirmation when a
		// matched command includes redirection (>, >>, <, ...), regressing
		// the previous tools.allowed behavior for headless workflows.
		b.WriteString("allowRedirection = true\n")
	}
	return b.String()
}

// tomlBasicString quotes s per TOML 1.0 basic-string rules. Go's %q uses
// strconv.Quote, which emits escapes (\a, \v, \xNN) that TOML rejects.
func tomlBasicString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
