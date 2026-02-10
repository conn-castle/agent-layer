package install

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestOwnershipPolicyForPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{path: commandsAllowRelPath, want: ownershipPolicyAllowlist},
		{path: "docs/agent-layer/ROADMAP.md", want: ownershipPolicyMemoryRoadmap},
		{path: "docs/agent-layer/BACKLOG.md", want: ownershipPolicyMemoryEntries},
		{path: ".agent-layer/config.toml", want: ""},
	}
	for _, tc := range cases {
		if got := ownershipPolicyForPath(tc.path); got != tc.want {
			t.Fatalf("ownershipPolicyForPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestBuildOwnershipComparable_MemoryMarkerErrors(t *testing.T) {
	_, err := buildOwnershipComparable("docs/agent-layer/BACKLOG.md", []byte("# BACKLOG\n\nmissing marker\n"))
	if err == nil {
		t.Fatal("expected missing marker error")
	}
	var compErr ownershipComparableError
	if !errors.As(err, &compErr) {
		t.Fatalf("expected ownershipComparableError, got %T", err)
	}
	if compErr.reasonCode != ownershipReasonSectionMarkerMissing {
		t.Fatalf("reasonCode = %q, want %q", compErr.reasonCode, ownershipReasonSectionMarkerMissing)
	}

	ambiguous := strings.Join([]string{
		"# BACKLOG",
		"<!-- ENTRIES START -->",
		"content",
		"<!-- ENTRIES START -->",
		"",
	}, "\n")
	_, err = buildOwnershipComparable("docs/agent-layer/BACKLOG.md", []byte(ambiguous))
	if err == nil {
		t.Fatal("expected ambiguous marker error")
	}
	if !errors.As(err, &compErr) {
		t.Fatalf("expected ownershipComparableError, got %T", err)
	}
	if compErr.reasonCode != ownershipReasonSectionMarkerAmbiguous {
		t.Fatalf("reasonCode = %q, want %q", compErr.reasonCode, ownershipReasonSectionMarkerAmbiguous)
	}
}

func TestOwnershipPolicyPayload_RoundTrip(t *testing.T) {
	memoryContent := "# COMMANDS\n\nheader\n\n<!-- ENTRIES START -->\n\n- x\n"
	memoryComp, err := buildOwnershipComparable("docs/agent-layer/COMMANDS.md", []byte(memoryContent))
	if err != nil {
		t.Fatalf("build memory comparable: %v", err)
	}
	memoryPayload, err := ownershipPolicyPayload(memoryComp)
	if err != nil {
		t.Fatalf("ownershipPolicyPayload(memory): %v", err)
	}
	memoryParsed, err := parseMemoryPolicyPayload(memoryComp.PolicyID, memoryPayload)
	if err != nil {
		t.Fatalf("parseMemoryPolicyPayload: %v", err)
	}
	if memoryParsed.ManagedSectionHash != memoryComp.ManagedHash {
		t.Fatalf("managed hash mismatch: got %q want %q", memoryParsed.ManagedSectionHash, memoryComp.ManagedHash)
	}

	allowContent := "# comment\n\nbeta\na1\nbeta\n"
	allowComp, err := buildOwnershipComparable(commandsAllowRelPath, []byte(allowContent))
	if err != nil {
		t.Fatalf("build allow comparable: %v", err)
	}
	allowPayload, err := ownershipPolicyPayload(allowComp)
	if err != nil {
		t.Fatalf("ownershipPolicyPayload(allowlist): %v", err)
	}
	allowParsed, err := parseAllowlistPolicyPayload(allowPayload)
	if err != nil {
		t.Fatalf("parseAllowlistPolicyPayload: %v", err)
	}
	if allowParsed.UpstreamSetHash != allowComp.AllowHash {
		t.Fatalf("allowlist hash mismatch: got %q want %q", allowParsed.UpstreamSetHash, allowComp.AllowHash)
	}
	if len(allowParsed.UpstreamSet) != 2 {
		t.Fatalf("expected deduped set length 2, got %d", len(allowParsed.UpstreamSet))
	}
}

func TestParseMemoryPolicyPayload_ErrorPaths(t *testing.T) {
	if _, err := parseMemoryPolicyPayload(ownershipPolicyMemoryEntries, nil); err == nil {
		t.Fatal("expected empty payload error")
	}
	if _, err := parseMemoryPolicyPayload(ownershipPolicyMemoryEntries, json.RawMessage("{bad")); err == nil {
		t.Fatal("expected decode error")
	}

	wrongMarker, err := json.Marshal(memoryPolicyPayload{
		Marker:             ownershipMarkerPhasesStart,
		ManagedSectionHash: "abc",
	})
	if err != nil {
		t.Fatalf("marshal wrongMarker: %v", err)
	}
	if _, err := parseMemoryPolicyPayload(ownershipPolicyMemoryEntries, wrongMarker); err == nil {
		t.Fatal("expected marker mismatch error")
	}

	missingHash, err := json.Marshal(memoryPolicyPayload{
		Marker: ownershipMarkerEntriesStart,
	})
	if err != nil {
		t.Fatalf("marshal missingHash: %v", err)
	}
	if _, err := parseMemoryPolicyPayload(ownershipPolicyMemoryEntries, missingHash); err == nil {
		t.Fatal("expected missing managed_section_hash error")
	}
}

func TestParseAllowlistPolicyPayload_AdditionalErrorPaths(t *testing.T) {
	if _, err := parseAllowlistPolicyPayload(json.RawMessage("{bad")); err == nil {
		t.Fatal("expected decode error")
	}
	noHash, err := json.Marshal(allowlistPolicyPayload{UpstreamSet: []string{"git status"}})
	if err != nil {
		t.Fatalf("marshal noHash: %v", err)
	}
	if _, err := parseAllowlistPolicyPayload(noHash); err == nil {
		t.Fatal("expected missing hash error")
	}
	noSet, err := json.Marshal(allowlistPolicyPayload{UpstreamSetHash: "abc"})
	if err != nil {
		t.Fatalf("marshal noSet: %v", err)
	}
	if _, err := parseAllowlistPolicyPayload(noSet); err == nil {
		t.Fatal("expected missing set error")
	}

	dupPayload := allowlistPolicyPayload{
		UpstreamSet: []string{"git status", "git status"},
	}
	hashBuilder := strings.Builder{}
	hashBuilder.WriteString("git status\n")
	dupPayload.UpstreamSetHash = hashOwnershipString(hashBuilder.String())
	dupRaw, err := json.Marshal(dupPayload)
	if err != nil {
		t.Fatalf("marshal dup payload: %v", err)
	}
	if _, err := parseAllowlistPolicyPayload(dupRaw); err == nil {
		t.Fatal("expected duplicate set error")
	}
}

func TestComparableFromManifestEntry_MissingFullHash(t *testing.T) {
	_, err := comparableFromManifestEntry(manifestFileEntry{Path: ".agent-layer/config.toml", FullHashNormalized: ""})
	if err == nil {
		t.Fatal("expected missing full hash error")
	}
}

func TestValidatePolicyPayload_PassThrough(t *testing.T) {
	comp := ownershipComparable{
		PolicyID:  ownershipPolicyAllowlist,
		AllowSet:  []string{"git status"},
		AllowHash: hashOwnershipString("git status\n"),
	}
	raw, err := ownershipPolicyPayload(comp)
	if err != nil {
		t.Fatalf("ownershipPolicyPayload: %v", err)
	}
	if _, err := validatePolicyPayload(ownershipPolicyAllowlist, raw); err != nil {
		t.Fatalf("validatePolicyPayload: %v", err)
	}
}
