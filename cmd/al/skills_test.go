package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestSkillsCommandTreeIsRegistered(t *testing.T) {
	root := newRootCmd()
	command, _, err := root.Find([]string{skillsCommandName})
	if err != nil {
		t.Fatalf("find %s: %v", skillsCommandName, err)
	}
	if command.Hidden {
		t.Fatalf("%s is a documented workflow command and must not be hidden", skillsCommandName)
	}

	want := map[string]bool{"add": false, "remove": false, "status": false, "pull": false, "push": false}
	for _, sub := range command.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("al skills %s is not registered", name)
		}
	}

	// The spec deliberately excludes a sync alias and any search or preview
	// surface; registering one would create a second projection entry point.
	for _, sub := range command.Commands() {
		switch sub.Name() {
		case "sync", "search", "preview", "list", "recommend":
			t.Fatalf("al skills %s must not exist", sub.Name())
		}
	}
}

func TestSkillsAddFlagsCoverEveryConfigurableField(t *testing.T) {
	command := newSkillsAddCmd()
	for _, name := range []string{"ref", "tracking", "write", "push-repository", "push-branch"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("al skills add is missing --%s", name)
		}
	}
	// Every flag must default to empty so an omitted flag resolves to the
	// documented constant instead of freezing a value into configuration.
	for _, name := range []string{"ref", "tracking", "write", "push-repository", "push-branch"} {
		if got := command.Flags().Lookup(name).DefValue; got != "" {
			t.Fatalf("--%s default = %q, want an empty default", name, got)
		}
	}
}

func TestSkillsAddRequiresARepositoryAndSelector(t *testing.T) {
	command := newSkillsAddCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SilenceUsage = true
	command.SetArgs([]string{"https://example.invalid/skills.git"})
	if err := command.Execute(); err == nil {
		t.Fatal("add without a selector must fail rather than import everything")
	}
}

func TestSkillsRemoveTakesExactlyRepositoryAndSelector(t *testing.T) {
	command := newSkillsRemoveCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SilenceUsage = true
	command.SetArgs([]string{"https://example.invalid/skills.git", "skills/a", "skills/b"})
	if err := command.Execute(); err == nil {
		t.Fatal("remove must name exactly one selector so it is never ambiguous")
	}
}

func TestSkillsHelpQuotesExclusionSelectors(t *testing.T) {
	// An unquoted '!' is history expansion in an interactive shell, so copying an
	// example straight from help must not break the user's session.
	addHelp := newSkillsAddCmd().Long
	if !strings.Contains(addHelp, "'!skills/internal'") {
		t.Fatalf("add help must show a quoted exclusion example:\n%s", addHelp)
	}
	removeHelp := newSkillsRemoveCmd().Long
	if !strings.Contains(removeHelp, "'!skills/internal'") {
		t.Fatalf("remove help must show a quoted exclusion example:\n%s", removeHelp)
	}
}

func TestSkillsStatusExitsNonzeroForAnUnmanagedDirectory(t *testing.T) {
	root := newSkillsStatusFixture(t)
	stray := filepath.Join(root, ".agent-layer", config.ImportedSkillsDirName, "stray")
	if err := os.MkdirAll(stray, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	out, err := runSkillsStatus(t, root, false)
	if err == nil {
		t.Fatalf("status must exit nonzero for an unmanaged directory:\n%s", out)
	}
	if !strings.Contains(out, "orphan") {
		t.Fatalf("status output must name the problem:\n%s", out)
	}
}

func TestSkillsStatusSucceedsOnACleanProject(t *testing.T) {
	root := newSkillsStatusFixture(t)
	out, err := runSkillsStatus(t, root, true)
	if err != nil {
		t.Fatalf("status on a project with no imports must succeed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "0 total") {
		t.Fatalf("status must report the totals:\n%s", out)
	}
}

func TestSkillsStatusQuietSuppressesOutputButKeepsTheExitCode(t *testing.T) {
	root := newSkillsStatusFixture(t)
	stray := filepath.Join(root, ".agent-layer", config.ImportedSkillsDirName, "stray")
	if err := os.MkdirAll(stray, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	command := newSkillsStatusCmd()
	command.Flags().BoolP("quiet", "q", true, "")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SilenceUsage = true
	command.SilenceErrors = true
	withWorkingDirectory(t, root)
	err := command.Execute()

	if out.Len() != 0 {
		t.Fatalf("quiet must suppress the narrative, got:\n%s", out.String())
	}
	var silent *SilentExitError
	if err == nil || !asSilentExit(err, &silent) {
		t.Fatalf("quiet must still fail with an exit code, got %v", err)
	}
	if silent.Code != 1 {
		t.Fatalf("exit code = %d, want 1", silent.Code)
	}
}

// newSkillsStatusFixture creates a minimal initialized project.
func newSkillsStatusFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	layer := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(filepath.Join(layer, "skills"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	configText := `[approvals]
mode = "all"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(filepath.Join(layer, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return root
}

// runSkillsStatus executes the status command against a project root.
func runSkillsStatus(t *testing.T, root string, all bool) (string, error) {
	t.Helper()
	command := newSkillsStatusCmd()
	command.Flags().BoolP("quiet", "q", false, "")
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SilenceUsage = true
	command.SilenceErrors = true
	args := []string{}
	if all {
		args = append(args, "--all")
	}
	command.SetArgs(args)
	withWorkingDirectory(t, root)
	err := command.Execute()
	return out.String(), err
}

// withWorkingDirectory points repo-root resolution at a fixture for one test.
func withWorkingDirectory(t *testing.T, root string) {
	t.Helper()
	original := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = original })
}

func asSilentExit(err error, target **SilentExitError) bool {
	silent, ok := err.(*SilentExitError)
	if !ok {
		return false
	}
	*target = silent
	return true
}
