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
		fmt.Fprintf(&b, "commandPrefix = %q\n", cmd)
		b.WriteString("decision = \"allow\"\n")
		b.WriteString("priority = 100\n")
	}
	return b.String()
}
