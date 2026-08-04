package agentdispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// seedImportedSkill writes a valid imported skill plus the lock entry that
// makes Agent Layer its owner.
func seedImportedSkill(t *testing.T, root string, name string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	dir := filepath.Join(paths.ImportedSkillsDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir imported skill: %v", err)
	}
	manifest := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\nImported body\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write imported manifest: %v", err)
	}
	lock := `{"version":1,"skills":[{"name":"` + name + `","repository":"https://example.test/skills.git",` +
		`"selector":"skills/` + name + `","selected_path":"skills/` + name + `","configured_ref":"",` +
		`"resolved_ref":"main","ref_kind":"branch","tracking":"tracked",` +
		`"commit":"` + strings.Repeat("a", 40) + `","tree_hash":"sha256:` + strings.Repeat("b", 64) + `"}]}`
	if err := os.WriteFile(paths.SkillsLockPath, []byte(lock), 0o600); err != nil {
		t.Fatalf("write skills lock: %v", err)
	}
}

// TestDispatchAcceptsAnImportedSkillName proves `al dispatch start --skill`
// validates against the locked combined source snapshot, so a Git-backed
// imported skill is selectable exactly like a user-managed one.
func TestDispatchAcceptsAnImportedSkillName(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	seedImportedSkill(t, root, "imported-helper")

	project, err := loadImportedDispatchProject(root)
	if err != nil {
		t.Fatalf("loadDispatchProject: %v", err)
	}
	if !projectHasSkill(project, "imported-helper") {
		names := make([]string, 0, len(project.Skills))
		for _, skill := range project.Skills {
			names = append(names, skill.Name)
		}
		t.Fatalf("imported skill is not visible to dispatch; loaded skills: %v", names)
	}

	prompt, err := BuildChildPrompt(project, "claude", "do the thing", "imported-helper")
	if err != nil {
		t.Fatalf("BuildChildPrompt: %v", err)
	}
	if !strings.Contains(string(prompt), "imported-helper") {
		t.Fatalf("child prompt = %q, want a reference to the imported skill", prompt)
	}

	if _, err := BuildChildPrompt(project, "claude", "do the thing", "absent-skill"); err == nil {
		t.Fatal("expected an unknown skill to be refused")
	}
}

// TestPrepareTargetProjectionMaterializesImportedSkills proves a derived
// working directory receives the imported tier from the same locked snapshot,
// so a dispatched agent can resolve the skill natively.
func TestPrepareTargetProjectionMaterializesImportedSkills(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	seedImportedSkill(t, root, "imported-helper")
	workDir := t.TempDir()

	target, ok := lookupTarget("claude")
	if !ok {
		t.Fatal("claude target is not registered")
	}
	projectionRoot, err := prepareTargetProjection(root, workDir, target)
	if err != nil {
		t.Fatalf("prepareTargetProjection: %v", err)
	}
	if projectionRoot != workDir {
		t.Fatalf("projection root = %q, want the derived working directory", projectionRoot)
	}
	projected := filepath.Join(workDir, ".claude", "skills", "imported-helper", "SKILL.md")
	data, err := os.ReadFile(projected) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("imported skill was not projected into the derived root: %v", err)
	}
	if !strings.Contains(string(data), "imported-helper") {
		t.Fatalf("projected manifest = %q", data)
	}
	if err := validateSkillProjection(projectionRoot, target, "imported-helper"); err != nil {
		t.Fatalf("validateSkillProjection: %v", err)
	}
}

// TestDispatchRefusesOrphanImportedState proves dispatch inherits the locked
// snapshot's ownership invariants instead of silently launching against an
// unmanaged imported directory.
func TestDispatchRefusesOrphanImportedState(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	dir := filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "stray")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := "---\nname: stray\ndescription: A stray skill.\n---\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if _, err := loadImportedDispatchProject(root); err == nil {
		t.Fatal("expected orphan imported state to block dispatch")
	}
}

// loadImportedDispatchProject loads the dispatch project snapshot for a root,
// dropping the writer, environment, and depth values these tests do not use.
func loadImportedDispatchProject(root string) (*config.ProjectConfig, error) {
	project, _, _, _, err := loadDispatchProject(root, nil, []string{}) //nolint:dogsled // only the snapshot is under test here.
	return project, err
}

// TestDispatchRefusesAnImportedSkillWithUnprojectableFrontmatter proves agent
// launch is held to the same strict rules import operations are. A locally
// added frontmatter field Agent Layer cannot project would otherwise be
// silently dropped, so the dispatched agent would load a lossy copy of a skill
// that `al skills status` classifies as invalid.
func TestDispatchRefusesAnImportedSkillWithUnprojectableFrontmatter(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	seedImportedSkill(t, root, "imported-helper")

	manifest := "---\nname: imported-helper\ndescription: The imported-helper skill.\nunsupported-field: value\n---\nImported body\n"
	path := filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "imported-helper", "SKILL.md")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	_, err := loadImportedDispatchProject(root)
	if err == nil {
		t.Fatal("expected unprojectable imported frontmatter to fail dispatch source loading")
	}
	if !strings.Contains(err.Error(), "unsupported-field") {
		t.Fatalf("error %q does not name the unsupported field", err)
	}
}
