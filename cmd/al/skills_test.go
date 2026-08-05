package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/skillimport"
)

// skillsTestRepoConfig is a minimally valid Agent Layer configuration for the
// `al skills` command tests.
const skillsTestRepoConfig = `[approvals]
mode = "none"

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

// newSkillsRepo creates a repository root and makes it the working directory
// so the commands resolve it the same way a user invocation does.
func newSkillsRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	agentLayer := filepath.Join(root, ".agent-layer")
	for _, dir := range []string{agentLayer, filepath.Join(agentLayer, "skills"), filepath.Join(agentLayer, "instructions")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	files := map[string]string{
		"config.toml":     skillsTestRepoConfig,
		".env":            "",
		"commands.allow":  "",
		"gitignore.block": "/.agent-layer/tmp/\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(agentLayer, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(agentLayer, "instructions", "00_rules.md"), []byte("Rules.\n"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	// os.Getwd may report the symlinked temporary path; resolve it so path
	// comparisons in assertions match what the command sees.
	resolved, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return resolved
}

// runSkillsCommand executes one `al skills` invocation and returns its stdout,
// stderr, and error.
func runSkillsCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := newRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"skills"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// TestSkillsCommandSurfaceMatchesTheContract proves the command family exposes
// exactly the specified subcommands, argument counts, and flags — and that no
// `sync` alias exists.
func TestSkillsCommandSurfaceMatchesTheContract(t *testing.T) {
	t.Parallel()
	skills := newSkillsCmd()

	got := map[string]bool{}
	for _, sub := range skills.Commands() {
		got[sub.Name()] = true
	}
	for _, want := range []string{"add", "remove", "status", "pull", "reset", "push"} {
		if !got[want] {
			t.Fatalf("missing subcommand %q", want)
		}
	}
	if got["sync"] {
		t.Fatal("an al skills sync alias was added")
	}
	if len(skills.Commands()) != 6 {
		t.Fatalf("subcommands = %d, want exactly 6", len(skills.Commands()))
	}

	find := func(name string) *cobra.Command {
		for _, sub := range skills.Commands() {
			if sub.Name() == name {
				return sub
			}
		}
		t.Fatalf("subcommand %q not found", name)
		return nil
	}

	add := find("add")
	for _, flag := range []string{"ref", "tracking", "write", "push-repository", "push-branch"} {
		if add.Flags().Lookup(flag) == nil {
			t.Fatalf("al skills add is missing --%s", flag)
		}
	}
	if err := add.Args(add, []string{"repo"}); err == nil {
		t.Fatal("al skills add accepted a repository with no selector")
	}
	if err := add.Args(add, []string{"repo", "skills/a", "skills/b"}); err != nil {
		t.Fatalf("al skills add rejected multiple selectors: %v", err)
	}

	remove := find("remove")
	if err := remove.Args(remove, []string{"repo"}); err == nil {
		t.Fatal("al skills remove accepted one argument")
	}
	if err := remove.Args(remove, []string{"repo", "a", "b"}); err == nil {
		t.Fatal("al skills remove accepted three arguments")
	}

	status := find("status")
	if status.Flags().Lookup("all") == nil {
		t.Fatal("al skills status is missing --all")
	}
	if err := status.Args(status, []string{"extra"}); err == nil {
		t.Fatal("al skills status accepted an argument")
	}

	reset := find("reset")
	if err := reset.Args(reset, nil); err == nil {
		t.Fatal("al skills reset accepted no name")
	}
	if err := reset.Args(reset, []string{"alpha", "beta"}); err == nil {
		t.Fatal("al skills reset accepted more than one name")
	}
	if err := reset.Args(reset, []string{"alpha"}); err != nil {
		t.Fatalf("al skills reset rejected exactly one name: %v", err)
	}
	if reset.Flags().Lookup("all") != nil {
		t.Fatal("al skills reset exposes a forbidden --all form")
	}
}

// TestSkillsStatusRunsWithoutImports proves status is local and read-only and
// renders a stable summary even before anything is imported.
func TestSkillsStatusRunsWithoutImports(t *testing.T) {
	newSkillsRepo(t)
	out, _, err := runSkillsCommand(t, "status")
	if err != nil {
		t.Fatalf("al skills status: %v", err)
	}
	for _, want := range []string{"imported skills: 0 total", "configured exclusions: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output %q does not contain %q", out, want)
		}
	}
}

// TestSkillsAddPullAndStatusRoundTrip proves the command family drives a real
// import end to end and that status then reflects it.
func TestSkillsAddPullAndStatusRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is required: %v", err)
	}
	root := newSkillsRepo(t)
	source := newSkillsSourceRepo(t)

	if _, _, err := runSkillsCommand(t, "add", source, "skills/alpha"); err != nil {
		t.Fatalf("al skills add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "imported-skills", "alpha", "SKILL.md")); err != nil {
		t.Fatalf("add did not import the skill: %v", err)
	}

	out, _, err := runSkillsCommand(t, "status", "--all")
	if err != nil {
		t.Fatalf("al skills status --all: %v", err)
	}
	if !strings.Contains(out, "alpha\tclean") {
		t.Fatalf("status --all = %q", out)
	}

	if _, _, err := runSkillsCommand(t, "pull"); err != nil {
		t.Fatalf("al skills pull: %v", err)
	}
	if _, _, err := runSkillsCommand(t, "push"); err != nil {
		t.Fatalf("al skills push with the default write policy: %v", err)
	}

	if _, _, err := runSkillsCommand(t, "remove", source, "skills/alpha"); err != nil {
		t.Fatalf("al skills remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "imported-skills", "alpha")); !os.IsNotExist(err) {
		t.Fatalf("remove did not retire the clean skill: %v", err)
	}
}

// newSkillsSourceRepo builds a hermetic local source repository.
func newSkillsSourceRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...) // #nosec G204 -- test-controlled arguments.
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	run("config", "user.name", "Agent Layer Test")
	run("config", "user.email", "test@example.invalid")
	skillDir := filepath.Join(dir, "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "---\nname: alpha\ndescription: The alpha skill.\n---\nAlpha body\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	run("add", "--all")
	run("commit", "--quiet", "--message", "add alpha")
	return dir
}

// TestFinishSkillsOperationExitsNonZeroOnPartialFailure proves a partially
// failed operation prints every outcome and still returns a nonzero exit
// without repeating the report as an error message.
func TestFinishSkillsOperationExitsNonZeroOnPartialFailure(t *testing.T) {
	t.Parallel()
	cmd := newSkillsPullCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)

	report := &skillimport.Report{Skills: []skillimport.SkillResult{
		{Name: "alpha", Outcome: skillimport.OutcomeImported},
		{Name: "beta", Outcome: skillimport.OutcomeFailed, Err: errors.New("merge conflict")},
	}}
	err := finishSkillsOperation(cmd, "pull", report, nil)

	var silent *SilentExitError
	if !errors.As(err, &silent) || silent.Code != 1 {
		t.Fatalf("error = %v, want a silent exit code 1", err)
	}
	for _, want := range []string{"imported alpha", "failed beta", "partially succeeded: 1 of 2"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q does not contain %q", out.String(), want)
		}
	}
	if !strings.Contains(errOut.String(), "al skills pull did not complete successfully") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// TestFinishSkillsOperationReturnsSetupFailuresDirectly proves a failure that
// prevented the operation from producing results is surfaced as an ordinary
// error rather than a silent exit.
func TestFinishSkillsOperationReturnsSetupFailuresDirectly(t *testing.T) {
	t.Parallel()
	cmd := newSkillsPullCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	setupErr := errors.New("orphan imported directory")
	if err := finishSkillsOperation(cmd, "pull", &skillimport.Report{}, setupErr); !errors.Is(err, setupErr) {
		t.Fatalf("error = %v, want the setup failure", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a setup failure printed a report: %q", out.String())
	}

	success := &skillimport.Report{Skills: []skillimport.SkillResult{{Name: "alpha", Outcome: skillimport.OutcomeImported}}}
	if err := finishSkillsOperation(cmd, "pull", success, nil); err != nil {
		t.Fatalf("a successful operation returned %v", err)
	}
}
