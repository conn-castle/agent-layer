package install

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ownershipPolicyMemoryEntries = "memory_entries_v1"
	ownershipPolicyMemoryRoadmap = "memory_roadmap_v1"
	ownershipPolicyAllowlist     = "allowlist_lines_v1"

	ownershipMarkerEntriesStart = "<!-- ENTRIES START -->"
	ownershipMarkerPhasesStart  = "<!-- PHASES START -->"
)

const (
	ownershipReasonBaselineMissing             = "baseline_missing"
	ownershipReasonPinManifestMissing          = "pin_manifest_missing"
	ownershipReasonManagedSectionMatchesPinned = "managed_section_matches_pinned"
	ownershipReasonManagedSectionMatchesOther  = "managed_section_matches_other_version"
	ownershipReasonSectionMarkerMissing        = "section_marker_missing"
	ownershipReasonSectionMarkerAmbiguous      = "section_marker_ambiguous"
	ownershipReasonAllowlistReorderedOnly      = "allowlist_reordered_only"
	ownershipReasonAllowlistUpstreamLineDelta  = "allowlist_upstream_line_delta"
	ownershipReasonAllowlistLocalLineDelta     = "allowlist_local_line_delta"
	ownershipReasonPolicyPayloadInvalid        = "policy_payload_invalid"
	ownershipReasonPolicyMismatch              = "policy_mismatch"
)

var memoryEntriesPaths = map[string]struct{}{
	"docs/agent-layer/ISSUES.md":    {},
	"docs/agent-layer/BACKLOG.md":   {},
	"docs/agent-layer/DECISIONS.md": {},
	"docs/agent-layer/COMMANDS.md":  {},
}

type ownershipComparable struct {
	PolicyID    string
	FullHash    string
	ManagedHash string
	AllowSet    []string
	AllowHash   string
}

type memoryPolicyPayload struct {
	Marker             string `json:"marker"`
	ManagedSectionHash string `json:"managed_section_hash"`
}

type allowlistPolicyPayload struct {
	UpstreamSet     []string `json:"upstream_set"`
	UpstreamSetHash string   `json:"upstream_set_hash"`
}

type ownershipComparableError struct {
	reasonCode string
	err        error
}

func (e ownershipComparableError) Error() string {
	return e.err.Error()
}

func (e ownershipComparableError) Unwrap() error {
	return e.err
}

func ownershipPolicyForPath(relPath string) string {
	if relPath == ".agent-layer/commands.allow" {
		return ownershipPolicyAllowlist
	}
	if relPath == "docs/agent-layer/ROADMAP.md" {
		return ownershipPolicyMemoryRoadmap
	}
	if _, ok := memoryEntriesPaths[relPath]; ok {
		return ownershipPolicyMemoryEntries
	}
	return ""
}

func buildOwnershipComparable(relPath string, content []byte) (ownershipComparable, error) {
	policyID := ownershipPolicyForPath(relPath)
	normalizedContent := normalizeTemplateContent(string(content))
	out := ownershipComparable{
		PolicyID: policyID,
		FullHash: hashOwnershipString(normalizedContent),
	}

	switch policyID {
	case ownershipPolicyMemoryEntries:
		hash, reasonCode, err := hashManagedMarkerSection(string(content), ownershipMarkerEntriesStart)
		if err != nil {
			return ownershipComparable{}, ownershipComparableError{reasonCode: reasonCode, err: err}
		}
		out.ManagedHash = hash
	case ownershipPolicyMemoryRoadmap:
		hash, reasonCode, err := hashManagedMarkerSection(string(content), ownershipMarkerPhasesStart)
		if err != nil {
			return ownershipComparable{}, ownershipComparableError{reasonCode: reasonCode, err: err}
		}
		out.ManagedHash = hash
	case ownershipPolicyAllowlist:
		set, setHash := parseAllowlistSet(normalizedContent)
		out.AllowSet = set
		out.AllowHash = setHash
	}

	return out, nil
}

func ownershipPolicyPayload(comp ownershipComparable) (json.RawMessage, error) {
	switch comp.PolicyID {
	case ownershipPolicyMemoryEntries:
		payload := memoryPolicyPayload{
			Marker:             ownershipMarkerEntriesStart,
			ManagedSectionHash: comp.ManagedHash,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return data, nil
	case ownershipPolicyMemoryRoadmap:
		payload := memoryPolicyPayload{
			Marker:             ownershipMarkerPhasesStart,
			ManagedSectionHash: comp.ManagedHash,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return data, nil
	case ownershipPolicyAllowlist:
		payload := allowlistPolicyPayload{
			UpstreamSet:     comp.AllowSet,
			UpstreamSetHash: comp.AllowHash,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		return data, nil
	default:
		return nil, nil
	}
}

func comparableKey(comp ownershipComparable) string {
	switch comp.PolicyID {
	case ownershipPolicyMemoryEntries, ownershipPolicyMemoryRoadmap:
		return comp.ManagedHash
	case ownershipPolicyAllowlist:
		return comp.AllowHash
	default:
		return comp.FullHash
	}
}

func comparableFromManifestEntry(entry manifestFileEntry) (ownershipComparable, error) {
	comp := ownershipComparable{
		PolicyID: entry.PolicyID,
		FullHash: entry.FullHashNormalized,
	}
	if strings.TrimSpace(comp.FullHash) == "" {
		return ownershipComparable{}, fmt.Errorf("manifest entry %s missing full hash", entry.Path)
	}
	switch entry.PolicyID {
	case "":
		return comp, nil
	case ownershipPolicyMemoryEntries, ownershipPolicyMemoryRoadmap:
		payload, err := parseMemoryPolicyPayload(entry.PolicyID, entry.PolicyPayload)
		if err != nil {
			return ownershipComparable{}, err
		}
		comp.ManagedHash = payload.ManagedSectionHash
	case ownershipPolicyAllowlist:
		payload, err := parseAllowlistPolicyPayload(entry.PolicyPayload)
		if err != nil {
			return ownershipComparable{}, err
		}
		comp.AllowHash = payload.UpstreamSetHash
		comp.AllowSet = append([]string(nil), payload.UpstreamSet...)
	default:
		return ownershipComparable{}, fmt.Errorf("unknown ownership policy_id %q", entry.PolicyID)
	}
	return comp, nil
}

func validatePolicyPayload(policyID string, payload json.RawMessage) (ownershipComparable, error) {
	entry := manifestFileEntry{
		Path:               "<validation>",
		PolicyID:           policyID,
		PolicyPayload:      payload,
		FullHashNormalized: strings.Repeat("0", 64),
	}
	return comparableFromManifestEntry(entry)
}

func parseMemoryPolicyPayload(policyID string, payload json.RawMessage) (memoryPolicyPayload, error) {
	if len(payload) == 0 {
		return memoryPolicyPayload{}, fmt.Errorf("policy %s requires payload", policyID)
	}
	var parsed memoryPolicyPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return memoryPolicyPayload{}, fmt.Errorf("decode memory payload: %w", err)
	}
	expectedMarker := ownershipMarkerEntriesStart
	if policyID == ownershipPolicyMemoryRoadmap {
		expectedMarker = ownershipMarkerPhasesStart
	}
	if parsed.Marker != expectedMarker {
		return memoryPolicyPayload{}, fmt.Errorf("memory payload marker %q does not match expected %q", parsed.Marker, expectedMarker)
	}
	if strings.TrimSpace(parsed.ManagedSectionHash) == "" {
		return memoryPolicyPayload{}, fmt.Errorf("memory payload managed_section_hash is required")
	}
	return parsed, nil
}

func parseAllowlistPolicyPayload(payload json.RawMessage) (allowlistPolicyPayload, error) {
	if len(payload) == 0 {
		return allowlistPolicyPayload{}, fmt.Errorf("allowlist policy requires payload")
	}
	var parsed allowlistPolicyPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return allowlistPolicyPayload{}, fmt.Errorf("decode allowlist payload: %w", err)
	}
	if strings.TrimSpace(parsed.UpstreamSetHash) == "" {
		return allowlistPolicyPayload{}, fmt.Errorf("allowlist payload upstream_set_hash is required")
	}
	if len(parsed.UpstreamSet) == 0 {
		return allowlistPolicyPayload{}, fmt.Errorf("allowlist payload upstream_set is required")
	}
	copiedSet := append([]string(nil), parsed.UpstreamSet...)
	sort.Strings(copiedSet)
	for idx := 1; idx < len(copiedSet); idx++ {
		if copiedSet[idx] == copiedSet[idx-1] {
			return allowlistPolicyPayload{}, fmt.Errorf("allowlist payload upstream_set contains duplicate %q", copiedSet[idx])
		}
	}
	canonicalBuilder := strings.Builder{}
	for _, line := range copiedSet {
		canonicalBuilder.WriteString(line)
		canonicalBuilder.WriteString("\n")
	}
	if hashOwnershipString(canonicalBuilder.String()) != parsed.UpstreamSetHash {
		return allowlistPolicyPayload{}, fmt.Errorf("allowlist payload upstream_set_hash does not match upstream_set")
	}
	parsed.UpstreamSet = copiedSet
	return parsed, nil
}

func hashManagedMarkerSection(content string, marker string) (string, string, error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	lines := strings.Split(normalized, "\n")
	markerLineIndex := -1
	for idx, line := range lines {
		if strings.TrimSpace(line) != marker {
			continue
		}
		if markerLineIndex >= 0 {
			return "", ownershipReasonSectionMarkerAmbiguous, fmt.Errorf("ownership marker %q must appear exactly once as a standalone line", marker)
		}
		markerLineIndex = idx
	}
	if markerLineIndex < 0 {
		return "", ownershipReasonSectionMarkerMissing, fmt.Errorf("ownership marker %q missing", marker)
	}
	builder := strings.Builder{}
	for idx := 0; idx <= markerLineIndex; idx++ {
		builder.WriteString(lines[idx])
		builder.WriteString("\n")
	}
	managed := normalizeTemplateContent(builder.String())
	return hashOwnershipString(managed), "", nil
}

func parseAllowlistSet(normalizedContent string) ([]string, string) {
	lines := strings.Split(normalizedContent, "\n")
	seen := make(map[string]struct{})
	set := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		set = append(set, trimmed)
	}
	sort.Strings(set)
	builder := strings.Builder{}
	for _, line := range set {
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return set, hashOwnershipString(builder.String())
}

func hashOwnershipString(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}
