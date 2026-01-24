package sync

import (
	"errors"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildMCPConfig_PromptCommandError(t *testing.T) {
	originalLookPath := lookPath
	t.Cleanup(func() { lookPath = originalLookPath })
	lookPath = func(string) (string, error) {
		return "", errors.New("missing")
	}

	// Create a root without al binary or source, so resolvePromptServerCommand fails
	root := t.TempDir()
	project := &config.ProjectConfig{Root: root}

	_, err := buildMCPConfig(project)
	if err == nil {
		t.Fatalf("expected error from resolvePromptServerCommand")
	}
}
