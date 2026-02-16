package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

func TestRunProfile_PreviewOnly(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	profile := strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)
	require.NoError(t, os.WriteFile(profilePath, []byte(profile), 0o644))

	syncCalled := false
	var out bytes.Buffer
	err := RunProfile(root, func(string) ([]warnings.Warning, error) {
		syncCalled = true
		return nil, nil
	}, "", profilePath, false, &out)
	require.NoError(t, err)
	require.False(t, syncCalled)

	updated, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), `mode = "none"`)
	require.Contains(t, out.String(), messages.WizardProfilePreviewOnly)
}

func TestRunProfile_Apply(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	profile := strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)
	require.NoError(t, os.WriteFile(profilePath, []byte(profile), 0o644))

	syncCalled := false
	var out bytes.Buffer
	err := RunProfile(root, func(string) ([]warnings.Warning, error) {
		syncCalled = true
		return nil, nil
	}, "", profilePath, true, &out)
	require.NoError(t, err)
	require.True(t, syncCalled)

	updated, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), `mode = "all"`)
	if _, err := os.Stat(configPath + ".bak"); err != nil {
		t.Fatalf("expected profile apply backup: %v", err)
	}
}

func TestCleanupBackups(t *testing.T) {
	root := t.TempDir()
	configBackup := filepath.Join(root, ".agent-layer", "config.toml.bak")
	envBackup := filepath.Join(root, ".agent-layer", ".env.bak")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755))
	require.NoError(t, os.WriteFile(configBackup, []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(envBackup, []byte("y"), 0o600))

	removed, err := CleanupBackups(root)
	require.NoError(t, err)
	require.Equal(t, []string{".agent-layer/.env.bak", ".agent-layer/config.toml.bak"}, removed)

	if _, err := os.Stat(configBackup); !os.IsNotExist(err) {
		t.Fatalf("expected config backup removed, err=%v", err)
	}
	if _, err := os.Stat(envBackup); !os.IsNotExist(err) {
		t.Fatalf("expected env backup removed, err=%v", err)
	}
}
