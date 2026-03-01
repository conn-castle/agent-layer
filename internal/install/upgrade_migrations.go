package install

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	tomlv2 "github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	upgradeMigrationManifestSchemaVersion = 1
	upgradeMigrationManifestDir           = "migrations"
	upgradeMigrationConfigPath            = ".agent-layer/config.toml"
)

// UpgradeMigrationSourceOrigin identifies where migration source-version evidence came from.
type UpgradeMigrationSourceOrigin string

const (
	// UpgradeMigrationSourceUnknown means source version could not be determined.
	UpgradeMigrationSourceUnknown UpgradeMigrationSourceOrigin = "unknown"
	// UpgradeMigrationSourcePin means source version came from .agent-layer/al.version.
	UpgradeMigrationSourcePin UpgradeMigrationSourceOrigin = "pin_file"
	// UpgradeMigrationSourceBaseline means source version came from managed baseline state.
	UpgradeMigrationSourceBaseline UpgradeMigrationSourceOrigin = "managed_baseline"
	// UpgradeMigrationSourceSnapshot means source version came from latest upgrade snapshot pin entry.
	UpgradeMigrationSourceSnapshot UpgradeMigrationSourceOrigin = "upgrade_snapshot"
	// UpgradeMigrationSourceManifestMatch means source version was inferred from embedded manifest fingerprint matching.
	UpgradeMigrationSourceManifestMatch UpgradeMigrationSourceOrigin = "manifest_match"
)

// UpgradeMigrationStatus describes migration execution/planning status.
type UpgradeMigrationStatus string

const (
	// UpgradeMigrationStatusPlanned means migration is eligible to run.
	UpgradeMigrationStatusPlanned UpgradeMigrationStatus = "planned"
	// UpgradeMigrationStatusApplied means migration mutated repository state during apply.
	UpgradeMigrationStatusApplied UpgradeMigrationStatus = "applied"
	// UpgradeMigrationStatusNoop means migration ran but made no changes (already migrated/idempotent).
	UpgradeMigrationStatusNoop UpgradeMigrationStatus = "no_op"
	// UpgradeMigrationStatusSkippedUnknownSource means migration requires a known source version but source is unknown.
	UpgradeMigrationStatusSkippedUnknownSource UpgradeMigrationStatus = "skipped_unknown_source"
	// UpgradeMigrationStatusSkippedSourceTooOld means migration requires a newer prior version than the resolved source.
	UpgradeMigrationStatusSkippedSourceTooOld UpgradeMigrationStatus = "skipped_source_too_old"
)

// UpgradeMigrationEntry is a deterministic migration-plan/apply record.
type UpgradeMigrationEntry struct {
	ID             string                 `json:"id"`
	Kind           string                 `json:"kind"`
	Rationale      string                 `json:"rationale"`
	SourceAgnostic bool                   `json:"source_agnostic"`
	Status         UpgradeMigrationStatus `json:"status"`
	SkipReason     string                 `json:"skip_reason,omitempty"`
	From           string                 `json:"from,omitempty"`
	To             string                 `json:"to,omitempty"`
	Path           string                 `json:"path,omitempty"`
	Key            string                 `json:"key,omitempty"`
	Value          json.RawMessage        `json:"value,omitempty"`
}

// UpgradeMigrationReport contains deterministic migration planning/execution data for upgrade output.
type UpgradeMigrationReport struct {
	TargetVersion         string                       `json:"target_version,omitempty"`
	MinPriorVersion       string                       `json:"min_prior_version,omitempty"`
	ManifestPath          string                       `json:"manifest_path,omitempty"`
	SourceVersion         string                       `json:"source_version"`
	SourceVersionOrigin   UpgradeMigrationSourceOrigin `json:"source_version_origin"`
	SourceResolutionNotes []string                     `json:"source_resolution_notes,omitempty"`
	Entries               []UpgradeMigrationEntry      `json:"entries"`
}

type upgradeMigrationOperationKind string

const (
	upgradeMigrationKindRenameFile              upgradeMigrationOperationKind = "rename_file"
	upgradeMigrationKindDeleteFile              upgradeMigrationOperationKind = "delete_file"
	upgradeMigrationKindRenameGeneratedArtifact upgradeMigrationOperationKind = "rename_generated_artifact"
	upgradeMigrationKindDeleteGeneratedArtifact upgradeMigrationOperationKind = "delete_generated_artifact"
	upgradeMigrationKindConfigRenameKey         upgradeMigrationOperationKind = "config_rename_key"
	upgradeMigrationKindConfigSetDefault        upgradeMigrationOperationKind = "config_set_default"
	upgradeMigrationKindMigrateSkillsFormat     upgradeMigrationOperationKind = "migrate_skills_format"
)

type upgradeMigrationOperation struct {
	ID             string                        `json:"id"`
	Kind           upgradeMigrationOperationKind `json:"kind"`
	Rationale      string                        `json:"rationale"`
	SourceAgnostic bool                          `json:"source_agnostic,omitempty"`
	From           string                        `json:"from,omitempty"`
	To             string                        `json:"to,omitempty"`
	Path           string                        `json:"path,omitempty"`
	Key            string                        `json:"key,omitempty"`
	Value          json.RawMessage               `json:"value,omitempty"`
}

type upgradeMigrationManifest struct {
	SchemaVersion   int                         `json:"schema_version"`
	TargetVersion   string                      `json:"target_version"`
	MinPriorVersion string                      `json:"min_prior_version"`
	Operations      []upgradeMigrationOperation `json:"operations"`
}

type sourceVersionResolution struct {
	version string
	origin  UpgradeMigrationSourceOrigin
	notes   []string
}

type migrationPlan struct {
	report           UpgradeMigrationReport
	executable       []upgradeMigrationOperation
	rollbackTargets  []string
	coveredPaths     map[string]struct{}
	configMigrations []ConfigKeyMigration
}

func (inst *installer) prepareUpgradeMigrations() error {
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		return err
	}
	inst.pendingMigrationOps = plan.executable
	inst.migrationRollbackTargets = plan.rollbackTargets
	inst.migrationManifestCoverage = plan.coveredPaths
	inst.migrationConfigMigrations = plan.configMigrations
	inst.migrationReport = plan.report
	inst.migrationsPrepared = true
	return nil
}

func (inst *installer) planUpgradeMigrations() (migrationPlan, error) {
	plan := migrationPlan{
		report: UpgradeMigrationReport{
			SourceVersion:       string(UpgradeMigrationSourceUnknown),
			SourceVersionOrigin: UpgradeMigrationSourceUnknown,
			Entries:             []UpgradeMigrationEntry{},
		},
		coveredPaths: make(map[string]struct{}),
	}
	if strings.TrimSpace(inst.pinVersion) == "" {
		return plan, nil
	}

	// Always load and validate the target manifest first. This ensures a
	// missing target manifest fails loudly regardless of source resolution.
	targetManifest, targetManifestPath, err := loadUpgradeMigrationManifestByVersion(inst.pinVersion)
	if err != nil {
		return migrationPlan{}, err
	}

	resolution := inst.resolveUpgradeMigrationSourceVersion()
	plan.report.SourceVersion = resolution.version
	plan.report.SourceVersionOrigin = resolution.origin
	plan.report.SourceResolutionNotes = dedupSortedStrings(resolution.notes)

	// Determine which manifests to load: when source is known, chain all
	// intermediate manifests (source, target]; when unknown, load only target.
	sourceKnown := resolution.origin != UpgradeMigrationSourceUnknown
	var manifests []chainedManifest
	if sourceKnown {
		chain, chainErr := collectMigrationChain(resolution.version, inst.pinVersion)
		if chainErr != nil {
			return migrationPlan{}, chainErr
		}
		if len(chain) == 0 {
			// No manifests in range (source == target). Target was already
			// validated above; nothing to migrate.
			plan.report.TargetVersion = inst.pinVersion
			return plan, nil
		}
		manifests = chain
	} else {
		manifests = []chainedManifest{{manifest: targetManifest, path: targetManifestPath}}
	}

	// Report fields from the chain.
	plan.report.TargetVersion = manifests[len(manifests)-1].manifest.TargetVersion
	plan.report.MinPriorVersion = manifests[0].manifest.MinPriorVersion
	chainPaths := make([]string, 0, len(manifests))
	for _, cm := range manifests {
		chainPaths = append(chainPaths, cm.path)
	}
	plan.report.ManifestPath = strings.Join(chainPaths, ",")

	seenOpIDs := make(map[string]struct{})
	entries := make([]UpgradeMigrationEntry, 0)
	rollbackTargets := make([]string, 0)
	configMigrations := make([]ConfigKeyMigration, 0)

	for _, cm := range manifests {
		operations := sortedUpgradeMigrationOperations(cm.manifest.Operations)
		for _, op := range operations {
			// Deduplicate by operation ID across the chain.
			if _, seen := seenOpIDs[op.ID]; seen {
				continue
			}
			seenOpIDs[op.ID] = struct{}{}

			entry := migrationEntryFromOperation(op)
			status := UpgradeMigrationStatusPlanned
			skipReason := ""
			if !op.SourceAgnostic {
				if resolution.version == string(UpgradeMigrationSourceUnknown) {
					status = UpgradeMigrationStatusSkippedUnknownSource
					skipReason = "source version is unknown"
				} else {
					cmp, cmpErr := compareSemver(resolution.version, cm.manifest.MinPriorVersion)
					if cmpErr != nil {
						return migrationPlan{}, fmt.Errorf("compare source version %s with min_prior_version %s: %w", resolution.version, cm.manifest.MinPriorVersion, cmpErr)
					}
					if cmp < 0 {
						status = UpgradeMigrationStatusSkippedSourceTooOld
						skipReason = fmt.Sprintf("source version %s is older than min prior version %s", resolution.version, cm.manifest.MinPriorVersion)
					}
				}
			}
			entry.Status = status
			entry.SkipReason = skipReason
			entries = append(entries, entry)
			if status != UpgradeMigrationStatusPlanned {
				continue
			}

			plan.executable = append(plan.executable, op)
			for _, relPath := range migrationCoveredPaths(op) {
				absPath, absErr := snapshotEntryAbsPath(inst.root, relPath)
				if absErr != nil {
					return migrationPlan{}, absErr
				}
				rollbackTargets = append(rollbackTargets, absPath)
				if migrationWillCoverPath(inst.sys, inst.root, op, relPath) {
					plan.coveredPaths[relPath] = struct{}{}
				}
			}
			if isConfigMigrationKind(op.Kind) {
				configPath, cfgErr := snapshotEntryAbsPath(inst.root, upgradeMigrationConfigPath)
				if cfgErr != nil {
					return migrationPlan{}, cfgErr
				}
				rollbackTargets = append(rollbackTargets, configPath)
				if cfgMigration, ok := configMigrationFromOperation(op); ok {
					configMigrations = append(configMigrations, cfgMigration)
				}
			}
		}
	}

	plan.report.Entries = entries
	plan.rollbackTargets = uniqueNormalizedPaths(rollbackTargets)
	plan.configMigrations = configMigrations
	return plan, nil
}

func (inst *installer) runMigrations() error {
	if !inst.migrationsPrepared {
		if err := inst.prepareUpgradeMigrations(); err != nil {
			return err
		}
	}
	if len(inst.migrationReport.Entries) == 0 {
		return nil
	}

	entryIndex := make(map[string]int, len(inst.migrationReport.Entries))
	for idx, entry := range inst.migrationReport.Entries {
		entryIndex[entry.ID] = idx
	}

	for _, op := range inst.pendingMigrationOps {
		changed, err := inst.executeUpgradeMigrationOperation(op)
		if err != nil {
			return fmt.Errorf("execute migration %s (%s): %w", op.ID, op.Kind, err)
		}
		idx, ok := entryIndex[op.ID]
		if !ok {
			continue
		}
		if changed {
			inst.migrationReport.Entries[idx].Status = UpgradeMigrationStatusApplied
			continue
		}
		inst.migrationReport.Entries[idx].Status = UpgradeMigrationStatusNoop
	}

	return writeUpgradeMigrationReport(inst.warnOutput(), inst.migrationReport)
}

// errWriter wraps an io.Writer and accumulates the first error encountered,
// allowing sequential writes without per-call error checks.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) printf(format string, args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}

func (ew *errWriter) println(args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintln(ew.w, args...)
}

func writeUpgradeMigrationReport(out io.Writer, report UpgradeMigrationReport) error {
	if len(report.Entries) == 0 {
		return nil
	}
	ew := &errWriter{w: out}
	ew.println("Migration report:")
	ew.printf("  - target version: %s\n", report.TargetVersion)
	ew.printf("  - source version: %s (%s)\n", report.SourceVersion, report.SourceVersionOrigin)
	for _, note := range report.SourceResolutionNotes {
		ew.printf("  - source note: %s\n", note)
	}
	for _, entry := range report.Entries {
		ew.printf("  - [%s] %s (%s): %s\n", entry.Status, entry.ID, entry.Kind, entry.Rationale)
		if entry.SkipReason != "" {
			ew.printf("    reason: %s\n", entry.SkipReason)
		}
		if entry.From != "" {
			ew.printf("    from: %s\n", entry.From)
		}
		if entry.To != "" {
			ew.printf("    to: %s\n", entry.To)
		}
		if entry.Path != "" {
			ew.printf("    path: %s\n", entry.Path)
		}
		if entry.Key != "" {
			ew.printf("    key: %s\n", entry.Key)
		}
	}
	ew.println()
	return ew.err
}

func (inst *installer) executeUpgradeMigrationOperation(op upgradeMigrationOperation) (bool, error) {
	switch op.Kind {
	case upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact:
		return inst.executeRenameMigration(op.From, op.To)
	case upgradeMigrationKindDeleteFile, upgradeMigrationKindDeleteGeneratedArtifact:
		return inst.executeDeleteMigration(op.Path)
	case upgradeMigrationKindConfigRenameKey:
		return inst.executeConfigRenameKeyMigration(op.From, op.To)
	case upgradeMigrationKindConfigSetDefault:
		return inst.executeConfigSetDefaultMigration(op)
	case upgradeMigrationKindMigrateSkillsFormat:
		return inst.executeMigrateSkillsFormat(op.Path)
	default:
		return false, fmt.Errorf("unsupported migration kind %q", op.Kind)
	}
}

func (inst *installer) executeRenameMigration(fromRel string, toRel string) (bool, error) {
	fromPath, err := snapshotEntryAbsPath(inst.root, fromRel)
	if err != nil {
		return false, err
	}
	toPath, err := snapshotEntryAbsPath(inst.root, toRel)
	if err != nil {
		return false, err
	}
	if filepath.Clean(fromPath) == filepath.Clean(toPath) {
		return false, nil
	}

	fromInfo, err := inst.sys.Stat(fromPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, statErr := inst.sys.Stat(toPath)
			if statErr == nil || errors.Is(statErr, os.ErrNotExist) {
				return false, nil
			}
			return false, fmt.Errorf(messages.InstallFailedStatFmt, toPath, statErr)
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, fromPath, err)
	}

	toInfo, err := inst.sys.Stat(toPath)
	if err == nil {
		if fromInfo.IsDir() && toInfo.IsDir() {
			dirNotEmptyErr := errors.New("directory is not empty")
			walkErr := inst.sys.WalkDir(toPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if filepath.Clean(path) == filepath.Clean(toPath) {
					return nil
				}
				return dirNotEmptyErr
			})
			if walkErr != nil && !errors.Is(walkErr, dirNotEmptyErr) {
				return false, fmt.Errorf(messages.InstallFailedReadFmt, toPath, walkErr)
			}
			if walkErr == nil {
				if removeErr := inst.sys.RemoveAll(toPath); removeErr != nil {
					return false, fmt.Errorf("remove empty rename destination %s: %w", toRel, removeErr)
				}
				if renameErr := inst.sys.Rename(fromPath, toPath); renameErr != nil {
					return false, fmt.Errorf("rename %s -> %s: %w", fromRel, toRel, renameErr)
				}
				return true, nil
			}
		}
		if fromInfo.Mode().IsRegular() && toInfo.Mode().IsRegular() {
			fromBytes, readFromErr := inst.sys.ReadFile(fromPath)
			if readFromErr != nil {
				return false, fmt.Errorf(messages.InstallFailedReadFmt, fromPath, readFromErr)
			}
			toBytes, readToErr := inst.sys.ReadFile(toPath)
			if readToErr != nil {
				return false, fmt.Errorf(messages.InstallFailedReadFmt, toPath, readToErr)
			}
			if normalizeTemplateContent(string(fromBytes)) == normalizeTemplateContent(string(toBytes)) {
				if removeErr := inst.sys.RemoveAll(fromPath); removeErr != nil {
					return false, fmt.Errorf("remove duplicate rename source %s: %w", fromRel, removeErr)
				}
				return true, nil
			}
		}
		return false, fmt.Errorf("rename migration target already exists: %s", toRel)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf(messages.InstallFailedStatFmt, toPath, err)
	}

	if mkErr := inst.sys.MkdirAll(filepath.Dir(toPath), 0o755); mkErr != nil {
		return false, fmt.Errorf(messages.InstallFailedCreateDirForFmt, toPath, mkErr)
	}
	if renameErr := inst.sys.Rename(fromPath, toPath); renameErr != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", fromRel, toRel, renameErr)
	}
	return true, nil
}

func (inst *installer) executeDeleteMigration(relPath string) (bool, error) {
	absPath, err := snapshotEntryAbsPath(inst.root, relPath)
	if err != nil {
		return false, err
	}
	if _, statErr := inst.sys.Stat(absPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, absPath, statErr)
	}
	if removeErr := inst.sys.RemoveAll(absPath); removeErr != nil {
		return false, fmt.Errorf("delete migration path %s: %w", relPath, removeErr)
	}
	return true, nil
}

func (inst *installer) executeConfigRenameKeyMigration(fromKey string, toKey string) (bool, error) {
	if strings.TrimSpace(fromKey) == strings.TrimSpace(toKey) {
		return false, nil
	}
	cfg, cfgPath, exists, err := inst.readMigrationConfigMap()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	fromParts, err := splitMigrationKeyPath(fromKey)
	if err != nil {
		return false, err
	}
	toParts, err := splitMigrationKeyPath(toKey)
	if err != nil {
		return false, err
	}
	fromValue, fromExists, err := getNestedConfigValue(cfg, fromParts)
	if err != nil {
		return false, err
	}
	toValue, toExists, err := getNestedConfigValue(cfg, toParts)
	if err != nil {
		return false, err
	}
	if !fromExists {
		return false, nil
	}
	if toExists {
		if reflect.DeepEqual(fromValue, toValue) {
			removed, removeErr := deleteNestedConfigValue(cfg, fromParts)
			if removeErr != nil {
				return false, removeErr
			}
			if !removed {
				return false, nil
			}
			if writeErr := inst.writeMigrationConfigMap(cfgPath, cfg); writeErr != nil {
				return false, writeErr
			}
			return true, nil
		}
		return false, fmt.Errorf("config key rename conflict: destination key %s already exists", toKey)
	}
	if setErr := setNestedConfigValue(cfg, toParts, fromValue, true); setErr != nil {
		return false, setErr
	}
	if _, removeErr := deleteNestedConfigValue(cfg, fromParts); removeErr != nil {
		return false, removeErr
	}
	if writeErr := inst.writeMigrationConfigMap(cfgPath, cfg); writeErr != nil {
		return false, writeErr
	}
	return true, nil
}

func (inst *installer) executeConfigSetDefaultMigration(op upgradeMigrationOperation) (bool, error) {
	keyPath := op.Key
	rawValue := op.Value
	cfg, cfgPath, exists, err := inst.readMigrationConfigMap()
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	parts, err := splitMigrationKeyPath(keyPath)
	if err != nil {
		return false, err
	}
	if _, keyExists, getErr := getNestedConfigValue(cfg, parts); getErr != nil {
		return false, getErr
	} else if keyExists {
		return false, nil
	}
	var decoded any
	if unmarshalErr := json.Unmarshal(rawValue, &decoded); unmarshalErr != nil {
		return false, fmt.Errorf("decode default value for key %s: %w", keyPath, unmarshalErr)
	}
	if prompter, ok := inst.prompter.(configSetDefaultPrompter); ok {
		var fieldPtr *config.FieldDef
		if f, found := config.LookupField(keyPath); found {
			fieldPtr = &f
		}
		prompted, promptErr := prompter.ConfigSetDefault(keyPath, decoded, op.Rationale, fieldPtr)
		if promptErr != nil {
			return false, fmt.Errorf("prompt for config key %s: %w", keyPath, promptErr)
		}
		decoded = prompted
	}
	if setErr := setNestedConfigValue(cfg, parts, decoded, true); setErr != nil {
		return false, setErr
	}
	if writeErr := inst.writeMigrationConfigMap(cfgPath, cfg); writeErr != nil {
		return false, writeErr
	}
	return true, nil
}

func (inst *installer) readMigrationConfigMap() (map[string]any, string, bool, error) {
	cfgPath := filepath.Join(inst.root, filepath.FromSlash(upgradeMigrationConfigPath))
	data, err := inst.sys.ReadFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, cfgPath, false, nil
		}
		return nil, cfgPath, false, fmt.Errorf(messages.InstallFailedReadFmt, cfgPath, err)
	}
	var cfg map[string]any
	if unmarshalErr := tomlv2.Unmarshal(data, &cfg); unmarshalErr != nil {
		return nil, cfgPath, false, fmt.Errorf("decode config %s for migration: %w", cfgPath, unmarshalErr)
	}
	if cfg == nil {
		cfg = make(map[string]any)
	}
	return cfg, cfgPath, true, nil
}

// writeMigrationConfigMap writes the updated config map back to config.toml.
// NOTE: This currently uses tomlv2.Marshal which does not preserve user comments
// or key ordering. This destructive formatting is currently intentional to ensure
// deterministic migration output.
func (inst *installer) writeMigrationConfigMap(cfgPath string, cfg map[string]any) error {
	encoded, err := tomlv2.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config migration output: %w", err)
	}
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		encoded = append(encoded, '\n')
	}
	if writeErr := inst.sys.WriteFileAtomic(cfgPath, encoded, 0o644); writeErr != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, cfgPath, writeErr)
	}
	return nil
}

func getNestedConfigValue(cfg map[string]any, parts []string) (any, bool, error) {
	if len(parts) == 0 {
		return nil, false, fmt.Errorf("config key path is required")
	}
	current := cfg
	for idx := 0; idx < len(parts)-1; idx++ {
		value, ok := current[parts[idx]]
		if !ok {
			return nil, false, nil
		}
		nested, nestedOK := asStringAnyMap(value)
		if !nestedOK {
			return nil, false, fmt.Errorf("config key path %s traverses non-table value", strings.Join(parts[:idx+1], "."))
		}
		current = nested
	}
	value, ok := current[parts[len(parts)-1]]
	if !ok {
		return nil, false, nil
	}
	return value, true, nil
}

func setNestedConfigValue(cfg map[string]any, parts []string, value any, create bool) error {
	if len(parts) == 0 {
		return fmt.Errorf("config key path is required")
	}
	current := cfg
	for idx := 0; idx < len(parts)-1; idx++ {
		segment := parts[idx]
		existing, ok := current[segment]
		if !ok {
			if !create {
				return fmt.Errorf("missing config table %s", strings.Join(parts[:idx+1], "."))
			}
			next := make(map[string]any)
			current[segment] = next
			current = next
			continue
		}
		nested, nestedOK := asStringAnyMap(existing)
		if !nestedOK {
			return fmt.Errorf("config key path %s traverses non-table value", strings.Join(parts[:idx+1], "."))
		}
		current = nested
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func deleteNestedConfigValue(cfg map[string]any, parts []string) (bool, error) {
	if len(parts) == 0 {
		return false, fmt.Errorf("config key path is required")
	}
	// Track parent tables so we can prune empty ones after deletion.
	parents := make([]map[string]any, 0, len(parts))
	parents = append(parents, cfg)
	current := cfg
	for idx := 0; idx < len(parts)-1; idx++ {
		value, ok := current[parts[idx]]
		if !ok {
			return false, nil
		}
		nested, nestedOK := asStringAnyMap(value)
		if !nestedOK {
			return false, fmt.Errorf("config key path %s traverses non-table value", strings.Join(parts[:idx+1], "."))
		}
		current = nested
		parents = append(parents, current)
	}
	leaf := parts[len(parts)-1]
	if _, ok := current[leaf]; !ok {
		return false, nil
	}
	delete(current, leaf)
	// Prune empty intermediate tables left behind by the deletion.
	// Walk backward from the deepest parent to the root (exclusive).
	for i := len(parents) - 1; i > 0; i-- {
		if len(parents[i]) == 0 {
			delete(parents[i-1], parts[i-1])
		}
	}
	return true, nil
}

func asStringAnyMap(value any) (map[string]any, bool) {
	asAny, ok := value.(map[string]any)
	if ok {
		return asAny, true
	}
	asInterface, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	converted := make(map[string]any, len(asInterface))
	for key, val := range asInterface {
		converted[key] = val
	}
	return converted, true
}

func splitMigrationKeyPath(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("migration config key path is required")
	}
	parts := strings.Split(trimmed, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		segment := strings.TrimSpace(part)
		if segment == "" {
			return nil, fmt.Errorf("invalid migration config key path %q", raw)
		}
		out = append(out, segment)
	}
	return out, nil
}

func isConfigMigrationKind(kind upgradeMigrationOperationKind) bool {
	return kind == upgradeMigrationKindConfigRenameKey || kind == upgradeMigrationKindConfigSetDefault
}

func migrationCoveredPaths(op upgradeMigrationOperation) []string {
	paths := make([]string, 0, 2)
	switch op.Kind {
	case upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact:
		from := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.From)))
		to := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.To)))
		if strings.TrimSpace(from) != "" {
			paths = append(paths, from)
		}
		if strings.TrimSpace(to) != "" {
			paths = append(paths, to)
		}
	case upgradeMigrationKindDeleteFile, upgradeMigrationKindDeleteGeneratedArtifact:
		pathValue := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.Path)))
		if strings.TrimSpace(pathValue) != "" {
			paths = append(paths, pathValue)
		}
	case upgradeMigrationKindMigrateSkillsFormat:
		pathValue := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.Path)))
		if strings.TrimSpace(pathValue) != "" {
			paths = append(paths, pathValue)
		}
	}
	return dedupSortedStrings(paths)
}

// migrationWillCoverPath returns true when the planned operation will actually
// handle relPath at execution time. This prevents filterCoveredUpgradeChanges
// from hiding template changes in the plan when a migration would no-op (e.g.,
// rename source already absent).
func migrationWillCoverPath(sys System, root string, op upgradeMigrationOperation, relPath string) bool {
	switch op.Kind {
	case upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact:
		fromRel := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.From)))
		toRel := normalizeRelPath(filepath.Clean(filepath.FromSlash(op.To)))
		if fromRel == toRel {
			return false
		}
		fromAbs, err := snapshotEntryAbsPath(root, fromRel)
		if err != nil {
			return false
		}
		if _, statErr := sys.Stat(fromAbs); statErr != nil {
			// Source doesn't exist — rename will no-op, so the template
			// writer may still need to handle the destination path.
			return false
		}
		// Source exists: rename will execute and cover both paths.
		return true
	case upgradeMigrationKindDeleteFile, upgradeMigrationKindDeleteGeneratedArtifact:
		absPath, err := snapshotEntryAbsPath(root, relPath)
		if err != nil {
			return false
		}
		if _, statErr := sys.Stat(absPath); statErr != nil {
			// File doesn't exist — delete will no-op.
			return false
		}
		return true
	case upgradeMigrationKindMigrateSkillsFormat:
		skillsDir, absErr := snapshotEntryAbsPath(root, normalizeRelPath(filepath.Clean(filepath.FromSlash(op.Path))))
		if absErr != nil {
			return false
		}
		flatCount, _, scanErr := preflightSkillsMigration(sys, skillsDir)
		if scanErr != nil {
			return false
		}
		return flatCount > 0
	default:
		// Config migrations don't cover file paths in the template sense.
		return false
	}
}

func configMigrationFromOperation(op upgradeMigrationOperation) (ConfigKeyMigration, bool) {
	switch op.Kind {
	case upgradeMigrationKindConfigRenameKey:
		return ConfigKeyMigration{Key: op.From, From: op.From, To: op.To}, true
	case upgradeMigrationKindConfigSetDefault:
		to := strings.TrimSpace(string(op.Value))
		if to == "" {
			to = "null"
		}
		return ConfigKeyMigration{Key: op.Key, From: "(unset)", To: to}, true
	default:
		return ConfigKeyMigration{}, false
	}
}

func migrationEntryFromOperation(op upgradeMigrationOperation) UpgradeMigrationEntry {
	return UpgradeMigrationEntry{
		ID:             op.ID,
		Kind:           string(op.Kind),
		Rationale:      op.Rationale,
		SourceAgnostic: op.SourceAgnostic,
		From:           op.From,
		To:             op.To,
		Path:           op.Path,
		Key:            op.Key,
		Value:          op.Value,
	}
}

func sortedUpgradeMigrationOperations(in []upgradeMigrationOperation) []upgradeMigrationOperation {
	if len(in) == 0 {
		return []upgradeMigrationOperation{}
	}
	out := make([]upgradeMigrationOperation, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (inst *installer) resolveUpgradeMigrationSourceVersion() sourceVersionResolution {
	resolution := sourceVersionResolution{
		version: string(UpgradeMigrationSourceUnknown),
		origin:  UpgradeMigrationSourceUnknown,
		notes:   []string{},
	}

	pinVersion, pinErr := readCurrentPinVersion(inst.root, inst.sys)
	if pinErr != nil {
		resolution.notes = append(resolution.notes, fmt.Sprintf("pin version unavailable: %v", pinErr))
	} else if strings.TrimSpace(pinVersion) != "" {
		resolution.version = pinVersion
		resolution.origin = UpgradeMigrationSourcePin
		return resolution
	}

	state, baselineErr := readManagedBaselineState(inst.root, inst.sys)
	if baselineErr == nil {
		normalized, normalizeErr := version.Normalize(strings.TrimSpace(state.BaselineVersion))
		if normalizeErr == nil {
			resolution.version = normalized
			resolution.origin = UpgradeMigrationSourceBaseline
			return resolution
		}
		resolution.notes = append(resolution.notes, fmt.Sprintf("managed baseline version invalid: %v", normalizeErr))
	} else if !errors.Is(baselineErr, os.ErrNotExist) {
		resolution.notes = append(resolution.notes, fmt.Sprintf("managed baseline unavailable: %v", baselineErr))
	}

	snapshotVersion, snapshotErr := inst.inferSourceVersionFromLatestSnapshot()
	if snapshotErr != nil {
		resolution.notes = append(resolution.notes, fmt.Sprintf("snapshot source inference failed: %v", snapshotErr))
	} else if strings.TrimSpace(snapshotVersion) != "" {
		resolution.version = snapshotVersion
		resolution.origin = UpgradeMigrationSourceSnapshot
		return resolution
	}

	manifestVersion, manifestErr := inst.inferSourceVersionFromManifestMatch()
	if manifestErr != nil {
		resolution.notes = append(resolution.notes, fmt.Sprintf("manifest source inference failed: %v", manifestErr))
	} else if strings.TrimSpace(manifestVersion) != "" {
		resolution.version = manifestVersion
		resolution.origin = UpgradeMigrationSourceManifestMatch
		return resolution
	}

	resolution.notes = dedupSortedStrings(resolution.notes)
	return resolution
}

func (inst *installer) inferSourceVersionFromLatestSnapshot() (string, error) {
	snapshotDir := inst.upgradeSnapshotDirPath()
	if _, err := inst.sys.Stat(snapshotDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf(messages.InstallFailedStatFmt, snapshotDir, err)
	}
	files, err := inst.listUpgradeSnapshotFiles()
	if err != nil {
		return "", err
	}
	for idx := len(files) - 1; idx >= 0; idx-- {
		snapshot, readErr := readUpgradeSnapshot(files[idx].path, inst.sys)
		if readErr != nil {
			continue
		}
		for _, entry := range snapshot.Entries {
			if entry.Path != ".agent-layer/al.version" || entry.Kind != upgradeSnapshotEntryKindFile {
				continue
			}
			decoded, decodeErr := base64.StdEncoding.DecodeString(entry.ContentBase64)
			if decodeErr != nil {
				continue
			}
			normalized, normalizeErr := version.Normalize(strings.TrimSpace(string(decoded)))
			if normalizeErr != nil {
				continue
			}
			return normalized, nil
		}
	}
	return "", nil
}

func (inst *installer) inferSourceVersionFromManifestMatch() (string, error) {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return "", err
	}
	candidates := make([]string, 0, len(manifests))
	for versionValue, manifest := range manifests {
		match, matchErr := inst.matchesTemplateDocsManifest(manifest)
		if matchErr != nil {
			return "", matchErr
		}
		if match {
			candidates = append(candidates, versionValue)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", nil
}

func (inst *installer) matchesTemplateDocsManifest(manifest templateManifest) (bool, error) {
	entries := make([]manifestFileEntry, 0)
	for _, entry := range manifest.Files {
		if strings.HasPrefix(entry.Path, "docs/agent-layer/") {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return false, nil
	}
	for _, entry := range entries {
		absPath := filepath.Join(inst.root, filepath.FromSlash(entry.Path))
		content, err := inst.sys.ReadFile(absPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			return false, fmt.Errorf(messages.InstallFailedReadFmt, absPath, err)
		}
		if hashNormalizedContent(content) != entry.FullHashNormalized {
			return false, nil
		}
	}
	return true, nil
}

func dedupSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		set[trimmed] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func compareSemver(a string, b string) (int, error) {
	aParts, err := parseSemver(a)
	if err != nil {
		return 0, err
	}
	bParts, err := parseSemver(b)
	if err != nil {
		return 0, err
	}
	for idx := 0; idx < len(aParts); idx++ {
		if aParts[idx] < bParts[idx] {
			return -1, nil
		}
		if aParts[idx] > bParts[idx] {
			return 1, nil
		}
	}
	return 0, nil
}

func parseSemver(raw string) ([3]int, error) {
	normalized, err := version.Normalize(raw)
	if err != nil {
		return [3]int{}, err
	}
	parts := strings.Split(normalized, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionFmt, raw)
	}
	var out [3]int
	for idx, part := range parts {
		value, atoiErr := strconv.Atoi(part)
		if atoiErr != nil {
			return [3]int{}, fmt.Errorf(messages.UpdateInvalidVersionSegmentFmt, part, atoiErr)
		}
		out[idx] = value
	}
	return out, nil
}

func loadUpgradeMigrationManifestByVersion(versionRaw string) (upgradeMigrationManifest, string, error) {
	normalized, err := version.Normalize(versionRaw)
	if err != nil {
		return upgradeMigrationManifest{}, "", fmt.Errorf(messages.InstallInvalidPinVersionFmt, err)
	}
	manifestPath := path.Join(upgradeMigrationManifestDir, normalized+".json")
	data, err := templates.Read(manifestPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return upgradeMigrationManifest{}, manifestPath, fmt.Errorf("missing migration manifest for target version %s at template path %s", normalized, manifestPath)
		}
		return upgradeMigrationManifest{}, manifestPath, err
	}
	var manifest upgradeMigrationManifest
	if unmarshalErr := json.Unmarshal(data, &manifest); unmarshalErr != nil {
		return upgradeMigrationManifest{}, manifestPath, fmt.Errorf("decode migration manifest %s: %w", manifestPath, unmarshalErr)
	}
	if validateErr := validateUpgradeMigrationManifest(manifest); validateErr != nil {
		return upgradeMigrationManifest{}, manifestPath, fmt.Errorf("validate migration manifest %s: %w", manifestPath, validateErr)
	}
	if manifest.TargetVersion != normalized {
		return upgradeMigrationManifest{}, manifestPath, fmt.Errorf("migration manifest %s target_version %q does not match requested version %q", manifestPath, manifest.TargetVersion, normalized)
	}
	return manifest, manifestPath, nil
}

// chainedManifest pairs a loaded manifest with its template path.
type chainedManifest struct {
	manifest upgradeMigrationManifest
	path     string
}

// listMigrationManifestVersions walks the embedded migrations directory
// and returns all available migration manifest versions, sorted ascending.
func listMigrationManifestVersions() ([]string, error) {
	var versions []string
	err := templates.Walk(upgradeMigrationManifestDir, func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".json") {
			return nil
		}
		ver := strings.TrimSuffix(name, ".json")
		if _, parseErr := parseSemver(ver); parseErr == nil {
			versions = append(versions, ver)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk migration manifests: %w", err)
	}
	sort.Slice(versions, func(i, j int) bool {
		cmp, _ := compareSemver(versions[i], versions[j])
		return cmp < 0
	})
	return versions, nil
}

// collectMigrationChain loads all migration manifests between sourceVersion
// (exclusive) and targetVersion (inclusive), returning them in ascending order.
func collectMigrationChain(sourceVersion string, targetVersion string) ([]chainedManifest, error) {
	allVersions, err := listMigrationManifestVersions()
	if err != nil {
		return nil, err
	}
	var chain []chainedManifest
	for _, ver := range allVersions {
		cmpSource, cmpErr := compareSemver(ver, sourceVersion)
		if cmpErr != nil {
			return nil, fmt.Errorf("compare migration version %s with source %s: %w", ver, sourceVersion, cmpErr)
		}
		if cmpSource <= 0 {
			continue // skip versions <= source
		}
		cmpTarget, cmpErr := compareSemver(ver, targetVersion)
		if cmpErr != nil {
			return nil, fmt.Errorf("compare migration version %s with target %s: %w", ver, targetVersion, cmpErr)
		}
		if cmpTarget > 0 {
			break // past target
		}
		manifest, manifestPath, loadErr := loadUpgradeMigrationManifestByVersion(ver)
		if loadErr != nil {
			return nil, loadErr
		}
		chain = append(chain, chainedManifest{manifest: manifest, path: manifestPath})
	}
	return chain, nil
}

func validateUpgradeMigrationManifest(manifest upgradeMigrationManifest) error {
	if manifest.SchemaVersion != upgradeMigrationManifestSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", manifest.SchemaVersion)
	}
	if strings.TrimSpace(manifest.TargetVersion) == "" {
		return fmt.Errorf("target_version is required")
	}
	normalizedTarget, err := version.Normalize(manifest.TargetVersion)
	if err != nil {
		return fmt.Errorf("invalid target_version %q: %w", manifest.TargetVersion, err)
	}
	if normalizedTarget != manifest.TargetVersion {
		return fmt.Errorf("target_version %q must be normalized to X.Y.Z", manifest.TargetVersion)
	}
	if strings.TrimSpace(manifest.MinPriorVersion) == "" {
		return fmt.Errorf("min_prior_version is required")
	}
	normalizedMin, err := version.Normalize(manifest.MinPriorVersion)
	if err != nil {
		return fmt.Errorf("invalid min_prior_version %q: %w", manifest.MinPriorVersion, err)
	}
	if normalizedMin != manifest.MinPriorVersion {
		return fmt.Errorf("min_prior_version %q must be normalized to X.Y.Z", manifest.MinPriorVersion)
	}

	seenIDs := make(map[string]struct{}, len(manifest.Operations))
	for _, op := range manifest.Operations {
		if validateErr := validateUpgradeMigrationOperation(op); validateErr != nil {
			return validateErr
		}
		if _, exists := seenIDs[op.ID]; exists {
			return fmt.Errorf("duplicate migration id %q", op.ID)
		}
		seenIDs[op.ID] = struct{}{}
	}
	return nil
}

func validateUpgradeMigrationOperation(op upgradeMigrationOperation) error {
	if strings.TrimSpace(op.ID) == "" {
		return fmt.Errorf("migration id is required")
	}
	if strings.TrimSpace(op.Rationale) == "" {
		return fmt.Errorf("migration %s rationale is required", op.ID)
	}
	switch op.Kind {
	case upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact:
		if strings.TrimSpace(op.From) == "" || strings.TrimSpace(op.To) == "" {
			return fmt.Errorf("migration %s (%s) requires from and to", op.ID, op.Kind)
		}
		if normalizeRelPath(filepath.Clean(filepath.FromSlash(op.From))) == normalizeRelPath(filepath.Clean(filepath.FromSlash(op.To))) {
			return fmt.Errorf("migration %s (%s) requires distinct from/to", op.ID, op.Kind)
		}
	case upgradeMigrationKindDeleteFile, upgradeMigrationKindDeleteGeneratedArtifact:
		if strings.TrimSpace(op.Path) == "" {
			return fmt.Errorf("migration %s (%s) requires path", op.ID, op.Kind)
		}
	case upgradeMigrationKindConfigRenameKey:
		if _, err := splitMigrationKeyPath(op.From); err != nil {
			return fmt.Errorf("migration %s invalid from key: %w", op.ID, err)
		}
		if _, err := splitMigrationKeyPath(op.To); err != nil {
			return fmt.Errorf("migration %s invalid to key: %w", op.ID, err)
		}
	case upgradeMigrationKindConfigSetDefault:
		if _, err := splitMigrationKeyPath(op.Key); err != nil {
			return fmt.Errorf("migration %s invalid key: %w", op.ID, err)
		}
		if len(op.Value) == 0 {
			return fmt.Errorf("migration %s (%s) requires value", op.ID, op.Kind)
		}
		var decoded any
		if err := json.Unmarshal(op.Value, &decoded); err != nil {
			return fmt.Errorf("migration %s (%s) has invalid value: %w", op.ID, op.Kind, err)
		}
	case upgradeMigrationKindMigrateSkillsFormat:
		if strings.TrimSpace(op.Path) == "" {
			return fmt.Errorf("migration %s (%s) requires path", op.ID, op.Kind)
		}
	default:
		return fmt.Errorf("migration %s has unsupported kind %q", op.ID, op.Kind)
	}
	return nil
}

// preflightAndConfirmSkillsMigration runs BEFORE any disk mutations to give the
// user a clear, up-front warning about the breaking skills format change. It
// scans for flat-format skills, detects conflicts, prints the full warning
// banner, and obtains explicit user confirmation. Call this from Run() between
// prepareUpgradeMigrations() and createUpgradeSnapshot().
func (inst *installer) preflightAndConfirmSkillsMigration() error {
	// Only relevant when a migrate_skills_format operation is pending.
	var migrateOp *upgradeMigrationOperation
	for i := range inst.pendingMigrationOps {
		if inst.pendingMigrationOps[i].Kind == upgradeMigrationKindMigrateSkillsFormat {
			migrateOp = &inst.pendingMigrationOps[i]
			break
		}
	}
	if migrateOp == nil {
		return nil
	}

	// Resolve the skills directory. Before migration execution, the directory
	// might still be at the legacy path (.agent-layer/slash-commands/) if the
	// preceding rename operation hasn't run yet. Check both locations.
	postRenamePath, err := snapshotEntryAbsPath(inst.root, filepath.FromSlash(migrateOp.Path))
	if err != nil {
		return err
	}
	absSkillsDir := postRenamePath

	if _, statErr := inst.sys.Stat(absSkillsDir); statErr != nil {
		// Try the legacy pre-rename path.
		legacyPath := filepath.Join(inst.root, ".agent-layer", "slash-commands")
		if _, legacyStatErr := inst.sys.Stat(legacyPath); legacyStatErr == nil {
			absSkillsDir = legacyPath
		} else {
			// Neither directory exists — no skills to migrate.
			return nil
		}
	}

	flatCount, conflicts, preErr := preflightSkillsMigration(inst.sys, absSkillsDir)
	if preErr != nil {
		return preErr
	}
	if flatCount == 0 {
		return nil // all skills already in directory format
	}

	flatSkills, scanErr := listFlatSkillNames(inst.sys, absSkillsDir)
	if scanErr != nil {
		return scanErr
	}

	// ── Warning banner (shown BEFORE any disk mutations) ──
	out := inst.warnOutput()
	ew := &errWriter{w: out}
	ew.println()
	ew.println("=============================================================")
	ew.println("  BREAKING CHANGE: Skills format migration")
	ew.println("=============================================================")
	ew.println()
	ew.println("  Starting with this version, ALL skills must use directory")
	ew.println("  format. The old flat file format (<name>.md) will no longer")
	ew.println("  work after this upgrade.")
	ew.println()
	ew.printf("  Found %d flat-format skill(s) that must be migrated:\n", len(flatSkills))
	ew.println()
	for _, name := range flatSkills {
		ew.printf("    %s.md  ->  %s/SKILL.md\n", name, name)
	}
	if ew.err != nil {
		return ew.err
	}

	if len(conflicts) > 0 {
		ew.println()
		ew.println("  MIGRATION BLOCKED")
		ew.println()
		ew.println("  The following skills exist in BOTH flat and directory format")
		ew.println("  with DIFFERENT content. The migration cannot choose which")
		ew.println("  version to keep — you need to resolve this manually.")
		ew.println()
		for _, c := range conflicts {
			ew.printf("    Skill: %s\n", c.SkillName)
			ew.printf("      Flat file:  %s\n", c.FlatPath)
			ew.printf("      Directory:  %s\n", c.DirPath)
			ew.println()
		}
		ew.println("  To fix: choose which version to keep for each skill above,")
		ew.println("  then delete the other one:")
		ew.println()
		ew.println("    Keep directory version:  rm .agent-layer/skills/<name>.md")
		ew.println("    Keep flat version:       rm -r .agent-layer/skills/<name>/")
		ew.println()
		ew.println("  Then re-run: al upgrade")
		ew.println()
		if ew.err != nil {
			return ew.err
		}
		return fmt.Errorf("skills format migration blocked by %d conflict(s); resolve manually and re-run 'al upgrade'", len(conflicts))
	}

	ew.println()
	ew.println("  No conflicts detected — all skills can be migrated automatically.")
	ew.println()
	if ew.err != nil {
		return ew.err
	}

	// Prompt for confirmation (before any mutations happen).
	if prompter, ok := inst.prompter.(skillsMigrationPrompter); ok {
		proceed, promptErr := prompter.ConfirmSkillsMigration(flatSkills, conflicts)
		if promptErr != nil {
			return fmt.Errorf("skills migration prompt: %w", promptErr)
		}
		if !proceed {
			return fmt.Errorf("skills format migration declined by user")
		}
	}

	inst.skillsMigrationConfirmed = true
	return nil
}

// executeMigrateSkillsFormat migrates all flat-format skills (<name>.md) to
// directory format (<name>/SKILL.md) under relSkillsDir. The user-facing
// warning and confirmation have already been handled by
// preflightAndConfirmSkillsMigration() before any disk mutations began.
func (inst *installer) executeMigrateSkillsFormat(relSkillsDir string) (bool, error) {
	absSkillsDir, err := snapshotEntryAbsPath(inst.root, filepath.FromSlash(relSkillsDir))
	if err != nil {
		return false, err
	}
	if _, statErr := inst.sys.Stat(absSkillsDir); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil // no skills directory — no-op
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, absSkillsDir, statErr)
	}

	flatSkills, scanErr := listFlatSkillNames(inst.sys, absSkillsDir)
	if scanErr != nil {
		return false, scanErr
	}
	if len(flatSkills) == 0 {
		return false, nil // no flat files — no-op
	}

	// Safety check: confirmation must have been obtained during pre-flight.
	// If not (e.g., tests calling executeMigrateSkillsFormat directly), fall
	// back to prompting here.
	if !inst.skillsMigrationConfirmed {
		_, conflicts, preErr := preflightSkillsMigration(inst.sys, absSkillsDir)
		if preErr != nil {
			return false, preErr
		}
		if len(conflicts) > 0 {
			return false, fmt.Errorf("skills format migration blocked by %d conflict(s); resolve manually and re-run 'al upgrade'", len(conflicts))
		}
		if prompter, ok := inst.prompter.(skillsMigrationPrompter); ok {
			proceed, promptErr := prompter.ConfirmSkillsMigration(flatSkills, conflicts)
			if promptErr != nil {
				return false, fmt.Errorf("skills migration prompt: %w", promptErr)
			}
			if !proceed {
				return false, fmt.Errorf("skills format migration declined by user")
			}
		}
	}

	// Execute migration, tracking which skills were actually migrated (moved)
	// vs. duplicates that were just cleaned up.
	changed := false
	var migratedNames []string
	for _, name := range flatSkills {
		flatPath := filepath.Join(absSkillsDir, name+".md")
		destDir := filepath.Join(absSkillsDir, name)
		destPath := filepath.Join(destDir, "SKILL.md")

		// Check if destination already exists (duplicate cleanup case).
		destInfo, destStatErr := inst.sys.Stat(destPath)
		destExisted := destStatErr == nil && !destInfo.IsDir()
		if destStatErr != nil && !errors.Is(destStatErr, os.ErrNotExist) {
			return false, fmt.Errorf(messages.InstallFailedStatFmt, destPath, destStatErr)
		}

		migrated, migErr := migrateSingleFlatSkill(inst.sys, flatPath, destDir, destPath)
		if migErr != nil {
			return false, fmt.Errorf("migrate skill %s: %w", name, migErr)
		}
		if migrated {
			changed = true
			if !destExisted {
				migratedNames = append(migratedNames, name)
			}
		}
	}

	// Print post-migration success summary.
	if changed {
		out := inst.warnOutput()
		ew := &errWriter{w: out}
		ew.println()
		if len(migratedNames) > 0 {
			ew.printf("  Migrated %d skill(s) to directory format:\n", len(migratedNames))
			ew.println()
			for _, name := range migratedNames {
				ew.printf("    %s.md  ->  %s/SKILL.md\n", name, name)
			}
			ew.println()
		}
		ew.println("  Skills migration complete.")
		ew.println()
		if ew.err != nil {
			return false, ew.err
		}
	}

	return changed, nil
}

// preflightSkillsMigration scans absSkillsDir for flat .md files and checks for
// conflicts with existing directory-format skills.
func preflightSkillsMigration(sys System, absSkillsDir string) (flatCount int, conflicts []SkillsMigrationConflict, err error) {
	entries, readErr := readSkillsDirEntries(sys, absSkillsDir)
	if readErr != nil {
		return 0, nil, readErr
	}

	for _, entry := range entries {
		if entry.isDir || !strings.HasSuffix(entry.name, ".md") || strings.HasPrefix(entry.name, ".") {
			continue
		}
		flatCount++
		name := strings.TrimSuffix(entry.name, ".md")
		flatPath := filepath.Join(absSkillsDir, entry.name)
		destPath := filepath.Join(absSkillsDir, name, "SKILL.md")

		destInfo, statErr := sys.Stat(destPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue // no conflict
			}
			return 0, nil, fmt.Errorf(messages.InstallFailedStatFmt, destPath, statErr)
		}
		if destInfo.IsDir() {
			continue
		}

		// Both exist — check content.
		flatData, flatReadErr := sys.ReadFile(flatPath)
		if flatReadErr != nil {
			return 0, nil, fmt.Errorf(messages.InstallFailedReadFmt, flatPath, flatReadErr)
		}
		destData, destReadErr := sys.ReadFile(destPath)
		if destReadErr != nil {
			return 0, nil, fmt.Errorf(messages.InstallFailedReadFmt, destPath, destReadErr)
		}
		if normalizeTemplateContent(string(flatData)) != normalizeTemplateContent(string(destData)) {
			conflicts = append(conflicts, SkillsMigrationConflict{
				SkillName: name,
				FlatPath:  flatPath,
				DirPath:   destPath,
				Reason:    fmt.Sprintf("%s.md and %s/SKILL.md have different content", name, name),
			})
		}
	}
	return flatCount, conflicts, nil
}

// listFlatSkillNames returns sorted names (without .md suffix) of flat-format
// skill files at the root of absSkillsDir.
func listFlatSkillNames(sys System, absSkillsDir string) ([]string, error) {
	entries, err := readSkillsDirEntries(sys, absSkillsDir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.isDir || !strings.HasSuffix(entry.name, ".md") || strings.HasPrefix(entry.name, ".") {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.name, ".md"))
	}
	sort.Strings(names)
	return names, nil
}

// skillsDirEntry mirrors the info needed from a directory scan.
type skillsDirEntry struct {
	name  string
	isDir bool
}

// readSkillsDirEntries performs a shallow directory scan of dir (no recursion).
func readSkillsDirEntries(sys System, dir string) ([]skillsDirEntry, error) {
	info, statErr := sys.Stat(dir)
	if statErr != nil {
		return nil, fmt.Errorf(messages.InstallFailedStatFmt, dir, statErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}

	var entries []skillsDirEntry
	err := sys.WalkDir(dir, func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filepath.Clean(walkPath) == filepath.Clean(dir) {
			return nil
		}
		entries = append(entries, skillsDirEntry{name: d.Name(), isDir: d.IsDir()})
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan skills directory %s: %w", dir, err)
	}
	return entries, nil
}

// migrateSingleFlatSkill moves a flat skill file to directory format. If the
// destination already exists with the same content, the flat file is removed.
func migrateSingleFlatSkill(sys System, flatPath string, destDir string, destPath string) (bool, error) {
	if _, statErr := sys.Stat(flatPath); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(messages.InstallFailedStatFmt, flatPath, statErr)
	}

	destInfo, destStatErr := sys.Stat(destPath)
	if destStatErr != nil && !errors.Is(destStatErr, os.ErrNotExist) {
		return false, fmt.Errorf(messages.InstallFailedStatFmt, destPath, destStatErr)
	}
	if destStatErr == nil && !destInfo.IsDir() {
		// Destination exists — check for same content (duplicate cleanup).
		flatData, readErr := sys.ReadFile(flatPath)
		if readErr != nil {
			return false, fmt.Errorf(messages.InstallFailedReadFmt, flatPath, readErr)
		}
		destData, readErr := sys.ReadFile(destPath)
		if readErr != nil {
			return false, fmt.Errorf(messages.InstallFailedReadFmt, destPath, readErr)
		}
		if normalizeTemplateContent(string(flatData)) == normalizeTemplateContent(string(destData)) {
			// Same content — remove flat file.
			if removeErr := sys.RemoveAll(flatPath); removeErr != nil {
				return false, fmt.Errorf("remove duplicate flat skill %s: %w", flatPath, removeErr)
			}
			return true, nil
		}
		// Different content should have been caught by preflight.
		return false, fmt.Errorf("conflict: %s and %s have different content", flatPath, destPath)
	}

	// Create destination directory and move.
	if mkErr := sys.MkdirAll(destDir, 0o755); mkErr != nil {
		return false, fmt.Errorf(messages.InstallFailedCreateDirForFmt, destPath, mkErr)
	}
	if renameErr := sys.Rename(flatPath, destPath); renameErr != nil {
		return false, fmt.Errorf("rename %s -> %s: %w", flatPath, destPath, renameErr)
	}
	return true, nil
}
