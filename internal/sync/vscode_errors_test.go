package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestWriteVSCodeSettingsInvalidJSONCExtraTokensAfterRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  \"editor.tabSize\": 2\n}\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}
	if err := WriteVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsBuildError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{}
	err := writeVSCodeSettings(sys, t.TempDir(), &config.ProjectConfig{}, func(*config.ProjectConfig) (*vscodeSettings, error) {
		return nil, errors.New("build fail")
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteVSCodeSettingsMarshalError(t *testing.T) {
	t.Parallel()
	sys := &MockSystem{
		MkdirAllFunc: func(path string, perm os.FileMode) error { return nil },
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal fail")
		},
	}
	if err := WriteVSCodeSettings(sys, t.TempDir(), &config.ProjectConfig{}); err == nil {
		t.Fatal("expected error")
	}
}
