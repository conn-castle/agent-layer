package install

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOwnershipLabelDisplay_EmptyDefaultsToLocalCustomization(t *testing.T) {
	if got := OwnershipLabel("").Display(); got != string(OwnershipLocalCustomization) {
		t.Fatalf("Display() = %q, want %q", got, string(OwnershipLocalCustomization))
	}
}

func TestOwnershipLabelState_Mappings(t *testing.T) {
	cases := []struct {
		label OwnershipLabel
		want  OwnershipState
	}{
		{label: OwnershipUpstreamTemplateDelta, want: OwnershipStateUpstreamTemplateDelta},
		{label: OwnershipLocalCustomization, want: OwnershipStateLocalCustomization},
		{label: OwnershipMixedUpstreamAndLocal, want: OwnershipStateMixedUpstreamAndLocal},
		{label: OwnershipUnknownNoBaseline, want: OwnershipStateUnknownNoBaseline},
		{label: OwnershipLabel(""), want: OwnershipStateLocalCustomization},
		{label: OwnershipLabel("custom unknown"), want: OwnershipStateLocalCustomization},
	}

	for _, tc := range cases {
		if got := tc.label.State(); got != tc.want {
			t.Fatalf("State(%q) = %q, want %q", tc.label, got, tc.want)
		}
	}
}

func TestShouldOverwriteAllManaged_FormatsOwnershipLabels(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom allow\n"), 0o644); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	var promptPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllFunc: func(paths []string) (bool, error) {
				promptPaths = append(promptPaths, paths...)
				return false, nil
			},
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return false, nil },
			OverwriteFunc:          func(string) (bool, error) { return false, nil },
		},
	}

	if _, err := inst.shouldOverwriteAllManaged(); err != nil {
		t.Fatalf("shouldOverwriteAllManaged: %v", err)
	}
	if len(promptPaths) == 0 {
		t.Fatalf("expected prompt paths")
	}
	if !strings.Contains(promptPaths[0], "unknown no baseline") {
		t.Fatalf("expected ownership label in prompt path, got %q", promptPaths[0])
	}
}

func TestShouldOverwriteAllMemory_FormatsOwnershipLabels(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o755); err != nil {
		t.Fatalf("mkdir baseline docs: %v", err)
	}
	content := []byte("# ISSUES\n\nLegacy header\n\n<!-- ENTRIES START -->\n")
	docPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
	if err := os.WriteFile(docPath, content, 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if err := os.WriteFile(baselinePath, content, 0o644); err != nil {
		t.Fatalf("write baseline doc: %v", err)
	}

	var promptPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllFunc:       func([]string) (bool, error) { return false, nil },
			OverwriteAllMemoryFunc: func(paths []string) (bool, error) { promptPaths = append(promptPaths, paths...); return false, nil },
			OverwriteFunc:          func(string) (bool, error) { return false, nil },
		},
	}

	if _, err := inst.shouldOverwriteAllMemory(); err != nil {
		t.Fatalf("shouldOverwriteAllMemory: %v", err)
	}
	if len(promptPaths) == 0 {
		t.Fatalf("expected prompt paths")
	}
	if !strings.Contains(promptPaths[0], "upstream template delta") {
		t.Fatalf("expected ownership label in prompt path, got %q", promptPaths[0])
	}
}

func TestClassifyOrphanOwnership_DocsAgentLayer_UsesBaseline(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
		t.Fatalf("mkdir baseline: %v", err)
	}
	content := []byte("# ROADMAP\n\nLegacy header\n\n<!-- PHASES START -->\n")
	if err := os.WriteFile(localPath, content, 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}
	if err := os.WriteFile(baselinePath, content, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership("docs/agent-layer/ROADMAP.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership: %v", err)
	}
	if ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("ownership = %s, want %s", ownership, OwnershipUpstreamTemplateDelta)
	}
}

func TestClassifyOrphanOwnership_TemplatesDocs_AlwaysUpstream(t *testing.T) {
	root := t.TempDir()
	orphanPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan template snapshot\n"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership(".agent-layer/templates/docs/ROADMAP.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership: %v", err)
	}
	if ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("ownership = %s, want %s", ownership, OwnershipUpstreamTemplateDelta)
	}
}

func TestClassifyOrphanOwnership_DocsMissingBaselineAndDefaultFallback(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(docsPath, []byte("# ISSUES\n\nLocal header\n\n<!-- ENTRIES START -->\n"), 0o644); err != nil {
		t.Fatalf("write docs orphan: %v", err)
	}
	defaultPath := filepath.Join(root, ".agent-layer", "slash-commands", "local-orphan.md")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		t.Fatalf("mkdir default orphan dir: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte("local orphan\n"), 0o644); err != nil {
		t.Fatalf("write default orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership docs: %v", err)
	}
	if ownership != OwnershipUnknownNoBaseline {
		t.Fatalf("docs missing baseline ownership = %s, want %s", ownership, OwnershipUnknownNoBaseline)
	}

	ownership, err = inst.classifyOrphanOwnership(".agent-layer/slash-commands/local-orphan.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership default: %v", err)
	}
	if ownership != OwnershipUnknownNoBaseline {
		t.Fatalf("default fallback ownership = %s, want %s", ownership, OwnershipUnknownNoBaseline)
	}
}

func TestClassifyAgainstBaseline_FromCanonicalMixedAndReordered(t *testing.T) {
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
	mixed, err := inst.classifyAgainstBaseline(
		relPath,
		[]byte("git status\ngit diff\ngit rev-parse --show-toplevel\n"),
		[]byte("git status\ngit diff\ngit log --oneline\n"),
		false,
	)
	if err != nil {
		t.Fatalf("classify mixed: %v", err)
	}
	if mixed.Label != OwnershipMixedUpstreamAndLocal {
		t.Fatalf("mixed label = %s, want %s", mixed.Label, OwnershipMixedUpstreamAndLocal)
	}
	if mixed.Confidence == nil || *mixed.Confidence != OwnershipConfidenceHigh {
		t.Fatalf("mixed confidence = %#v, want high", mixed.Confidence)
	}
	if mixed.BaselineSource == nil || *mixed.BaselineSource != BaselineStateSourceWrittenByInit {
		t.Fatalf("mixed baseline source = %#v, want written_by_init", mixed.BaselineSource)
	}
	if !slices.Contains(mixed.ReasonCodes, ownershipReasonAllowlistUpstreamLineDelta) {
		t.Fatalf("missing upstream delta reason in %#v", mixed.ReasonCodes)
	}
	if !slices.Contains(mixed.ReasonCodes, ownershipReasonAllowlistLocalLineDelta) {
		t.Fatalf("missing local delta reason in %#v", mixed.ReasonCodes)
	}

	reordered, err := inst.classifyAgainstBaseline(
		relPath,
		[]byte("git diff\ngit status\n"),
		[]byte("git status\ngit diff\n"),
		false,
	)
	if err != nil {
		t.Fatalf("classify reordered: %v", err)
	}
	if reordered.Label != OwnershipLocalCustomization {
		t.Fatalf("reordered label = %s, want %s", reordered.Label, OwnershipLocalCustomization)
	}
	if !slices.Contains(reordered.ReasonCodes, ownershipReasonAllowlistReorderedOnly) {
		t.Fatalf("missing reorder reason in %#v", reordered.ReasonCodes)
	}
}

func TestClassifyAgainstBaseline_PolicyMismatchReturnsUnknown(t *testing.T) {
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
	result, err := inst.classifyAgainstBaseline(relPath, []byte("git status\n"), []byte("git status\n"), false)
	if err != nil {
		t.Fatalf("classifyAgainstBaseline: %v", err)
	}
	if result.Label != OwnershipUnknownNoBaseline {
		t.Fatalf("label = %s, want %s", result.Label, OwnershipUnknownNoBaseline)
	}
	if !slices.Contains(result.ReasonCodes, ownershipReasonPolicyMismatch) {
		t.Fatalf("expected policy mismatch reason, got %#v", result.ReasonCodes)
	}
	if result.BaselineSource == nil || *result.BaselineSource != BaselineStateSourceWrittenByInit {
		t.Fatalf("baseline source = %#v, want written_by_init", result.BaselineSource)
	}
}

func TestResolveBaselineComparable_PinManifestAndLegacyPaths(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	localComp, err := buildOwnershipComparable(relPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("9.9.9\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	_, meta, err := inst.resolveBaselineComparable(relPath, localComp)
	if err != nil {
		t.Fatalf("resolveBaselineComparable missing manifest: %v", err)
	}
	if meta.available {
		t.Fatalf("expected unavailable baseline metadata")
	}
	if !slices.Contains(meta.reasons, ownershipReasonPinManifestMissing) {
		t.Fatalf("expected pin_manifest_missing reason, got %#v", meta.reasons)
	}

	docsRelPath := "docs/agent-layer/BACKLOG.md"
	legacyPath := filepath.Join(root, ".agent-layer", "templates", "docs", "BACKLOG.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("# BACKLOG\n\nmissing marker\n"), 0o644); err != nil {
		t.Fatalf("write legacy docs: %v", err)
	}
	_, _, err = inst.readLegacyDocsBaselineComparable(docsRelPath)
	if err == nil {
		t.Fatal("expected parse error for malformed legacy docs snapshot")
	}
}

func TestResolveBaselineComparable_ReadErrors(t *testing.T) {
	root := t.TempDir()
	relPath := commandsAllowRelPath
	localComp, err := buildOwnershipComparable(relPath, []byte("git status\n"))
	if err != nil {
		t.Fatalf("build local comparable: %v", err)
	}

	// Corrupted canonical baseline should fail loudly.
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("write corrupted baseline: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if _, _, err := inst.resolveBaselineComparable(relPath, localComp); err == nil {
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
	if _, _, err := inst.resolveBaselineComparable(relPath, localComp); err == nil {
		t.Fatal("expected pin read error")
	}
}
