package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// newSourcesTestRoot creates a repository root with valid Agent Layer
// scaffolding for combined-snapshot tests.
func newSourcesTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	agentLayer := filepath.Join(root, ".agent-layer")
	for _, dir := range []string{agentLayer, filepath.Join(agentLayer, "skills"), filepath.Join(agentLayer, "instructions")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	write := func(name string, content string) {
		if err := os.WriteFile(filepath.Join(agentLayer, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("config.toml", `[approvals]
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
`)
	write(".env", "")
	write("commands.allow", "")
	write("gitignore.block", "/.agent-layer/tmp/\n")
	if err := os.WriteFile(filepath.Join(agentLayer, "instructions", "00_rules.md"), []byte("Rules.\n"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	return root
}

func writeSkillDir(t *testing.T, dir string, name string, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeSkillsLock(t *testing.T, root string, names ...string) {
	t.Helper()
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, `{"name":"`+name+`","repository":"https://example.test/skills.git","selector":"s/`+name+
			`","selected_path":"s/`+name+`","configured_ref":"","resolved_ref":"main","ref_kind":"branch",`+
			`"tracking":"tracked","commit":"`+strings.Repeat("a", 40)+`","tree_hash":"sha256:`+strings.Repeat("b", 64)+`"}`)
	}
	content := `{"version":1,"skills":[` + strings.Join(entries, ",") + `]}`
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

// TestLoadSourcesCombinesBothTiers proves projection is built from one snapshot
// that carries user-managed and imported skills, each marked with its tier.
func TestLoadSourcesCombinesBothTiers(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "local"), "local", "Local body")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "remote"), "remote", "Remote body")
	writeSkillsLock(t, root, "remote")

	project, err := LoadSources(os.DirFS(root), root)
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(project.Skills) != 2 {
		t.Fatalf("skills = %+v, want both tiers", project.Skills)
	}
	if project.Skills[0].Name != "local" || project.Skills[0].Imported {
		t.Fatalf("first skill = %+v, want the user-managed local skill", project.Skills[0])
	}
	if project.Skills[1].Name != "remote" || !project.Skills[1].Imported {
		t.Fatalf("second skill = %+v, want the imported remote skill", project.Skills[1])
	}
}

// TestLoadSourcesRejectsOrphanImportedDirectories proves the imported tier is
// fully managed: a directory with no lock entry is an actionable error.
func TestLoadSourcesRejectsOrphanImportedDirectories(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "stray"), "stray", "Body")

	_, err := LoadSources(os.DirFS(root), root)
	if err == nil {
		t.Fatal("expected an orphan imported directory to fail")
	}
	for _, want := range []string{"stray", "adopt it as user-managed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

// TestLoadSourcesRejectsCollidingNames proves one name owned by both tiers
// fails with both paths rather than silently shadowing one source.
func TestLoadSourcesRejectsCollidingNames(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "alpha"), "alpha", "Local")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "alpha"), "alpha", "Imported")
	writeSkillsLock(t, root, "alpha")

	_, err := LoadSources(os.DirFS(root), root)
	if err == nil {
		t.Fatal("expected a cross-tier name collision to fail")
	}
	for _, want := range []string{filepath.Join(".agent-layer", "skills", "alpha"), filepath.Join(".agent-layer", "imported-skills", "alpha")} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("collision error %q does not name %q", err, want)
		}
	}
}

// TestLoadSourcesFailsOnAMalformedLock proves a lockfile that cannot establish
// ownership is not treated as an empty one.
func TestLoadSourcesFailsOnAMalformedLock(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "alpha"), "alpha", "Imported")
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	if _, err := LoadSources(os.DirFS(root), root); err == nil {
		t.Fatal("expected a malformed lock to fail source loading")
	}
}

// TestRunProjectsBothTiersWithoutNetworkAccess proves ordinary sync projects
// the combined snapshot from local files only.
func TestRunProjectsBothTiersWithoutNetworkAccess(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "local"), "local", "Local body")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "remote"), "remote", "Remote body")
	writeSkillsLock(t, root, "remote")

	if _, err := Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"local", "remote"} {
		if _, err := os.Stat(filepath.Join(root, ".claude", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("%s was not projected: %v", name, err)
		}
	}
}

// TestProjectLockedRequiresASystem proves the locked projection entry point
// refuses to run without its injected filesystem operations rather than
// panicking.
func TestProjectLockedRequiresASystem(t *testing.T) {
	t.Parallel()
	if _, err := ProjectLocked(nil, t.TempDir()); err == nil {
		t.Fatal("expected a missing System to fail")
	}
	if err := WithLockedProject(nil, t.TempDir(), func(_ *config.ProjectConfig) error { return nil }); err == nil {
		t.Fatal("expected a missing System to fail")
	}
}

// TestLoadLockedSourcesReturnsTheValidatedSnapshot proves callers that need the
// combined snapshot without doing their own writes get it through the lock.
func TestLoadLockedSourcesReturnsTheValidatedSnapshot(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "local"), "local", "Local body")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "remote"), "remote", "Remote body")
	writeSkillsLock(t, root, "remote")

	project, err := LoadLockedSources(RealSystem{}, root)
	if err != nil {
		t.Fatalf("LoadLockedSources: %v", err)
	}
	if len(project.Skills) != 2 {
		t.Fatalf("skills = %+v, want both tiers", project.Skills)
	}

	// A source-loading failure inside the lock is surfaced, not swallowed.
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "stray"), "stray", "Body")
	if _, err := LoadLockedSources(RealSystem{}, root); err == nil {
		t.Fatal("expected orphan imported state to fail the locked load")
	}
}

// TestWithLockedProjectRunsInsideTheCriticalSection proves the callback both
// receives the snapshot and has its error returned to the caller.
func TestWithLockedProjectRunsInsideTheCriticalSection(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "local"), "local", "Local body")

	var seen int
	if err := WithLockedProject(RealSystem{}, root, func(project *config.ProjectConfig) error {
		seen = len(project.Skills)
		return nil
	}); err != nil {
		t.Fatalf("WithLockedProject: %v", err)
	}
	if seen != 1 {
		t.Fatalf("callback saw %d skills, want 1", seen)
	}

	callbackErr := errors.New("callback failed")
	if err := WithLockedProject(RealSystem{}, root, func(*config.ProjectConfig) error {
		return callbackErr
	}); !errors.Is(err, callbackErr) {
		t.Fatalf("error = %v, want the callback error", err)
	}
}

// TestLoadSourcesRejectsAnImportedSkillWithUnprojectableFrontmatter proves the
// imported tier is held to the same strict rules its import was accepted under.
// Generic configuration loading keeps only the fields it can project, so an
// unsupported field would otherwise be silently dropped from the projection
// while `al skills status` reported the same tree as invalid.
func TestLoadSourcesRejectsAnImportedSkillWithUnprojectableFrontmatter(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	dir := filepath.Join(root, ".agent-layer", "imported-skills", "remote")
	writeSkillDir(t, dir, "remote", "Remote body")
	manifest := "---\nname: remote\ndescription: The remote skill.\nunsupported-field: value\n---\nRemote body\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	writeSkillsLock(t, root, "remote")

	_, err := LoadSources(os.DirFS(root), root)
	if err == nil {
		t.Fatal("expected unprojectable imported frontmatter to fail source loading")
	}
	for _, want := range []string{"remote", "unsupported-field"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}

// TestLoadSourcesRejectsAnImportedSkillWithALowercaseManifest proves the
// canonical manifest requirement applies to imported sources. The generic
// loader keeps its lowercase compatibility fallback for user-managed skills,
// but an imported tree that no import operation would accept must not project.
func TestLoadSourcesRejectsAnImportedSkillWithALowercaseManifest(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	dir := filepath.Join(root, ".agent-layer", "imported-skills", "remote")
	writeSkillDir(t, dir, "remote", "Remote body")
	if err := os.Rename(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "skill.md")); err != nil {
		t.Fatalf("rename manifest: %v", err)
	}
	writeSkillsLock(t, root, "remote")

	_, err := LoadSources(os.DirFS(root), root)
	if err == nil {
		t.Fatal("expected a lowercase imported manifest to fail source loading")
	}
	if !strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("error %q does not name the canonical manifest", err)
	}
}

// TestLoadSourcesKeepsTheUserManagedLowercaseFallback proves the strict rule is
// scoped to the imported tier, so an existing user-managed skill that predates
// imports keeps syncing exactly as before.
func TestLoadSourcesKeepsTheUserManagedLowercaseFallback(t *testing.T) {
	t.Parallel()
	root := newSourcesTestRoot(t)
	dir := filepath.Join(root, ".agent-layer", "skills", "local")
	writeSkillDir(t, dir, "local", "Local body")
	if err := os.Rename(filepath.Join(dir, "SKILL.md"), filepath.Join(dir, "skill.md")); err != nil {
		t.Fatalf("rename manifest: %v", err)
	}

	project, err := LoadSources(os.DirFS(root), root)
	if err != nil {
		t.Fatalf("LoadSources: %v", err)
	}
	if len(project.Skills) != 1 || project.Skills[0].Name != "local" {
		t.Fatalf("skills = %+v, want the user-managed skill", project.Skills)
	}
}
