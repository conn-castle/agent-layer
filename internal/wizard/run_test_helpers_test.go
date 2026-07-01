package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func setupRepo(t *testing.T, root string) {
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.MkdirAll(configDir, 0700))
	require.NoError(t, os.Mkdir(filepath.Join(configDir, "instructions"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "instructions", "00_rules.md"), []byte(""), 0600))
	require.NoError(t, os.Mkdir(filepath.Join(configDir, "skills"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0600))
	gitignoreBlock, err := templates.Read("gitignore.block")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "gitignore.block"), gitignoreBlock, 0600))
}

// basicAgentConfig returns a minimal valid config for tests.
func basicAgentConfig() string {
	return `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
`
}
