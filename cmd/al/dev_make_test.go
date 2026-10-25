package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// devMakeCommand runs the real Makefile without this agent's dev-launch
// environment, publishing the development executable under devDir.
func devMakeCommand(devDir string, args ...string) *exec.Cmd {
	cmd := exec.Command("make", append([]string{"--no-print-directory", "-s", "-f", "Makefile"}, args...)...)
	cmd.Args = append(cmd.Args, "AL_DEV_BIN_DIR="+strings.ReplaceAll(devDir, "$", "$$"))
	cmd.Dir = "../.."
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "AL_DEV_BYPASS_VERSION_DISPATCH", "AL_VERSION", "AL_SHIM_ACTIVE", "AL_NO_NETWORK", "MAKEFLAGS", "MFLAGS", "MAKEOVERRIDES":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return cmd
}

func TestMakeSourceCommandBypassesInheritedVersionSelection(t *testing.T) {
	// A conflicting override must not redirect a source command.
	devDir := filepath.Join(t.TempDir(), "source with spaces $() `false` ' quote")
	cmd := devMakeCommand(devDir, "-f", "-", "test-source-version")
	cmd.Stdin = strings.NewReader("test-source-version: al-dev-build\n\t@$(AL_RUN) --version\n")
	cmd.Env = append(cmd.Env, "AL_VERSION=invalid-source-command-override", "AL_NO_NETWORK=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("source Make command must bypass release selection: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(devDir, "al")); err != nil {
		t.Fatalf("atomic build did not publish literal executable path: %v", err)
	}
	if !strings.Contains(string(output), "dev") {
		t.Fatalf("expected local development binary, got %s", output)
	}
}

func TestMakeDevBuildPreservesUnchangedExecutable(t *testing.T) {
	// A stub go writes chosen bytes so the test controls whether output changes.
	stubDir := t.TempDir()
	stub := "#!/bin/sh\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = -o ]; then out=$2; fi\n  shift\ndone\n" +
		"printf %s \"$STUB_GO_OUTPUT\" > \"$out\" && chmod 755 \"$out\"\n[ -z \"$STUB_GO_FAIL\" ]\n"
	if err := os.WriteFile(filepath.Join(stubDir, "go"), []byte(stub), 0o755); err != nil { //nolint:gosec // The stub go must be executable.
		t.Fatal(err)
	}
	devDir := filepath.Join(t.TempDir(), "dev bin $() ' quote")
	devBin := filepath.Join(devDir, "al")
	cache := t.TempDir()
	build := func(output string, fail bool) (string, error) {
		t.Helper()
		cmd := devMakeCommand(devDir, "al-dev-build", "GO_CACHE="+cache, "GO_MOD_CACHE="+cache)
		cmd.Env = append(cmd.Env, "PATH="+stubDir+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_GO_OUTPUT="+output)
		if fail {
			cmd.Env = append(cmd.Env, "STUB_GO_FAIL=1")
		}
		out, err := cmd.CombinedOutput()
		if staged, _ := filepath.Glob(filepath.Join(devDir, "al.build-*")); len(staged) != 0 {
			t.Fatalf("staging files left behind: %v", staged)
		}
		return string(out), err
	}
	expect := func(content string) os.FileInfo {
		t.Helper()
		data, err := os.ReadFile(devBin) //nolint:gosec // Test-owned temporary path.
		if err != nil || string(data) != content {
			t.Fatalf("executable = %q, %v; want %q", data, err, content)
		}
		info, err := os.Stat(devBin)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}

	if out, err := build("first", false); err != nil || !strings.Contains(out, "replaced") {
		t.Fatalf("initial build: %v\n%s", err, out)
	}
	expect("first")
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(devBin, old, old); err != nil {
		t.Fatal(err)
	}
	before := expect("first")

	if out, err := build("first", false); err != nil || !strings.Contains(out, "unchanged") {
		t.Fatalf("unchanged build: %v\n%s", err, out)
	}
	unchanged := expect("first")
	if !os.SameFile(before, unchanged) || !unchanged.ModTime().Equal(old) || unchanged.Mode() != before.Mode() {
		t.Fatalf("unchanged build replaced executable: before %v %v, after %v %v", before.ModTime(), before.Mode(), unchanged.ModTime(), unchanged.Mode())
	}

	if out, err := build("second", false); err != nil || !strings.Contains(out, "replaced") {
		t.Fatalf("changed build: %v\n%s", err, out)
	}
	changed := expect("second")
	if os.SameFile(before, changed) {
		t.Fatal("changed build kept the old executable file")
	}

	if out, err := build("partial", true); err == nil {
		t.Fatalf("failed build succeeded:\n%s", out)
	}
	if failed := expect("second"); !os.SameFile(changed, failed) {
		t.Fatal("failed build replaced the executable")
	}
}
