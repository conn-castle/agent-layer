package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerScriptHandlesDotSlashChecksums(t *testing.T) {
	path := filepath.Join("..", "..", "al-install.sh")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if strings.Contains(script, `SHA256SUMS`) {
		t.Fatalf("installer script should use checksums.txt, not SHA256SUMS")
	}
	if !strings.Contains(script, `sub(/^\.\//, "", path)`) {
		t.Fatalf("installer script should strip ./ prefixes when parsing checksums.txt entries")
	}
}

func TestInstallerScriptProvidesErrorOutput(t *testing.T) {
	path := filepath.Join("..", "..", "al-install.sh")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if !strings.Contains(script, "Error: installer failed") {
		t.Fatalf("installer script should emit a generic error message when a command fails")
	}
	if !strings.Contains(script, "Checksum verification failed") {
		t.Fatalf("installer script should emit a clear checksum failure message")
	}
}

func TestInstallerScriptSupportsCompletionFlags(t *testing.T) {
	path := filepath.Join("..", "..", "al-install.sh")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if !strings.Contains(script, "--no-completions") {
		t.Fatalf("installer script should accept --no-completions")
	}
	if !strings.Contains(script, "--shell") {
		t.Fatalf("installer script should accept --shell")
	}
}

func TestInstallerScriptSupportsPrefixFlag(t *testing.T) {
	path := filepath.Join("..", "..", "al-install.sh")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read installer script: %v", err)
	}

	script := string(data)
	if !strings.Contains(script, "--prefix") {
		t.Fatalf("installer script should accept --prefix")
	}
}
