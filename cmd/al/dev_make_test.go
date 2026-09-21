package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMakeSourceCommandBypassesInheritedVersionSelection(t *testing.T) {
	// Exercise the real Makefile entry point without this agent's dev-launch
	// environment. A conflicting override must not redirect a source command.
	cmd := exec.Command("make", "--no-print-directory", "-s", "-f", "Makefile", "-f", "-", "test-source-version")
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader("test-source-version:\n\t@$(AL_RUN) --version\n")
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "AL_DEV_BYPASS_VERSION_DISPATCH", "AL_VERSION", "AL_SHIM_ACTIVE", "AL_NO_NETWORK", "MAKEFLAGS", "MFLAGS", "MAKEOVERRIDES":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "AL_VERSION=invalid-source-command-override", "AL_NO_NETWORK=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source Make command must bypass release selection: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "dev") {
		t.Fatalf("expected local development binary, got %s", output)
	}
}
