package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const releasedProjectionMarker = "<!--\n  GENERATED FILE\n  Source: .agent-layer/skills/alpha/SKILL.md\n  Regenerate: al sync\n-->\n"

func TestExclusiveSkillRootPreflightReportsEveryBlockingEntry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, ".agents", "skills", "manual"),
		filepath.Join(root, ".agents", "skills", "loose.txt"),
		filepath.Join(root, ".claude", "skills", "linked"),
	}
	if err := os.MkdirAll(paths[0], 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths[1]), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths[1], []byte("manual"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(paths[2]), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", paths[2]); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.preflightExclusiveSkillRoots()
	if err == nil {
		t.Fatal("expected preflight to block")
	}
	for _, path := range paths {
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error %q does not name %s", err, path)
		}
	}
	for _, guidance := range []string{".agent-layer/skills", "remove", "retry"} {
		if !strings.Contains(err.Error(), guidance) {
			t.Fatalf("error %q lacks %q guidance", err, guidance)
		}
	}
}

func TestExclusiveSkillRootPreflightAcceptsReleasedMarkers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{filepath.Join(".agents", "skills", "alpha"), filepath.Join(".claude", "skills", "alpha")} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(releasedProjectionMarker), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "resources", "reference.md"), []byte("released projection resource"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lowercaseDir := filepath.Join(root, ".agents", "skills", "lowercase-release")
	if err := os.MkdirAll(lowercaseDir, 0o750); err != nil {
		t.Fatal(err)
	}
	lowercaseMarker := strings.Replace(releasedProjectionMarker, "skills/alpha/SKILL.md", "skills/lowercase-release/skill.md", 1)
	if err := os.WriteFile(filepath.Join(lowercaseDir, "SKILL.md"), []byte(lowercaseMarker), 0o600); err != nil {
		t.Fatal(err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.preflightExclusiveSkillRoots(); err != nil {
		t.Fatalf("released projections must be safe to replace: %v", err)
	}
}

// writeSkillDir creates dir with a marker-free manifest, the shape both the
// byte-exact source tiers and their projections have.
func writeSkillDir(t *testing.T, dir string, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: " + name + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Projections stopped carrying a generated header when skill trees became
// byte-exact copies of their source. Without this, a repository synced by the
// claiming version can never complete the claim: its own output is
// indistinguishable from user content and every entry blocks forever.
func TestExclusiveSkillRootPreflightAcceptsMarkerFreeProjectionsOfCurrentSources(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "user-tier"), "user-tier")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills-imported", "imported-tier"), "imported-tier")
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "imported-skills", "legacy-tier"), "legacy-tier")
	for _, client := range []string{".agents", ".claude"} {
		for _, name := range []string{"user-tier", "imported-tier", "legacy-tier"} {
			writeSkillDir(t, filepath.Join(root, client, "skills", name), name)
		}
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.preflightExclusiveSkillRoots(); err != nil {
		t.Fatalf("projections of current sources must be safe to replace: %v", err)
	}
}

// Name matching is only sound because a source skill's client directory is
// rewritten on every sync. A name with no source behind it has never been
// projected, so it is user content and must still block.
func TestExclusiveSkillRootPreflightBlocksNamesNoSourceTierProjects(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "projected"), "projected")
	// A source directory without a manifest is not a projectable skill and must
	// not launder a client-root entry of the same name.
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "not-a-skill"), 0o750); err != nil {
		t.Fatal(err)
	}
	manifestDir := filepath.Join(root, ".agent-layer", "skills", "manifest-is-directory")
	if err := os.MkdirAll(filepath.Join(manifestDir, "SKILL.md"), 0o750); err != nil {
		t.Fatal(err)
	}
	handWritten := filepath.Join(root, ".claude", "skills", "hand-written")
	notASkill := filepath.Join(root, ".claude", "skills", "not-a-skill")
	manifestIsDirectory := filepath.Join(root, ".claude", "skills", "manifest-is-directory")
	writeSkillDir(t, handWritten, "hand-written")
	writeSkillDir(t, notASkill, "not-a-skill")
	writeSkillDir(t, manifestIsDirectory, "manifest-is-directory")
	writeSkillDir(t, filepath.Join(root, ".claude", "skills", "projected"), "projected")
	mustBlock := []string{handWritten, notASkill, manifestIsDirectory}

	linkedManifest := filepath.Join(root, ".agent-layer", "skills", "linked-manifest")
	if err := os.MkdirAll(linkedManifest, 0o750); err != nil {
		t.Fatal(err)
	}
	manifestTarget := filepath.Join(root, "linked-manifest-target.md")
	if err := os.WriteFile(manifestTarget, []byte("---\nname: linked-manifest\ndescription: linked\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(manifestTarget, filepath.Join(linkedManifest, "SKILL.md")); err == nil {
		linkedManifestClient := filepath.Join(root, ".claude", "skills", "linked-manifest")
		writeSkillDir(t, linkedManifestClient, "linked-manifest")
		mustBlock = append(mustBlock, linkedManifestClient)
	} else {
		t.Logf("symlinks unavailable, skipping the symlinked-manifest case: %v", err)
	}

	err := (&installer{root: root, sys: RealSystem{}}).preflightExclusiveSkillRoots()
	if err == nil {
		t.Fatal("expected preflight to block content no source tier projects")
	}
	for _, path := range mustBlock {
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("error %q does not name %s", err, path)
		}
	}
	if strings.Contains(err.Error(), filepath.Join(root, ".claude", "skills", "projected")) {
		t.Fatalf("error %q blocks a projection of a current source", err)
	}
}

// An unreadable source manifest must fail the preflight outright. Silently
// treating it as a non-skill would drop the name from the projected set and
// misreport the skill's own client projection as blocking user content.
func TestExclusiveSkillRootPreflightSurfacesManifestInspectionErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSkillDir(t, filepath.Join(root, ".agent-layer", "skills", "projected"), "projected")
	writeSkillDir(t, filepath.Join(root, ".claude", "skills", "projected"), "projected")

	sys := newFaultSystem(RealSystem{})
	manifestPath := filepath.Join(root, ".agent-layer", "skills", "projected", "SKILL.md")
	sys.lstatErrs[manifestPath] = os.ErrPermission

	err := (&installer{root: root, sys: sys}).preflightExclusiveSkillRoots()
	if err == nil || !strings.Contains(err.Error(), manifestPath) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("manifest inspection failure was not surfaced: %v", err)
	}
	if strings.Contains(err.Error(), "cannot claim the client skill roots") {
		t.Fatalf("inspection failure was misreported as blocking user content: %v", err)
	}
}

func TestExclusiveSkillRootPreflightRejectsNonDirectoryRootAndMarkerTextOutsideHeader(t *testing.T) {
	t.Parallel()

	t.Run("client root is a regular file", func(t *testing.T) {
		root := t.TempDir()
		clientRoot := filepath.Join(root, ".agents", "skills")
		if err := os.MkdirAll(filepath.Dir(clientRoot), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(clientRoot, []byte("manual"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := (&installer{root: root, sys: RealSystem{}}).preflightExclusiveSkillRoots()
		if err == nil || !strings.Contains(err.Error(), clientRoot) {
			t.Fatalf("non-directory client root was not blocked: %v", err)
		}
	})

	t.Run("marker phrases occur only in manual content", func(t *testing.T) {
		root := t.TempDir()
		skillDir := filepath.Join(root, ".claude", "skills", "manual")
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			t.Fatal(err)
		}
		manifest := "---\nname: manual\ndescription: manual\n---\nGENERATED FILE\nSource: .agent-layer/\nRegenerate: al sync\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		err := (&installer{root: root, sys: RealSystem{}}).preflightExclusiveSkillRoots()
		if err == nil || !strings.Contains(err.Error(), skillDir) {
			t.Fatalf("manual marker text was accepted as a released header: %v", err)
		}
	})

	t.Run("anchored marker names an impossible released source", func(t *testing.T) {
		root := t.TempDir()
		skillDir := filepath.Join(root, ".claude", "skills", "manual")
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			t.Fatal(err)
		}
		manifest := strings.Replace(releasedProjectionMarker, "skills/alpha/SKILL.md", "manual-content", 1)
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		err := (&installer{root: root, sys: RealSystem{}}).preflightExclusiveSkillRoots()
		if err == nil || !strings.Contains(err.Error(), skillDir) {
			t.Fatalf("impossible released source was accepted: %v", err)
		}
	})
}

func TestExclusiveSkillRootUpgradeCheckRunsOnlyWhileMigrationIsPending(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manual := filepath.Join(root, ".agents", "skills", "marker-free-generated", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(manual), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manual, []byte("---\nname: marker-free-generated\ndescription: generated\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.preflightPendingExclusiveSkillRoots(); err != nil {
		t.Fatalf("completed transition was reinspected: %v", err)
	}
	inst.pendingMigrationOps = []upgradeMigrationOperation{{Kind: upgradeMigrationKindClaimSkillRoots}}
	if err := inst.preflightPendingExclusiveSkillRoots(); err == nil || !strings.Contains(err.Error(), "marker-free-generated") {
		t.Fatalf("pending transition did not block marker-free content: %v", err)
	}
}

func TestInitSkillRootPreflightRunsBeforeMutation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manual := filepath.Join(root, ".claude", "skills", "manual", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(manual), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manual, []byte("manual"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Run(root, Options{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), filepath.Dir(manual)) {
		t.Fatalf("init did not reject existing client content: %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, ".agent-layer")); !os.IsNotExist(statErr) {
		t.Fatalf("init mutated the repository before preflight: %v", statErr)
	}
}

func TestRunExclusiveSkillRootTransitionIsPendingOnlyAndPreMutation(t *testing.T) {
	t.Run("0.15 to 0.16 blocks before mutation", func(t *testing.T) {
		root := t.TempDir()
		if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.15.0"}); err != nil {
			t.Fatalf("seed 0.15.0 install: %v", err)
		}
		manual := filepath.Join(root, ".agents", "skills", "manual", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(manual), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manual, []byte("manual"), 0o600); err != nil {
			t.Fatal(err)
		}

		versionPath := filepath.Join(root, ".agent-layer", "al.version")
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		beforeVersion, err := os.ReadFile(versionPath) // #nosec G304 -- test-controlled path.
		if err != nil {
			t.Fatal(err)
		}
		beforeConfig, err := os.ReadFile(configPath) // #nosec G304 -- test-controlled path.
		if err != nil {
			t.Fatal(err)
		}

		err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.16.0"})
		if err == nil || !strings.Contains(err.Error(), filepath.Dir(manual)) {
			t.Fatalf("pending transition did not block manual content: %v", err)
		}
		afterVersion, readErr := os.ReadFile(versionPath) // #nosec G304 -- test-controlled path.
		if readErr != nil || string(afterVersion) != string(beforeVersion) {
			t.Fatalf("version mutated before preflight completed: %q, %v", afterVersion, readErr)
		}
		afterConfig, readErr := os.ReadFile(configPath) // #nosec G304 -- test-controlled path.
		if readErr != nil || string(afterConfig) != string(beforeConfig) {
			t.Fatalf("config mutated before preflight completed: %v", readErr)
		}
		if _, statErr := os.Lstat(filepath.Join(root, upgradeSnapshotDirRelPath)); !os.IsNotExist(statErr) {
			t.Fatalf("upgrade snapshot was created before preflight completed: %v", statErr)
		}
	})

	t.Run("0.16 to 0.16 accepts marker-free projection", func(t *testing.T) {
		root := t.TempDir()
		if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.16.0"}); err != nil {
			t.Fatalf("seed 0.16.0 install: %v", err)
		}
		projection := filepath.Join(root, ".agents", "skills", "generated", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(projection), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(projection, []byte("---\nname: generated\ndescription: generated\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.16.0"}); err != nil {
			t.Fatalf("completed transition was reinspected: %v", err)
		}
	})
}
