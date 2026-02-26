package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupRepo(t *testing.T, root string) {
	configDir := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.Mkdir(filepath.Join(configDir, "instructions"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "instructions", "00_base.md"), []byte(""), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(configDir, "slash-commands"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0644))
}

// basicAgentConfig returns a minimal valid config for tests.
func basicAgentConfig() string {
	return `[approvals]
mode = "none"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
`
}
