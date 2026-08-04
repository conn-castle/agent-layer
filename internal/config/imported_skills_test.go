package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// importedSkillsProject builds a repo root with a valid config and the given
// user-managed and imported skills.
type importedSkillsProject struct {
	t    *testing.T
	root string
}

func newImportedSkillsProject(t *testing.T) *importedSkillsProject {
	t.Helper()
	root := t.TempDir()
	layer := filepath.Join(root, ".agent-layer")
	for _, dir := range []string{"skills", "instructions"} {
		if err := os.MkdirAll(filepath.Join(layer, dir), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	write := func(path string, content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	write(filepath.Join(layer, "config.toml"), skillImportBaseConfig)
	write(filepath.Join(layer, ".env"), "")
	write(filepath.Join(layer, "commands.allow"), "")
	write(filepath.Join(layer, "instructions", "00_rules.md"), "# Rules\n")
	return &importedSkillsProject{t: t, root: root}
}

func (p *importedSkillsProject) writeSkill(tier string, name string) {
	p.t.Helper()
	dir := filepath.Join(p.root, ".agent-layer", tier, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		p.t.Fatalf("mkdir: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		p.t.Fatalf("write skill: %v", err)
	}
}

func (p *importedSkillsProject) writeLock(names ...string) {
	p.t.Helper()
	entries := make([]SkillImportLockEntry, 0, len(names))
	for i, name := range names {
		entries = append(entries, SkillImportLockEntry{
			Repository:      "https://example.invalid/skills.git",
			SourcePath:      "skills/" + name,
			RefOmitted:      true,
			ResolvedRefName: "main",
			ResolvedRefType: SkillRefBranch,
			SourceCommit:    strings.Repeat("a", 40),
			UpstreamTreeHash: "sha256-v1:" + strings.Repeat("b", 64-len(name)) +
				strings.Repeat("c", len(name)),
			Tracking:       SkillTrackingTracked,
			Write:          SkillWriteNone,
			PushRepository: "https://example.invalid/skills.git",
			SkillName:      name,
		})
		_ = i
	}
	data, err := MarshalSkillImportLock(&SkillImportLock{Version: SkillImportLockVersion, Entries: entries})
	if err != nil {
		p.t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.root, ".agent-layer", SkillImportLockFileName), data, 0o600); err != nil {
		p.t.Fatalf("write lock: %v", err)
	}
}

func (p *importedSkillsProject) load() (*ProjectConfig, error) {
	p.t.Helper()
	return LoadProjectConfigFS(os.DirFS(p.root), p.root)
}

func TestLoadProjectConfigMergesBothSkillTiers(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	p.writeSkill("skills", "local-helper")
	p.writeSkill(ImportedSkillsDirName, "code-review")
	p.writeLock("code-review")

	project, err := p.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(project.Skills) != 2 {
		t.Fatalf("skills = %d, want both tiers merged", len(project.Skills))
	}
	byName := map[string]Skill{}
	for _, skill := range project.Skills {
		byName[skill.Name] = skill
	}
	if !byName["code-review"].Imported {
		t.Fatal("an imported skill must be marked as imported for reporting surfaces")
	}
	if byName["local-helper"].Imported {
		t.Fatal("a user-managed skill must not be marked as imported")
	}
	// Provenance stays in config and lock; it must never be injected into the
	// skill's own content.
	if strings.Contains(byName["code-review"].Body, "example.invalid") {
		t.Fatal("import provenance leaked into skill content")
	}
}

func TestLoadProjectConfigWorksWithoutAnImportedSkillsDirectory(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	p.writeSkill("skills", "local-helper")

	project, err := p.load()
	if err != nil {
		t.Fatalf("a project with no imports must load: %v", err)
	}
	if len(project.Skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(project.Skills))
	}
}

func TestLoadProjectConfigRejectsAnOrphanImportedDirectory(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	p.writeSkill(ImportedSkillsDirName, "stray")

	_, err := p.load()
	if err == nil {
		t.Fatal("a managed directory with no lock entry must be an actionable error, not silently projected")
	}
	if !strings.Contains(err.Error(), "no entry in") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "adopt it") {
		t.Fatalf("the error must tell the user how to resolve it: %v", err)
	}
}

func TestLoadProjectConfigRejectsCrossTierNameCollision(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	p.writeSkill("skills", "code-review")
	p.writeSkill(ImportedSkillsDirName, "code-review")
	p.writeLock("code-review")

	_, err := p.load()
	if err == nil {
		t.Fatal("one name in both tiers must fail rather than let one source silently shadow the other")
	}
	// Both paths must appear so the user can see exactly which two files collide.
	if !strings.Contains(err.Error(), filepath.Join(".agent-layer", "skills", "code-review")) {
		t.Fatalf("the user-managed path is missing from the error: %v", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(".agent-layer", ImportedSkillsDirName, "code-review")) {
		t.Fatalf("the imported path is missing from the error: %v", err)
	}
}

func TestLoadProjectConfigSurfacesAMalformedLock(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	lockPath := filepath.Join(p.root, ".agent-layer", SkillImportLockFileName)
	if err := os.WriteFile(lockPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	_, err := p.load()
	if err == nil {
		t.Fatal("a lock Agent Layer cannot trust must stop the load rather than be ignored")
	}
}

func TestLoadProjectConfigNamesAnUnmanagedDirectoryEvenWhenItIsEmpty(t *testing.T) {
	t.Parallel()
	p := newImportedSkillsProject(t)
	// An empty directory nobody owns must produce the ownership error, not a
	// "this skill is malformed" error about a file the user never wrote.
	stray := filepath.Join(p.root, ".agent-layer", ImportedSkillsDirName, "stray")
	if err := os.MkdirAll(stray, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := p.load()
	if err == nil {
		t.Fatal("an unmanaged directory must stop the load")
	}
	if !strings.Contains(err.Error(), "adopt it") {
		t.Fatalf("error = %v, want the adopt-or-remove guidance", err)
	}
	if strings.Contains(err.Error(), "SKILL.md") {
		t.Fatalf("the error must be about ownership, not contents: %v", err)
	}
}
