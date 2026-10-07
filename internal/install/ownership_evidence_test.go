package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckRemovalEvidence_DocsAgentLayer_UsesBaseline(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(baselinePath), 0o700); err != nil {
		t.Fatalf("mkdir baseline: %v", err)
	}
	content := []byte("# ROADMAP\n\nLegacy header\n\n<!-- PHASES START -->\n")
	if err := os.WriteFile(localPath, content, 0o600); err != nil {
		t.Fatalf("write local: %v", err)
	}
	if err := os.WriteFile(baselinePath, content, 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.checkRemovalEvidence("docs/agent-layer/ROADMAP.md")
	if err != nil {
		t.Fatalf("checkRemovalEvidence: %v", err)
	}
}

func TestCheckRemovalEvidence_TemplatesDocs_Shortcut(t *testing.T) {
	root := t.TempDir()
	orphanPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o700); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan template snapshot\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.checkRemovalEvidence(".agent-layer/templates/docs/ROADMAP.md")
	if err != nil {
		t.Fatalf("checkRemovalEvidence: %v", err)
	}
}

func TestCheckRemovalEvidence_MissingBaselineAndDefaultFallback(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(docsPath, []byte("# ISSUES\n\nLocal header\n\n<!-- ENTRIES START -->\n"), 0o600); err != nil {
		t.Fatalf("write docs orphan: %v", err)
	}
	defaultPath := filepath.Join(root, ".agent-layer", "skills", "local-orphan.md")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o700); err != nil {
		t.Fatalf("mkdir default orphan dir: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte("local orphan\n"), 0o600); err != nil {
		t.Fatalf("write default orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.checkRemovalEvidence("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("checkRemovalEvidence docs: %v", err)
	}

	err = inst.checkRemovalEvidence(".agent-layer/skills/local-orphan.md")
	if err != nil {
		t.Fatalf("checkRemovalEvidence default: %v", err)
	}
}

func TestCheckOwnershipEvidence_CanonicalMixedAndReordered(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	baselineContent := []byte("git status\ngit diff\n")

	baselineComp, err := buildOwnershipComparable(relPath, baselineContent)
	if err != nil {
		t.Fatalf("build baseline comparable: %v", err)
	}
	baselinePayload, err := ownershipPolicyPayload(baselineComp)
	if err != nil {
		t.Fatalf("build baseline payload: %v", err)
	}
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               relPath,
			FullHashNormalized: baselineComp.FullHash,
			PolicyID:           baselineComp.PolicyID,
			PolicyPayload:      baselinePayload,
		}},
	}
	if err := writeManagedBaselineState(root, RealSystem{}, state); err != nil {
		t.Fatalf("write baseline state: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err = inst.checkOwnershipEvidence(
		relPath,
		[]byte("git status\ngit diff\ngit rev-parse --show-toplevel\n"),
		[]byte("git status\ngit diff\ngit log --oneline\n"),
		false,
	)
	if err != nil {
		t.Fatalf("classify mixed: %v", err)
	}

	err = inst.checkOwnershipEvidence(
		relPath,
		[]byte("git diff\ngit status\n"),
		[]byte("git status\ngit diff\n"),
		false,
	)
	if err != nil {
		t.Fatalf("classify reordered: %v", err)
	}
}

func TestCheckOwnershipEvidence_PolicyMismatch(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	rawPayload := []byte(`{"marker":"` + ownershipMarkerEntriesStart + `","managed_section_hash":"abc"}`)
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               relPath,
			FullHashNormalized: strings.Repeat("a", 64),
			PolicyID:           ownershipPolicyMemoryEntries,
			PolicyPayload:      rawPayload,
		}},
	}
	if err := writeManagedBaselineState(root, RealSystem{}, state); err != nil {
		t.Fatalf("write baseline state: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	err := inst.checkOwnershipEvidence(relPath, []byte("git status\n"), []byte("git status\n"), false)
	if err != nil {
		t.Fatalf("checkOwnershipEvidence: %v", err)
	}
}

func TestCheckBaselineEvidence_PinManifestAndLegacyPaths(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	localComp, err := buildOwnershipComparable(relPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("9.9.9\n"), 0o600); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	err = inst.checkBaselineEvidence(relPath, localComp)
	if err != nil {
		t.Fatalf("checkBaselineEvidence missing manifest: %v", err)
	}

	docsRelPath := "docs/agent-layer/BACKLOG.md"
	legacyPath := filepath.Join(root, ".agent-layer", "templates", "docs", "BACKLOG.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("# BACKLOG\n\nmissing marker\n"), 0o600); err != nil {
		t.Fatalf("write legacy docs: %v", err)
	}
	err = inst.checkLegacyDocsBaseline(docsRelPath)
	if err == nil {
		t.Fatal("expected parse error for malformed legacy docs snapshot")
	}
}

func TestCheckBaselineEvidence_ReadErrors(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	localComp, err := buildOwnershipComparable(relPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}

	// Corrupted canonical baseline should fail loudly.
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{bad-json"), 0o600); err != nil {
		t.Fatalf("write corrupted baseline: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.checkBaselineEvidence(relPath, localComp); err == nil {
		t.Fatal("expected canonical baseline decode error")
	}

	// Non-ENOENT pin read failures should also fail loudly.
	readFault := newFaultSystem(RealSystem{})
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	readFault.readErrs[normalizePath(pinPath)] = errors.New("read boom")
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remove state file: %v", err)
	}
	inst = &installer{root: root, sys: readFault}
	if err := inst.checkBaselineEvidence(relPath, localComp); err == nil {
		t.Fatal("expected pin read error")
	}
}

func TestMatchAnyOtherManifest(t *testing.T) {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		t.Fatalf("load manifests: %v", err)
	}

	type candidate struct {
		path   string
		pinned string
		key    string
	}
	var found *candidate
	for pinnedVersion, manifest := range manifests {
		entries := manifestFileMap(manifest.Files)
		for path, entry := range entries {
			comp, compErr := comparableFromManifestEntry(entry)
			if compErr != nil {
				t.Fatalf("comparable from entry: %v", compErr)
			}
			key := comparableKey(comp)
			for otherVersion, otherManifest := range manifests {
				if otherVersion == pinnedVersion {
					continue
				}
				otherEntry, ok := manifestFileMap(otherManifest.Files)[path]
				if !ok {
					continue
				}
				otherComp, otherErr := comparableFromManifestEntry(otherEntry)
				if otherErr != nil {
					t.Fatalf("comparable from other entry: %v", otherErr)
				}
				if comparableKey(otherComp) == key {
					found = &candidate{path: path, pinned: pinnedVersion, key: key}
					break
				}
			}
			if found != nil {
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		t.Skip("no cross-version comparable key found")
	}

	ok, err := matchAnyOtherManifest(found.path, found.pinned, found.key)
	if err != nil {
		t.Fatalf("matchAnyOtherManifest: %v", err)
	}
	if !ok {
		t.Fatalf("expected matchAnyOtherManifest to find another version for %s", found.path)
	}

	ok, err = matchAnyOtherManifest(found.path, found.pinned, "does-not-exist")
	if err != nil {
		t.Fatalf("matchAnyOtherManifest with missing key: %v", err)
	}
	if ok {
		t.Fatal("expected no match for unknown key")
	}
}

func TestCheckOwnershipEvidence_ParseGates(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	sys.readErrs[filepath.Join(root, filepath.FromSlash(baselineStateRelPath))] = errors.New("must not read baseline")
	inst := &installer{root: root, sys: sys}
	relPath := "docs/agent-layer/BACKLOG.md"
	valid := []byte("# header\n" + ownershipMarkerEntriesStart + "\n")
	invalid := []byte("# no marker\n")
	require.NoError(t, inst.checkOwnershipEvidence(relPath, invalid, invalid, false))
	err := inst.checkOwnershipEvidence(relPath, valid, invalid, false)
	require.EqualError(t, err, `parse target comparable for docs/agent-layer/BACKLOG.md (section_marker_missing): ownership marker "<!-- ENTRIES START -->" missing`)
}

func TestCheckBaselineEvidence_ShortCircuits(t *testing.T) {
	t.Run("canonical entry skips pin", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, Run(root, Options{System: RealSystem{}}))
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[filepath.Join(root, ".agent-layer", "al.version")] = errors.New("pin must not be read")
		inst := &installer{root: root, sys: sys}
		comp, err := buildOwnershipComparable(commandsAllowRelPath, []byte("custom\n"))
		require.NoError(t, err)
		require.NoError(t, inst.checkBaselineEvidence(commandsAllowRelPath, comp))
	})
	for _, gate := range []string{"equal pinned key", "different pinned key", "different pinned policy"} {
		t.Run(gate, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte("0.7.0\n"), 0o600))
			manifest, err := loadTemplateManifestByVersion("0.7.0")
			require.NoError(t, err)
			relPath := "docs/agent-layer/BACKLOG.md"
			comp, err := comparableFromManifestEntry(manifestFileMap(manifest.Files)[relPath])
			require.NoError(t, err)
			sys := newFaultSystem(RealSystem{})
			failure := errors.New("legacy read denied")
			sys.readErrs[filepath.Join(root, ".agent-layer", "templates", "docs", "BACKLOG.md")] = failure
			inst := &installer{root: root, sys: sys}
			switch gate {
			case "different pinned key":
				comp.ManagedHash = strings.Repeat("f", 64)
			case "different pinned policy":
				comp.PolicyID = ""
			}
			err = inst.checkBaselineEvidence(relPath, comp)
			if gate == "equal pinned key" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestCheckRemovalEvidence_TemplateDocsReadBeforeShortcut(t *testing.T) {
	root := t.TempDir()
	relPath := ".agent-layer/templates/docs/ROADMAP.md"
	path := filepath.Join(root, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("# missing marker\n"), 0o600))
	sys := newFaultSystem(RealSystem{})
	failure := errors.New("snapshot read denied")
	sys.readErrs[path] = failure
	inst := &installer{root: root, sys: sys}
	require.ErrorIs(t, inst.checkRemovalEvidence(relPath), failure)
}
