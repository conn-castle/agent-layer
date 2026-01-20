package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerScriptHandlesDotSlashChecksums(t *testing.T) {
	path := filepath.Join("..", "..", "agent-layer-install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if strings.Contains(script, `grep " ${ASSET}$"`) {
		t.Fatalf("installer script should not rely on a grep pattern that misses ./ prefixes in SHA256SUMS")
	}
	if !strings.Contains(script, `sub(/^\.\//, "", path)`) {
		t.Fatalf("installer script should strip ./ prefixes when parsing SHA256SUMS entries")
	}
}

func TestInstallerScriptProvidesErrorOutput(t *testing.T) {
	path := filepath.Join("..", "..", "agent-layer-install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if !strings.Contains(script, "Error: installer failed") {
		t.Fatalf("installer script should emit a generic error message when a command fails")
	}
	if !strings.Contains(script, "Agent Layer install failed") {
		t.Fatalf("installer script should emit a clear install failure message")
	}
}

func TestInstallerScriptSupportsNoWizardFlag(t *testing.T) {
	path := filepath.Join("..", "..", "agent-layer-install.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if !strings.Contains(script, "--no-wizard") {
		t.Fatalf("installer script should accept --no-wizard")
	}
	if !strings.Contains(script, "install_args") {
		t.Fatalf("installer script should pass install args through")
	}
}
