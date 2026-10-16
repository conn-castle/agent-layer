package versiondispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitDevelopmentEnvironmentPreservesProvenance(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv(EnvDevelopmentSourceRoot, root)
	t.Setenv(EnvDevelopmentBypassVersionDispatch, "1")
	t.Setenv(EnvDevelopmentExecutable, "")
	original := os.Getenv("PATH")
	t.Setenv("PATH", original)
	if err := ConfigureDevelopmentEnvironment(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(EnvDevelopmentExecutable) != "" || os.Getenv("PATH") != original {
		t.Fatal("bypass alone invented executable provenance")
	}
	source := filepath.Join(t.TempDir(), "source with spaces", "al")
	t.Setenv(EnvDevelopmentExecutable, source)
	if err := ConfigureDevelopmentEnvironment(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(EnvDevelopmentExecutable) != source || !strings.HasPrefix(os.Getenv("PATH"), filepath.Dir(source)+string(os.PathListSeparator)) {
		t.Fatal("explicit selection did not reach descendants")
	}
	once := os.Getenv("PATH")
	if err := ConfigureDevelopmentEnvironment(); err != nil || os.Getenv("PATH") != once {
		t.Fatal("development PATH prepending is not idempotent")
	}
	t.Setenv(EnvDevelopmentExecutable, "relative/al")
	if err := ConfigureDevelopmentEnvironment(); err == nil {
		t.Fatal("relative executable must not be projected")
	}
}

func TestDevelopmentSelectionIsScopedToExplicitRoot(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	source := filepath.Join(a, ".agent-layer", "tmp", "dev-bin", "al")
	t.Setenv(EnvDevelopmentExecutable, source)
	t.Setenv(EnvDevelopmentBypassVersionDispatch, "1")
	t.Setenv(EnvDevelopmentSourceRoot, a)
	t.Setenv(EnvVersionOverride, "")
	if got := DevelopmentExecutable(b); got != "" {
		t.Fatalf("inherited source leaked into B: %s", got)
	}
	if DevelopmentExecutable(a) != source {
		t.Fatal("matching root lost source selection")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(a, alias); err != nil {
		t.Fatal(err)
	}
	if DevelopmentExecutable(alias) != source {
		t.Fatal("same-root symlink lost provenance")
	}
	t.Setenv(EnvDevelopmentSourceRoot, "")
	if DevelopmentExecutable(a) != "" {
		t.Fatal("unscoped environment inferred a project")
	}
	// Deliberate scratch-project testing requires an explicit matching opt-in.
	t.Setenv(EnvDevelopmentSourceRoot, b)
	if DevelopmentExecutable(b) != source || DevelopmentExecutable(a) != "" {
		t.Fatal("explicit scratch root did not scope selection")
	}
}

func TestForeignDevelopmentEnvironmentDoesNotChangePath(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv(EnvDevelopmentBypassVersionDispatch, "1")
	t.Setenv(EnvDevelopmentSourceRoot, t.TempDir())
	t.Setenv(EnvDevelopmentExecutable, filepath.Join(t.TempDir(), "al"))
	before := os.Getenv("PATH")
	if err := ConfigureDevelopmentEnvironment(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PATH") != before {
		t.Fatal("foreign source changed PATH")
	}
}
