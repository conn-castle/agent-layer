package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// importedSkillsFixture builds a repo whose .agent-layer contains both a
// user-managed and an imported skill with the same shape, so a projection
// difference between the tiers shows up as a test failure.
func importedSkillsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	if err := copyFixtureRepo(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	writeTemplateToFixtureSource(t, root, "claude-statusline.sh", filepath.Join(".agent-layer", "claude-statusline.sh"), 0o755)
	writeTemplateToFixtureSource(t, root, "codex-statusline.toml", filepath.Join(".agent-layer", "codex-statusline.toml"), 0o644)
	envPath := filepath.Join(root, ".agent-layer", ".env")
	if err := os.WriteFile(envPath, []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	return root
}

// writeSkillTree materializes a skill with the same resources under a tier root.
func writeSkillTree(t *testing.T, root string, tier string, name string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-layer", tier, name)
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	files := map[string]struct {
		content string
		mode    os.FileMode
	}{
		"SKILL.md":      {"---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n\nBody for " + name + "\n", 0o644},
		".resourcerc":   {"hidden resource\n", 0o644},
		"scripts/go.sh": {"#!/bin/sh\necho " + name + "\n", 0o755},
	}
	for relative, spec := range files {
		target := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.WriteFile(target, []byte(spec.content), spec.mode); err != nil {
			t.Fatalf("write %s: %v", target, err)
		}
		if err := os.Chmod(target, spec.mode); err != nil {
			t.Fatalf("chmod %s: %v", target, err)
		}
	}
}

// writeImportLock writes a lock that claims ownership of the named skills.
func writeImportLock(t *testing.T, root string, names ...string) {
	t.Helper()
	entries := make([]config.SkillImportLockEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, config.SkillImportLockEntry{
			Repository:       "https://example.invalid/skills.git",
			SourcePath:       "skills/" + name,
			RefOmitted:       true,
			ResolvedRefName:  "main",
			ResolvedRefType:  config.SkillRefBranch,
			SourceCommit:     strings.Repeat("a", 40),
			UpstreamTreeHash: "sha256-v1:" + strings.Repeat("b", 64),
			Tracking:         config.SkillTrackingTracked,
			Write:            config.SkillWriteNone,
			PushRepository:   "https://example.invalid/skills.git",
			SkillName:        name,
		})
	}
	data, err := config.MarshalSkillImportLock(&config.SkillImportLock{
		Version: config.SkillImportLockVersion, Entries: entries,
	})
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	path := filepath.Join(root, ".agent-layer", config.SkillImportLockFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

func TestSyncProjectsImportedSkillsIdenticallyToUserManagedOnes(t *testing.T) {
	root := importedSkillsFixture(t)
	writeSkillTree(t, root, "skills", "user-tier")
	writeSkillTree(t, root, config.ImportedSkillsDirName, "import-tier")
	writeImportLock(t, root, "import-tier")

	if _, err := Run(root); err != nil {
		t.Fatalf("sync: %v", err)
	}

	for _, clientDir := range []string{
		filepath.Join(root, ".claude", "skills"),
		filepath.Join(root, ".agents", "skills"),
	} {
		userManifest := readProjectedFile(t, filepath.Join(clientDir, "user-tier", "SKILL.md"))
		importedManifest := readProjectedFile(t, filepath.Join(clientDir, "import-tier", "SKILL.md"))

		// Each projection is a byte-for-byte copy of its own source, so the two
		// differ only where the sources differ: the skill's own name.
		for tier, projected := range map[string]string{"user-tier": userManifest, "import-tier": importedManifest} {
			source := readProjectedFile(t, filepath.Join(root, ".agent-layer", sourceTierDir(tier), tier, "SKILL.md"))
			if projected != source {
				t.Fatalf("%s projection is not byte-identical to its source in %s:\nwant %q\ngot  %q",
					tier, clientDir, source, projected)
			}
		}
		if strings.ReplaceAll(importedManifest, "import-tier", "user-tier") != userManifest {
			t.Fatalf("the two tiers project differently in %s:\n%s\n---\n%s",
				clientDir, userManifest, importedManifest)
		}
		// Import provenance belongs in config and lock, never in skill content,
		// and no Agent Layer header is injected either.
		for _, leak := range []string{
			"example.invalid", "skills.git", "tracked", strings.Repeat("a", 40), generatedMarkerHeader,
		} {
			if strings.Contains(importedManifest, leak) {
				t.Fatalf("%q leaked into the projected skill:\n%s", leak, importedManifest)
			}
		}

		// The exhaustive resource set applies to both tiers, including dotfiles and
		// the executable bit.
		for _, tier := range []string{"user-tier", "import-tier"} {
			if _, err := os.Stat(filepath.Join(clientDir, tier, ".resourcerc")); err != nil {
				t.Fatalf("%s: hidden resource was not projected: %v", tier, err)
			}
			info, err := os.Stat(filepath.Join(clientDir, tier, "scripts", "go.sh"))
			if err != nil {
				t.Fatalf("%s: script was not projected: %v", tier, err)
			}
			if info.Mode().Perm() != skillResourceExecutableMode {
				t.Fatalf("%s: script mode = %v, want %v", tier, info.Mode().Perm(), skillResourceExecutableMode)
			}
		}
	}
}

func TestSyncFailsOnAnOrphanImportedSkillDirectory(t *testing.T) {
	root := importedSkillsFixture(t)
	writeSkillTree(t, root, config.ImportedSkillsDirName, "unmanaged")

	_, err := Run(root)
	if err == nil {
		t.Fatal("a managed directory with no lock entry must stop sync rather than be silently projected")
	}
	if !strings.Contains(err.Error(), "adopt it") {
		t.Fatalf("the error must be actionable: %v", err)
	}
}

func TestSyncFailsOnCrossTierSkillNameCollision(t *testing.T) {
	root := importedSkillsFixture(t)
	writeSkillTree(t, root, "skills", "shared")
	writeSkillTree(t, root, config.ImportedSkillsDirName, "shared")
	writeImportLock(t, root, "shared")

	_, err := Run(root)
	if err == nil {
		t.Fatal("one name in both tiers must fail rather than let one source silently shadow the other")
	}
	if !strings.Contains(err.Error(), "narrow the import selector") {
		t.Fatalf("the error must explain how to resolve the collision: %v", err)
	}
}

func TestSyncNeverRunsGitForSkillImports(t *testing.T) {
	root := importedSkillsFixture(t)
	writeSkillTree(t, root, config.ImportedSkillsDirName, "import-tier")
	writeImportLock(t, root, "import-tier")

	// A PATH with no git at all: ordinary sync must still succeed, which is only
	// possible if it never contacts a skill remote.
	t.Setenv("PATH", t.TempDir())

	if _, err := Run(root); err != nil {
		t.Fatalf("ordinary sync must be network-free and git-free: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "import-tier", "SKILL.md")); err != nil {
		t.Fatalf("the imported skill was not projected: %v", err)
	}
}

// sourceTierDir maps a fixture skill name to the .agent-layer root it came from.
func sourceTierDir(tier string) string {
	if tier == "import-tier" {
		return config.ImportedSkillsDirName
	}
	return "skills"
}

func readProjectedFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
