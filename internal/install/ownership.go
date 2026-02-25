package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	// OwnershipUpstreamTemplateDelta indicates a file differs because upstream template content changed.
	OwnershipUpstreamTemplateDelta OwnershipLabel = "upstream template delta"
	// OwnershipLocalCustomization indicates a file differs because local edits/customization are present.
	OwnershipLocalCustomization OwnershipLabel = "local customization"
	// OwnershipMixedUpstreamAndLocal indicates both upstream template changes and local customization are present.
	OwnershipMixedUpstreamAndLocal OwnershipLabel = "mixed upstream and local"
	// OwnershipUnknownNoBaseline indicates ownership cannot be classified due to missing baseline evidence.
	OwnershipUnknownNoBaseline OwnershipLabel = "unknown no baseline"
)

// OwnershipLabel classifies why a managed file differs from the embedded template.
type OwnershipLabel string

// OwnershipState is the machine-readable ownership enum for upgrade-plan JSON.
type OwnershipState string

const (
	// OwnershipStateUpstreamTemplateDelta is used when a change is attributable to upstream template changes.
	OwnershipStateUpstreamTemplateDelta OwnershipState = "upstream_template_delta"
	// OwnershipStateLocalCustomization is used when a change is attributable to local customization.
	OwnershipStateLocalCustomization OwnershipState = "local_customization"
	// OwnershipStateMixedUpstreamAndLocal is used when both upstream and local changes are present.
	OwnershipStateMixedUpstreamAndLocal OwnershipState = "mixed_upstream_and_local"
	// OwnershipStateUnknownNoBaseline is used when ownership cannot be determined due to missing baseline.
	OwnershipStateUnknownNoBaseline OwnershipState = "unknown_no_baseline"
)

// OwnershipConfidence is the certainty level for ownership classification evidence.
type OwnershipConfidence string

const (
	// OwnershipConfidenceHigh indicates canonical baseline evidence.
	OwnershipConfidenceHigh OwnershipConfidence = "high"
	// OwnershipConfidenceMedium indicates inferred baseline evidence (for example pinned manifest).
	OwnershipConfidenceMedium OwnershipConfidence = "medium"
	// OwnershipConfidenceLow indicates legacy snapshot migration bridge evidence.
	OwnershipConfidenceLow OwnershipConfidence = "low"
)

// LabeledPath is a path paired with an ownership label.
type LabeledPath struct {
	Path      string
	Ownership OwnershipLabel
}

type ownershipClassification struct {
	Label          OwnershipLabel
	State          OwnershipState
	Confidence     *OwnershipConfidence
	BaselineSource *BaselineStateSource
	ReasonCodes    []string
}

// Display returns a stable user-facing ownership label string.
func (o OwnershipLabel) Display() string {
	trimmed := strings.TrimSpace(string(o))
	if trimmed == "" {
		return string(OwnershipLocalCustomization)
	}
	return trimmed
}

// State returns a stable machine-readable ownership enum for JSON output.
func (o OwnershipLabel) State() OwnershipState {
	switch strings.TrimSpace(string(o)) {
	case string(OwnershipUpstreamTemplateDelta):
		return OwnershipStateUpstreamTemplateDelta
	case string(OwnershipMixedUpstreamAndLocal):
		return OwnershipStateMixedUpstreamAndLocal
	case string(OwnershipUnknownNoBaseline):
		return OwnershipStateUnknownNoBaseline
	case string(OwnershipLocalCustomization), "":
		return OwnershipStateLocalCustomization
	default:
		// Unknown labels are treated conservatively as local customization in v1.
		return OwnershipStateLocalCustomization
	}
}

// classifyOwnership classifies a template diff ownership label.
func (inst ownershipClassifier) classifyOwnership(relPath string, templatePath string) (OwnershipLabel, error) {
	result, err := inst.classifyOwnershipDetail(relPath, templatePath)
	if err != nil {
		return "", err
	}
	return result.Label, nil
}

// classifyOwnershipDetail classifies a template diff and returns rich ownership metadata.
func (inst ownershipClassifier) classifyOwnershipDetail(relPath string, templatePath string) (ownershipClassification, error) {
	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return ownershipClassification{}, err
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return ownershipClassification{}, err
	}
	return inst.classifyAgainstBaseline(relPath, localBytes, templateBytes, false)
}

// classifyOrphanOwnership classifies template orphans and returns a user-facing label.
func (inst ownershipClassifier) classifyOrphanOwnership(relPath string) (OwnershipLabel, error) {
	result, err := inst.classifyOrphanOwnershipDetail(relPath)
	if err != nil {
		return "", err
	}
	return result.Label, nil
}

// classifyOrphanOwnershipDetail classifies template orphans and returns rich ownership metadata.
func (inst ownershipClassifier) classifyOrphanOwnershipDetail(relPath string) (ownershipClassification, error) {
	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return ownershipClassification{}, err
	}
	if strings.HasPrefix(relPath, ".agent-layer/templates/docs/") {
		return ownershipClassification{
			Label: OwnershipUpstreamTemplateDelta,
			State: OwnershipStateUpstreamTemplateDelta,
		}, nil
	}
	return inst.classifyAgainstBaseline(relPath, localBytes, nil, true)
}

func (inst ownershipClassifier) classifyAgainstBaseline(
	relPath string,
	localBytes []byte,
	targetTemplateBytes []byte,
	orphan bool,
) (ownershipClassification, error) {
	localComp, localParseReason, localCompErr := classifyComparable(relPath, localBytes)
	localComparableParsed := localCompErr == nil
	if !localComparableParsed {
		reasons := appendUnique([]string{localParseReason}, []string{ownershipReasonBaselineMissing})
		return unknownOwnershipClassification(nil, reasons), nil
	}

	var targetComp ownershipComparable
	if !orphan {
		targetComparable, targetReason, err := classifyComparable(relPath, targetTemplateBytes)
		if err != nil {
			return ownershipClassification{}, fmt.Errorf("parse target comparable for %s (%s): %w", relPath, targetReason, err)
		}
		targetComp = targetComparable
	}

	baselineComp, baselineMeta, baselineErr := inst.resolveBaselineComparable(relPath, localComp)
	if baselineErr != nil {
		return ownershipClassification{}, baselineErr
	}
	if !baselineMeta.available {
		reasons := appendUnique([]string{ownershipReasonBaselineMissing}, baselineMeta.reasons)
		return unknownOwnershipClassification(nil, reasons), nil
	}

	if baselineComp.PolicyID != localComp.PolicyID {
		reasons := appendUnique([]string{ownershipReasonPolicyMismatch}, baselineMeta.reasons)
		return unknownOwnershipClassification(&baselineMeta, reasons), nil
	}
	if !orphan && baselineComp.PolicyID != targetComp.PolicyID {
		reasons := appendUnique([]string{ownershipReasonPolicyMismatch}, baselineMeta.reasons)
		return unknownOwnershipClassification(&baselineMeta, reasons), nil
	}

	baselineKey := comparableKey(baselineComp)
	localKey := comparableKey(localComp)
	upstreamChanged := true
	if !orphan {
		upstreamChanged = baselineKey != comparableKey(targetComp)
	}
	localChanged := baselineKey != localKey

	reasons := append([]string{}, baselineMeta.reasons...)
	if baselineComp.PolicyID == ownershipPolicyAllowlist {
		if upstreamChanged {
			reasons = appendUnique(reasons, []string{ownershipReasonAllowlistUpstreamLineDelta})
		}
		if localChanged {
			reasons = appendUnique(reasons, []string{ownershipReasonAllowlistLocalLineDelta})
		}
		if !orphan && !upstreamChanged && !localChanged && localComp.FullHash != targetComp.FullHash {
			reasons = appendUnique(reasons, []string{ownershipReasonAllowlistReorderedOnly})
		}
	}

	label := OwnershipLocalCustomization
	switch {
	case upstreamChanged && localChanged:
		label = OwnershipMixedUpstreamAndLocal
	case upstreamChanged:
		label = OwnershipUpstreamTemplateDelta
	case localChanged:
		label = OwnershipLocalCustomization
	default:
		// Semantically equivalent managed sections still count as local customizations
		// when a raw file diff exists (for example user-owned memory entries).
		label = OwnershipLocalCustomization
	}

	result := ownershipClassification{
		Label:          label,
		State:          label.State(),
		Confidence:     baselineMeta.confidence,
		BaselineSource: baselineMeta.source,
		ReasonCodes:    sortAndDedupeReasons(reasons),
	}
	return result, nil
}

type baselineMetadata struct {
	available  bool
	source     *BaselineStateSource
	confidence *OwnershipConfidence
	reasons    []string
}

func (inst ownershipClassifier) resolveBaselineComparable(relPath string, localComp ownershipComparable) (ownershipComparable, baselineMetadata, error) {
	reasons := make([]string, 0)

	canonicalState, err := readManagedBaselineState(inst.root, inst.sys)
	if err == nil {
		entries := manifestFileMap(canonicalState.Files)
		if entry, ok := entries[relPath]; ok {
			comp, parseErr := comparableFromManifestEntry(entry)
			if parseErr != nil {
				return ownershipComparable{}, baselineMetadata{}, parseErr
			}
			source := BaselineStateSourceWrittenByInit
			if canonicalState.Source != "" {
				source = canonicalState.Source
			}
			confidence := OwnershipConfidenceHigh
			return comp, baselineMetadata{
				available:  true,
				source:     ptrBaselineSource(source),
				confidence: ptrOwnershipConfidence(confidence),
				reasons:    []string{},
			}, nil
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ownershipComparable{}, baselineMetadata{}, err
	}

	pinVersion, pinErr := readCurrentPinVersion(inst.root, inst.sys)
	if pinErr != nil {
		return ownershipComparable{}, baselineMetadata{}, pinErr
	}
	if pinVersion != "" {
		manifest, manifestErr := loadTemplateManifestByVersion(pinVersion)
		switch {
		case manifestErr == nil:
			entries := manifestFileMap(manifest.Files)
			if entry, ok := entries[relPath]; ok {
				comp, parseErr := comparableFromManifestEntry(entry)
				if parseErr != nil {
					return ownershipComparable{}, baselineMetadata{}, parseErr
				}
				switch {
				case comp.PolicyID != localComp.PolicyID:
					reasons = appendUnique(reasons, []string{ownershipReasonPolicyMismatch})
				case comparableKey(comp) == comparableKey(localComp):
					source := BaselineStateSourceInferredFromPinManifest
					confidence := OwnershipConfidenceMedium
					return comp, baselineMetadata{
						available:  true,
						source:     ptrBaselineSource(source),
						confidence: ptrOwnershipConfidence(confidence),
						reasons:    appendUnique(reasons, []string{ownershipReasonManagedSectionMatchesPinned}),
					}, nil
				default:
					matchOther, checkErr := matchAnyOtherManifest(relPath, pinVersion, comparableKey(localComp))
					if checkErr != nil {
						return ownershipComparable{}, baselineMetadata{}, checkErr
					}
					if matchOther {
						reasons = appendUnique(reasons, []string{ownershipReasonManagedSectionMatchesOther})
					}
				}
			}
		case !errors.Is(manifestErr, os.ErrNotExist):
			return ownershipComparable{}, baselineMetadata{}, manifestErr
		default:
			reasons = appendUnique(reasons, []string{ownershipReasonPinManifestMissing})
		}
	}

	legacyComp, legacyAvailable, legacyErr := inst.readLegacyDocsBaselineComparable(relPath)
	if legacyErr != nil {
		return ownershipComparable{}, baselineMetadata{}, legacyErr
	}
	if legacyAvailable {
		source := BaselineStateSourceMigratedFromLegacyDocsSnapshot
		confidence := OwnershipConfidenceLow
		return legacyComp, baselineMetadata{
			available:  true,
			source:     ptrBaselineSource(source),
			confidence: ptrOwnershipConfidence(confidence),
			reasons:    reasons,
		}, nil
	}

	if len(reasons) == 0 {
		reasons = []string{ownershipReasonBaselineMissing}
	}
	return ownershipComparable{}, baselineMetadata{reasons: reasons}, nil
}

func (inst ownershipClassifier) readLegacyDocsBaselineComparable(relPath string) (ownershipComparable, bool, error) {
	if !strings.HasPrefix(relPath, "docs/agent-layer/") {
		return ownershipComparable{}, false, nil
	}
	suffix := strings.TrimPrefix(relPath, "docs/agent-layer/")
	legacyPath := filepath.Join(inst.root, ".agent-layer", "templates", "docs", filepath.FromSlash(suffix))
	legacyBytes, err := inst.sys.ReadFile(legacyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ownershipComparable{}, false, nil
		}
		return ownershipComparable{}, false, err
	}
	comp, reasonCode, compErr := classifyComparable(relPath, legacyBytes)
	if compErr != nil {
		if reasonCode == "" {
			reasonCode = ownershipReasonPolicyPayloadInvalid
		}
		return ownershipComparable{}, false, fmt.Errorf("parse legacy baseline comparable for %s (%s): %w", relPath, reasonCode, compErr)
	}
	return comp, true, nil
}

func matchAnyOtherManifest(relPath string, pinnedVersion string, key string) (bool, error) {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return false, err
	}
	for versionValue, manifest := range manifests {
		if versionValue == pinnedVersion {
			continue
		}
		entries := manifestFileMap(manifest.Files)
		entry, ok := entries[relPath]
		if !ok {
			continue
		}
		comp, compErr := comparableFromManifestEntry(entry)
		if compErr != nil {
			return false, compErr
		}
		if comparableKey(comp) == key {
			return true, nil
		}
	}
	return false, nil
}

func classifyComparable(relPath string, content []byte) (ownershipComparable, string, error) {
	comp, err := buildOwnershipComparable(relPath, content)
	if err == nil {
		return comp, "", nil
	}
	var compErr ownershipComparableError
	if errors.As(err, &compErr) {
		return ownershipComparable{}, compErr.reasonCode, compErr
	}
	return ownershipComparable{}, ownershipReasonPolicyPayloadInvalid, err
}

func unknownOwnershipClassification(meta *baselineMetadata, reasons []string) ownershipClassification {
	result := ownershipClassification{
		Label:       OwnershipUnknownNoBaseline,
		State:       OwnershipStateUnknownNoBaseline,
		ReasonCodes: sortAndDedupeReasons(reasons),
	}
	if meta != nil {
		result.BaselineSource = meta.source
	}
	return result
}

func ptrOwnershipConfidence(value OwnershipConfidence) *OwnershipConfidence {
	copyValue := value
	return &copyValue
}

func ptrBaselineSource(value BaselineStateSource) *BaselineStateSource {
	copyValue := value
	return &copyValue
}

func appendUnique(base []string, values []string) []string {
	if len(values) == 0 {
		return base
	}
	seen := make(map[string]struct{}, len(base)+len(values))
	for _, item := range base {
		if strings.TrimSpace(item) == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	out := append([]string{}, base...)
	for _, item := range values {
		if strings.TrimSpace(item) == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func sortAndDedupeReasons(reasons []string) []string {
	if len(reasons) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(reasons))
	out := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		trimmed := strings.TrimSpace(reason)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	sort.Strings(out)
	return out
}
