package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	templateManifestSchemaVersion = 1
	baselineStateSchemaVersion    = 1
	baselineStateRelPath          = ".agent-layer/state/managed-baseline.json"
	templateManifestDir           = "manifests"

	baselineVersionUnknown = "unknown"
)

// BaselineStateSource identifies where baseline ownership evidence came from.
type BaselineStateSource string

const (
	// BaselineStateSourceWrittenByInit indicates baseline was captured by a successful init write flow.
	BaselineStateSourceWrittenByInit BaselineStateSource = "written_by_init"
	// BaselineStateSourceWrittenByUpgrade indicates baseline was captured by an upgrade write flow.
	BaselineStateSourceWrittenByUpgrade BaselineStateSource = "written_by_overwrite"
	// BaselineStateSourceWrittenByOverwrite is a legacy name for BaselineStateSourceWrittenByUpgrade.
	BaselineStateSourceWrittenByOverwrite BaselineStateSource = BaselineStateSourceWrittenByUpgrade
	// BaselineStateSourceInferredFromPinManifest indicates baseline was inferred from a pinned release manifest.
	BaselineStateSourceInferredFromPinManifest BaselineStateSource = "inferred_from_pin_manifest"
	// BaselineStateSourceMigratedFromLegacyDocsSnapshot indicates baseline was inferred from legacy docs snapshot files.
	BaselineStateSourceMigratedFromLegacyDocsSnapshot BaselineStateSource = "migrated_from_legacy_docs_snapshot"
)

type manifestFileEntry struct {
	Path               string          `json:"path"`
	FullHashNormalized string          `json:"full_hash_normalized"`
	PolicyID           string          `json:"policy_id,omitempty"`
	PolicyPayload      json.RawMessage `json:"policy_payload,omitempty"`
}

type templateManifest struct {
	SchemaVersion int                 `json:"schema_version"`
	Version       string              `json:"version"`
	GeneratedAt   string              `json:"generated_at_utc"`
	Files         []manifestFileEntry `json:"files"`
	Metadata      map[string]any      `json:"metadata,omitempty"`
}

type managedBaselineState struct {
	SchemaVersion   int                 `json:"schema_version"`
	BaselineVersion string              `json:"baseline_version"`
	Source          BaselineStateSource `json:"source"`
	CreatedAt       string              `json:"created_at_utc"`
	UpdatedAt       string              `json:"updated_at_utc"`
	Files           []manifestFileEntry `json:"files"`
	Metadata        map[string]any      `json:"metadata,omitempty"`
}

var (
	allTemplateManifestOnce sync.Once
	allTemplateManifestByV  map[string]templateManifest
	allTemplateManifestErr  error
)

func loadTemplateManifestByVersion(versionRaw string) (templateManifest, error) {
	normalized, err := version.Normalize(versionRaw)
	if err != nil {
		return templateManifest{}, fmt.Errorf(messages.InstallInvalidPinVersionFmt, err)
	}
	manifestPath := path.Join(templateManifestDir, normalized+".json")
	data, err := templates.Read(manifestPath)
	if err != nil {
		return templateManifest{}, err
	}
	var manifest templateManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return templateManifest{}, fmt.Errorf("decode template manifest %s: %w", manifestPath, err)
	}
	if err := validateTemplateManifest(manifest); err != nil {
		return templateManifest{}, fmt.Errorf("validate template manifest %s: %w", manifestPath, err)
	}
	if manifest.Version != normalized {
		return templateManifest{}, fmt.Errorf("template manifest %s has version %q; expected %q", manifestPath, manifest.Version, normalized)
	}
	return manifest, nil
}

func loadAllTemplateManifests() (map[string]templateManifest, error) {
	allTemplateManifestOnce.Do(func() {
		manifests := make(map[string]templateManifest)
		walkErr := templates.Walk(templateManifestDir, func(templatePath string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(templatePath, ".json") {
				return nil
			}
			data, readErr := templates.Read(templatePath)
			if readErr != nil {
				return readErr
			}
			var manifest templateManifest
			if unmarshalErr := json.Unmarshal(data, &manifest); unmarshalErr != nil {
				return fmt.Errorf("decode template manifest %s: %w", templatePath, unmarshalErr)
			}
			if validateErr := validateTemplateManifest(manifest); validateErr != nil {
				return fmt.Errorf("validate template manifest %s: %w", templatePath, validateErr)
			}
			if _, exists := manifests[manifest.Version]; exists {
				return fmt.Errorf("duplicate template manifest version %q", manifest.Version)
			}
			manifests[manifest.Version] = manifest
			return nil
		})
		if walkErr != nil {
			allTemplateManifestErr = walkErr
			return
		}
		if len(manifests) == 0 {
			allTemplateManifestErr = fmt.Errorf("no embedded template manifests found")
			return
		}
		allTemplateManifestByV = manifests
	})
	if allTemplateManifestErr != nil {
		return nil, allTemplateManifestErr
	}
	cloned := make(map[string]templateManifest, len(allTemplateManifestByV))
	for key, value := range allTemplateManifestByV {
		cloned[key] = value
	}
	return cloned, nil
}

func validateTemplateManifest(manifest templateManifest) error {
	if manifest.SchemaVersion != templateManifestSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("version is required")
	}
	normalized, err := version.Normalize(manifest.Version)
	if err != nil {
		return fmt.Errorf("invalid version %q: %w", manifest.Version, err)
	}
	if normalized != manifest.Version {
		return fmt.Errorf("version %q must be normalized to X.Y.Z", manifest.Version)
	}
	if strings.TrimSpace(manifest.GeneratedAt) == "" {
		return fmt.Errorf("generated_at_utc is required")
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at_utc %q: %w", manifest.GeneratedAt, err)
	}
	if len(manifest.Files) == 0 {
		return fmt.Errorf("files is required")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if err := validateManifestFileEntry(file); err != nil {
			return err
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("duplicate manifest path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func validateManifestFileEntry(file manifestFileEntry) error {
	if strings.TrimSpace(file.Path) == "" {
		return fmt.Errorf("manifest file path is required")
	}
	if strings.TrimSpace(file.FullHashNormalized) == "" {
		return fmt.Errorf("manifest file %s full_hash_normalized is required", file.Path)
	}
	if file.PolicyID == "" && len(file.PolicyPayload) != 0 {
		return fmt.Errorf("manifest file %s policy_payload requires policy_id", file.Path)
	}
	if file.PolicyID != "" {
		if _, err := validatePolicyPayload(file.PolicyID, file.PolicyPayload); err != nil {
			return fmt.Errorf("manifest file %s policy payload invalid: %w", file.Path, err)
		}
	}
	return nil
}

func manifestFileMap(entries []manifestFileEntry) map[string]manifestFileEntry {
	out := make(map[string]manifestFileEntry, len(entries))
	for _, entry := range entries {
		out[entry.Path] = entry
	}
	return out
}

func readManagedBaselineState(root string, sys System) (managedBaselineState, error) {
	if sys == nil {
		return managedBaselineState{}, fmt.Errorf(messages.InstallSystemRequired)
	}
	path := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	data, err := sys.ReadFile(path)
	if err != nil {
		return managedBaselineState{}, err
	}
	var state managedBaselineState
	if err := json.Unmarshal(data, &state); err != nil {
		return managedBaselineState{}, fmt.Errorf("decode managed baseline state %s: %w", path, err)
	}
	if err := validateManagedBaselineState(state); err != nil {
		return managedBaselineState{}, fmt.Errorf("validate managed baseline state %s: %w", path, err)
	}
	return state, nil
}

func validateManagedBaselineState(state managedBaselineState) error {
	if state.SchemaVersion != baselineStateSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", state.SchemaVersion)
	}
	if strings.TrimSpace(state.BaselineVersion) == "" {
		return fmt.Errorf("baseline_version is required")
	}
	if strings.TrimSpace(string(state.Source)) == "" {
		return fmt.Errorf("source is required")
	}
	if _, err := time.Parse(time.RFC3339, state.CreatedAt); err != nil {
		return fmt.Errorf("invalid created_at_utc %q: %w", state.CreatedAt, err)
	}
	if _, err := time.Parse(time.RFC3339, state.UpdatedAt); err != nil {
		return fmt.Errorf("invalid updated_at_utc %q: %w", state.UpdatedAt, err)
	}
	if len(state.Files) == 0 {
		return fmt.Errorf("files is required")
	}
	seen := make(map[string]struct{}, len(state.Files))
	for _, file := range state.Files {
		if err := validateManifestFileEntry(file); err != nil {
			return err
		}
		if _, exists := seen[file.Path]; exists {
			return fmt.Errorf("duplicate baseline file path %q", file.Path)
		}
		seen[file.Path] = struct{}{}
	}
	return nil
}

func writeManagedBaselineState(root string, sys System, state managedBaselineState) error {
	if sys == nil {
		return fmt.Errorf(messages.InstallSystemRequired)
	}
	if err := validateManagedBaselineState(state); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode managed baseline state: %w", err)
	}
	data = append(data, '\n')
	if err := sys.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func buildCurrentTemplateManifest(inst *installer, generatedAt time.Time) (templateManifest, error) {
	entries, err := inst.templates().currentTemplateEntries()
	if err != nil {
		return templateManifest{}, err
	}
	files := make([]manifestFileEntry, 0, len(entries))
	for _, entry := range entries {
		templateBytes, err := templates.Read(entry.templatePath)
		if err != nil {
			return templateManifest{}, fmt.Errorf(messages.InstallFailedReadTemplateFmt, entry.templatePath, err)
		}
		comp, compErr := buildOwnershipComparable(entry.relPath, templateBytes)
		if compErr != nil {
			return templateManifest{}, fmt.Errorf("build ownership comparable for %s: %w", entry.relPath, compErr)
		}
		payload, payloadErr := ownershipPolicyPayload(comp)
		if payloadErr != nil {
			return templateManifest{}, fmt.Errorf("build ownership policy payload for %s: %w", entry.relPath, payloadErr)
		}
		files = append(files, manifestFileEntry{
			Path:               entry.relPath,
			FullHashNormalized: comp.FullHash,
			PolicyID:           comp.PolicyID,
			PolicyPayload:      payload,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	return templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       resolveBaselineVersion(inst),
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		Files:         files,
		Metadata: map[string]any{
			"source": "embedded_templates",
		},
	}, nil
}

func resolveBaselineVersion(inst *installer) string {
	if inst == nil {
		return baselineVersionUnknown
	}
	if strings.TrimSpace(inst.pinVersion) != "" {
		return inst.pinVersion
	}
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	data, err := inst.sys.ReadFile(path)
	if err == nil {
		normalized, normErr := version.Normalize(strings.TrimSpace(string(data)))
		if normErr == nil {
			return normalized
		}
	}
	return baselineVersionUnknown
}

func baselineFileEntriesFromManifest(manifest templateManifest) []manifestFileEntry {
	entries := make([]manifestFileEntry, len(manifest.Files))
	copy(entries, manifest.Files)
	return entries
}

func makeManagedBaselineState(
	manifest templateManifest,
	source BaselineStateSource,
	now time.Time,
	existing *managedBaselineState,
) managedBaselineState {
	createdAt := now.UTC().Format(time.RFC3339)
	if existing != nil && strings.TrimSpace(existing.CreatedAt) != "" {
		createdAt = existing.CreatedAt
	}
	return managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: manifest.Version,
		Source:          source,
		CreatedAt:       createdAt,
		UpdatedAt:       now.UTC().Format(time.RFC3339),
		Files:           baselineFileEntriesFromManifest(manifest),
		Metadata: map[string]any{
			"manifest_generated_at_utc": manifest.GeneratedAt,
		},
	}
}

func readCurrentPinVersion(root string, sys System) (string, error) {
	if sys == nil {
		return "", fmt.Errorf(messages.InstallSystemRequired)
	}
	path := filepath.Join(root, ".agent-layer", "al.version")
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", nil
	}
	normalized, normalizeErr := version.Normalize(trimmed)
	if normalizeErr == nil {
		return normalized, nil
	}
	return "", nil
}

func (inst *installer) writeManagedBaselineIfConsistent(source BaselineStateSource) error {
	if inst == nil || inst.sys == nil {
		return nil
	}
	managedDiffs, err := inst.templates().listManagedDiffs()
	if err != nil {
		return err
	}
	memoryDiffs, err := inst.templates().listMemoryDiffs()
	if err != nil {
		return err
	}
	if len(managedDiffs) != 0 || len(memoryDiffs) != 0 {
		return nil
	}
	now := time.Now().UTC()
	manifest, err := buildCurrentTemplateManifest(inst, now)
	if err != nil {
		return err
	}
	var existing *managedBaselineState
	existingState, err := readManagedBaselineState(inst.root, inst.sys)
	switch {
	case err == nil:
		existing = &existingState
	case errors.Is(err, os.ErrNotExist):
		// no-op; create a new baseline state
	default:
		return err
	}
	state := makeManagedBaselineState(manifest, source, now, existing)
	if err := writeManagedBaselineState(inst.root, inst.sys, state); err != nil {
		return err
	}
	return nil
}
