package wizard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
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
	err := RunProfile(root, func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
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
	err := RunProfile(root, func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
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

func TestRunProfile_ApplyWarnsWhenExistingConfigIsInvalidTOML(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte("[approvals"), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	profile := strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)
	require.NoError(t, os.WriteFile(profilePath, []byte(profile), 0o644))

	syncCalled := false
	var out bytes.Buffer
	err := RunProfile(root, func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}, "", profilePath, true, &out)
	require.NoError(t, err)
	require.True(t, syncCalled)
	require.Contains(t, out.String(), "existing .agent-layer/config.toml is invalid TOML")

	updated, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(updated), `mode = "all"`)
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

func TestRunProfile_PathRequired(t *testing.T) {
	err := RunProfile(t.TempDir(), nil, "", "   ", false, nil)
	require.ErrorContains(t, err, messages.WizardProfilePathRequired)
}

func TestRunProfile_InstallFailureWhenConfigMissing(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o644))

	err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "not-a-version", profilePath, false, nil)
	require.ErrorContains(t, err, "install failed")
}

func TestRunProfile_ConfigStatError(t *testing.T) {
	root := t.TempDir()
	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("not-a-dir"), 0o644))

	err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", profilePath, false, nil)
	require.Error(t, err)
}

func TestRunProfile_ProfileReadError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", filepath.Join(root, "missing.toml"), false, nil)
	require.ErrorContains(t, err, "failed to read profile")
}

func TestRunProfile_ProfileInvalid(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte("[approvals"), 0o644))

	err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", profilePath, false, nil)
	require.ErrorContains(t, err, "invalid profile")
}

func TestRunProfile_CurrentConfigReadError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))
	require.NoError(t, os.Remove(configPath))
	require.NoError(t, os.Mkdir(configPath, 0o755))

	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o644))

	err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", profilePath, false, nil)
	require.Error(t, err)
}

func TestRunProfile_NoConfigChangesPreviewOnly(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o644))

	syncCalled := false
	var out bytes.Buffer
	err := RunProfile(root, func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}, "", profilePath, false, &out)
	require.NoError(t, err)
	require.False(t, syncCalled)
	require.Contains(t, out.String(), messages.WizardProfileNoConfigChanges)
	require.NotContains(t, out.String(), messages.WizardProfilePreviewOnly)
}

func TestRunProfile_NoConfigChangesApplyWithWarningAndSyncError(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o644))

	t.Run("warning output", func(t *testing.T) {
		var out bytes.Buffer
		err := RunProfile(root, func(string) (*alsync.Result, error) {
			return &alsync.Result{Warnings: []warnings.Warning{{Message: "be careful"}}}, nil
		}, "", profilePath, true, &out)
		require.NoError(t, err)
		require.Contains(t, out.String(), messages.WizardProfileNoConfigChanges)
		require.Contains(t, out.String(), messages.WizardRunningSync)
		require.Contains(t, out.String(), "be careful")
		require.Contains(t, out.String(), messages.WizardCompleted)
	})

	t.Run("sync error", func(t *testing.T) {
		err := RunProfile(root, func(string) (*alsync.Result, error) {
			return nil, errors.New("sync failed")
		}, "", profilePath, true, nil)
		require.ErrorContains(t, err, "sync failed")
	})
}

func TestRunProfile_BackupAndWriteErrors(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644))

	profilePath := filepath.Join(root, "profile.toml")
	profile := strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)
	require.NoError(t, os.WriteFile(profilePath, []byte(profile), 0o644))

	t.Run("backup write failure", func(t *testing.T) {
		require.NoError(t, os.Mkdir(configPath+".bak", 0o755))
		err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", profilePath, true, nil)
		require.ErrorContains(t, err, "failed to backup config")
	})

	t.Run("write config failure", func(t *testing.T) {
		require.NoError(t, os.RemoveAll(configPath+".bak"))
		origWrite := writeFileAtomic
		t.Cleanup(func() { writeFileAtomic = origWrite })
		writeFileAtomic = func(path string, data []byte, perm os.FileMode) error {
			if path == configPath {
				return errors.New("boom")
			}
			return origWrite(path, data, perm)
		}

		err := RunProfile(root, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "", profilePath, true, nil)
		require.ErrorContains(t, err, "failed to write config")
	})
}

func TestCleanupBackups_MissingAndRemoveError(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755))

	removed, err := CleanupBackups(root)
	require.NoError(t, err)
	require.Empty(t, removed)

	badPath := filepath.Join(root, ".agent-layer", "config.toml.bak")
	require.NoError(t, os.Mkdir(badPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badPath, "child"), []byte("x"), 0o644))

	_, err = CleanupBackups(root)
	require.Error(t, err)
}
