package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadinessMuseVSCodeSharedMCPWithoutClaude(t *testing.T) {
	for _, state := range []string{"missing", "unchanged MCP", "fresh", "Muse disabled"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}}))
			cfg := "[agents.vscode]\nenabled = true\n[agents.muse]\nenabled = true\n"
			if state == "Muse disabled" {
				cfg = "[agents.vscode]\nenabled = true\n[agents.muse]\nenabled = false\n"
			}
			configPath := filepath.Join(root, ".agent-layer", "config.toml")
			require.NoError(t, os.WriteFile(configPath, []byte(cfg), 0o600))
			base := time.Now().Add(-time.Hour)
			require.NoError(t, os.Chtimes(configPath, base, base))
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".vscode"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".vscode", "settings.json"), []byte("// >>> agent-layer\n// <<< agent-layer\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".vscode", "mcp.json"), []byte("{}"), 0o600))
			if state == "unchanged MCP" || state == "fresh" {
				path := filepath.Join(root, ".mcp.json")
				require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
				if state == "unchanged MCP" {
					require.NoError(t, os.Chtimes(path, base.Add(-time.Hour), base.Add(-time.Hour)))
				}
			}
			checks, err := buildUpgradeReadinessChecks(&installer{root: root, sys: RealSystem{}})
			require.NoError(t, err)
			check := findReadinessCheckByID(checks, readinessCheckVSCodeNoSyncStaleOutput)
			if state == "missing" {
				require.NotNil(t, check)
				require.Contains(t, strings.Join(check.Details, "\n"), ".mcp.json")
				require.NotContains(t, strings.Join(check.Details, "\n"), ".claude/settings.json")
			} else {
				require.Nil(t, check)
			}
		})
	}
}
