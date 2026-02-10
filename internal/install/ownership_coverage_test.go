package install

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestClassifyOwnership_PropagatesReadError(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	localPath := filepath.Join(root, filepath.FromSlash(commandsAllowRelPath))
	sys.readErrs[normalizePath(localPath)] = errors.New("read boom")

	inst := &installer{root: root, sys: sys}
	if _, err := inst.classifyOwnership(commandsAllowRelPath, "commands.allow"); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestClassifyOwnershipDetail_TemplateReadError(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, filepath.FromSlash(commandsAllowRelPath))
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir local dir: %v", err)
	}
	if err := os.WriteFile(localPath, []byte("git status\n"), 0o644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			return nil, errors.New("template boom")
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.classifyOwnershipDetail(commandsAllowRelPath, "commands.allow"); err == nil || !strings.Contains(err.Error(), "template boom") {
		t.Fatalf("expected template read error, got %v", err)
	}
}

func TestClassifyOrphanOwnership_PropagatesReadError(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	relPath := "docs/agent-layer/ISSUES.md"
	localPath := filepath.Join(root, filepath.FromSlash(relPath))
	sys.readErrs[normalizePath(localPath)] = errors.New("read boom")

	inst := &installer{root: root, sys: sys}
	if _, err := inst.classifyOrphanOwnership(relPath); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected orphan read error, got %v", err)
	}
}

func TestClassifyAgainstBaseline_TargetParseError(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	relPath := "docs/agent-layer/ISSUES.md"
	localBytes := []byte("# ISSUES\n\n<!-- ENTRIES START -->\n")
	targetBytes := []byte("# ISSUES\n\n(no marker)\n")

	if _, err := inst.classifyAgainstBaseline(relPath, localBytes, targetBytes, false); err == nil || !strings.Contains(err.Error(), "parse target comparable") {
		t.Fatalf("expected parse target comparable error, got %v", err)
	}
}

func TestClassifyAgainstBaseline_PropagatesBaselineError(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir baseline dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if _, err := inst.classifyAgainstBaseline(commandsAllowRelPath, []byte("git status\n"), []byte("git status\n"), false); err == nil {
		t.Fatal("expected baseline decode error")
	}
}

func TestResolveBaselineComparable_PinManifestDecodeError(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == path.Join(templateManifestDir, "0.0.1.json") {
			return []byte("{bad-json"), nil
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	localComp, err := buildOwnershipComparable(commandsAllowRelPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}

	if _, _, err := inst.resolveBaselineComparable(commandsAllowRelPath, localComp); err == nil || !strings.Contains(err.Error(), "decode template manifest") {
		t.Fatalf("expected manifest decode error, got %v", err)
	}
}

func TestResolveBaselineComparable_PinManifestPolicyMismatchAddsReason(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}

	payload, err := json.Marshal(memoryPolicyPayload{Marker: ownershipMarkerEntriesStart, ManagedSectionHash: "abc"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	manifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.0.1",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: strings.Repeat("a", 64),
			PolicyID:           ownershipPolicyMemoryEntries,
			PolicyPayload:      payload,
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == path.Join(templateManifestDir, "0.0.1.json") {
			return manifestBytes, nil
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	localComp, err := buildOwnershipComparable(commandsAllowRelPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}

	_, meta, err := inst.resolveBaselineComparable(commandsAllowRelPath, localComp)
	if err != nil {
		t.Fatalf("resolveBaselineComparable: %v", err)
	}
	if meta.available {
		t.Fatalf("expected baseline metadata to be unavailable, got %#v", meta)
	}
	foundMismatch := false
	for _, reason := range meta.reasons {
		if reason == ownershipReasonPolicyMismatch {
			foundMismatch = true
			break
		}
	}
	if !foundMismatch {
		t.Fatalf("expected policy mismatch in reasons, got %#v", meta.reasons)
	}
}

func TestResolveBaselineComparable_MatchAnyOtherManifestAddsReason(t *testing.T) {
	resetManifestCacheForTest()
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		resetManifestCacheForTest()
	})

	localBytes := []byte("git status\n")
	pinnedBytes := []byte("git diff\n")
	localComp, err := buildOwnershipComparable(commandsAllowRelPath, localBytes)
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}
	pinnedComp, err := buildOwnershipComparable(commandsAllowRelPath, pinnedBytes)
	if err != nil {
		t.Fatalf("build pinned comparable: %v", err)
	}

	localPayload, err := ownershipPolicyPayload(localComp)
	if err != nil {
		t.Fatalf("build local payload: %v", err)
	}
	pinnedPayload, err := ownershipPolicyPayload(pinnedComp)
	if err != nil {
		t.Fatalf("build pinned payload: %v", err)
	}

	pinnedManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.1.0",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: pinnedComp.FullHash,
			PolicyID:           pinnedComp.PolicyID,
			PolicyPayload:      pinnedPayload,
		}},
	}
	otherManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.2.0",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: localComp.FullHash,
			PolicyID:           localComp.PolicyID,
			PolicyPayload:      localPayload,
		}},
	}
	pinnedBytesJSON, err := json.Marshal(pinnedManifest)
	if err != nil {
		t.Fatalf("marshal pinned manifest: %v", err)
	}
	otherBytesJSON, err := json.Marshal(otherManifest)
	if err != nil {
		t.Fatalf("marshal other manifest: %v", err)
	}

	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		if root != templateManifestDir {
			return originalWalk(root, fn)
		}
		if err := fn(root, staticDirEntry{name: root, dir: true}, nil); err != nil {
			return err
		}
		for _, p := range []string{
			path.Join(root, "0.1.0.json"),
			path.Join(root, "0.2.0.json"),
		} {
			if err := fn(p, staticDirEntry{name: path.Base(p), dir: false}, nil); err != nil {
				return err
			}
		}
		return nil
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		switch name {
		case path.Join(templateManifestDir, "0.1.0.json"):
			return pinnedBytesJSON, nil
		case path.Join(templateManifestDir, "0.2.0.json"):
			return otherBytesJSON, nil
		default:
			return originalRead(name)
		}
	}

	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	_, meta, err := inst.resolveBaselineComparable(commandsAllowRelPath, localComp)
	if err != nil {
		t.Fatalf("resolveBaselineComparable: %v", err)
	}
	found := false
	for _, reason := range meta.reasons {
		if reason == ownershipReasonManagedSectionMatchesOther {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected managed_section_matches_other_version reason, got %#v", meta.reasons)
	}
}

func TestResolveBaselineComparable_MatchAnyOtherManifestErrorPropagates(t *testing.T) {
	resetManifestCacheForTest()
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		resetManifestCacheForTest()
	})

	localComp, err := buildOwnershipComparable(commandsAllowRelPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}
	pinnedComp, err := buildOwnershipComparable(commandsAllowRelPath, []byte("git diff\n"))
	if err != nil {
		t.Fatalf("build pinned comparable: %v", err)
	}
	pinnedPayload, err := ownershipPolicyPayload(pinnedComp)
	if err != nil {
		t.Fatalf("build pinned payload: %v", err)
	}
	pinnedManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.1.0",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: pinnedComp.FullHash,
			PolicyID:           pinnedComp.PolicyID,
			PolicyPayload:      pinnedPayload,
		}},
	}
	pinnedBytesJSON, err := json.Marshal(pinnedManifest)
	if err != nil {
		t.Fatalf("marshal pinned manifest: %v", err)
	}

	templates.WalkFunc = func(string, fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == path.Join(templateManifestDir, "0.1.0.json") {
			return pinnedBytesJSON, nil
		}
		return originalRead(name)
	}

	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if _, _, err := inst.resolveBaselineComparable(commandsAllowRelPath, localComp); err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected walk error, got %v", err)
	}
}

func TestMatchAnyOtherManifest_PathMissingInManifestsReturnsFalse(t *testing.T) {
	ok, err := matchAnyOtherManifest("does/not/exist", "0.6.0", "key")
	if err != nil {
		t.Fatalf("matchAnyOtherManifest: %v", err)
	}
	if ok {
		t.Fatal("expected no match for missing path")
	}
}

func TestAppendUniqueAndSortAndDedupeReasons_EdgeCases(t *testing.T) {
	got := appendUnique([]string{"", "a"}, []string{"", "a", "b", "b", "  "})
	if strings.Join(got, ",") != ",a,b" {
		t.Fatalf("appendUnique result = %#v", got)
	}

	reasons := sortAndDedupeReasons([]string{"", "b", " a ", "a", "b"})
	if strings.Join(reasons, ",") != "a,b" {
		t.Fatalf("sortAndDedupeReasons result = %#v", reasons)
	}
}
