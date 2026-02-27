package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultPaths(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if paths.Root != root {
		t.Fatalf("expected root %s, got %s", root, paths.Root)
	}
	if paths.ConfigPath != filepath.Join(root, ".agent-layer", "config.toml") {
		t.Fatalf("unexpected config path: %s", paths.ConfigPath)
	}
	if paths.EnvPath != filepath.Join(root, ".agent-layer", ".env") {
		t.Fatalf("unexpected env path: %s", paths.EnvPath)
	}
	if paths.InstructionsDir != filepath.Join(root, ".agent-layer", "instructions") {
		t.Fatalf("unexpected instructions dir: %s", paths.InstructionsDir)
	}
	if paths.SkillsDir != filepath.Join(root, ".agent-layer", "skills") {
		t.Fatalf("unexpected skills dir: %s", paths.SkillsDir)
	}
	if paths.CommandsAllow != filepath.Join(root, ".agent-layer", "commands.allow") {
		t.Fatalf("unexpected commands allow path: %s", paths.CommandsAllow)
	}
}
