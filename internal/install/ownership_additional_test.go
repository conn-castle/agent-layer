package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnershipComparableError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("boom")
	err := ownershipComparableError{reasonCode: ownershipReasonPolicyPayloadInvalid, err: inner}
	if err.Error() != "boom" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, inner) {
		t.Fatal("expected wrapped error")
	}
}

func TestComparableFromManifestEntry_ErrorPaths(t *testing.T) {
	_, err := comparableFromManifestEntry(manifestFileEntry{Path: "x", FullHashNormalized: "abc", PolicyID: "unknown_policy"})
	if err == nil {
		t.Fatal("expected unknown policy error")
	}

	_, err = comparableFromManifestEntry(manifestFileEntry{Path: "x", FullHashNormalized: "abc", PolicyID: ownershipPolicyAllowlist})
	if err == nil {
		t.Fatal("expected allowlist payload error")
	}

	payload, marshalErr := json.Marshal(memoryPolicyPayload{Marker: ownershipMarkerEntriesStart, ManagedSectionHash: "hash"})
	if marshalErr != nil {
		t.Fatalf("marshal payload: %v", marshalErr)
	}
	_, err = comparableFromManifestEntry(manifestFileEntry{Path: "x", FullHashNormalized: "abc", PolicyID: ownershipPolicyMemoryRoadmap, PolicyPayload: payload})
	if err == nil {
		t.Fatal("expected marker mismatch error")
	}
}

func TestParseAllowlistPolicyPayload_ErrorPaths(t *testing.T) {
	_, err := parseAllowlistPolicyPayload(nil)
	if err == nil {
		t.Fatal("expected empty payload error")
	}

	badHashPayload, marshalErr := json.Marshal(allowlistPolicyPayload{
		UpstreamSet:     []string{"git status"},
		UpstreamSetHash: "invalid",
	})
	if marshalErr != nil {
		t.Fatalf("marshal payload: %v", marshalErr)
	}
	_, err = parseAllowlistPolicyPayload(badHashPayload)
	if err == nil {
		t.Fatal("expected hash mismatch error")
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

func TestUnknownOwnershipClassification_WithBaselineSource(t *testing.T) {
	source := BaselineStateSourceMigratedFromLegacyDocsSnapshot
	meta := baselineMetadata{source: &source}
	result := unknownOwnershipClassification(&meta, []string{ownershipReasonBaselineMissing})
	if result.Label != OwnershipUnknownNoBaseline {
		t.Fatalf("expected unknown label, got %s", result.Label)
	}
	if result.BaselineSource == nil || *result.BaselineSource != source {
		t.Fatalf("unexpected baseline source: %#v", result.BaselineSource)
	}
}

func TestUpgradeChangeAt_OutOfRange(t *testing.T) {
	changes := []upgradeChangeWithTemplate{{path: "a"}}
	if _, ok := upgradeChangeAt(changes, -1); ok {
		t.Fatal("expected out-of-range for negative index")
	}
	if _, ok := upgradeChangeAt(changes, 1); ok {
		t.Fatal("expected out-of-range for index past end")
	}
}

func TestReadCurrentPinVersion_InvalidPinReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("invalid\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	pin, err := readCurrentPinVersion(root, RealSystem{})
	if err != nil {
		t.Fatalf("readCurrentPinVersion: %v", err)
	}
	if pin != "" {
		t.Fatalf("expected empty pin, got %q", pin)
	}
}
