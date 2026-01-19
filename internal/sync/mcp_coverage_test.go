package sync

import (
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

func TestBuildMCPConfig_PromptCommandError(t *testing.T) {
	// Create a root without al binary or source, so resolvePromptServerCommand fails
	root := t.TempDir()
	project := &config.ProjectConfig{Root: root}

	_, err := buildMCPConfig(project)
	if err == nil {
		t.Fatalf("expected error from resolvePromptServerCommand")
	}
}
