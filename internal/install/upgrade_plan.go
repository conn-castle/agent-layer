package install

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	// UpgradeRenameConfidenceHigh is emitted when rename detection is a unique exact content match.
	UpgradeRenameConfidenceHigh = "high"
	// UpgradeRenameDetectionUniqueExactHash identifies rename detection by unique exact normalized hash.
	UpgradeRenameDetectionUniqueExactHash = "unique_exact_normalized_hash"
	// UpgradePlanSchemaVersion is the JSON schema version for `al upgrade plan` output.
	UpgradePlanSchemaVersion = 1
)

// UpgradePinAction identifies the pin transition kind in an upgrade plan.
type UpgradePinAction string

const (
	// UpgradePinActionNone means the current pin already matches the target pin.
	UpgradePinActionNone UpgradePinAction = "none"
	// UpgradePinActionSet means the repo currently has no pin and the plan sets one.
	UpgradePinActionSet UpgradePinAction = "set"
	// UpgradePinActionUpdate means the repo pin changes from one value to another.
	UpgradePinActionUpdate UpgradePinAction = "update"
	// UpgradePinActionRemove means the repo pin is removed for the target.
	UpgradePinActionRemove UpgradePinAction = "remove"
)

// UpgradePlanOptions controls dry-run plan generation.
type UpgradePlanOptions struct {
	TargetPinVersion string
	System           System
}

// UpgradePlan is the machine-readable output of `al upgrade plan`.
type UpgradePlan struct {
	SchemaVersion             int                     `json:"schema_version"`
	DryRun                    bool                    `json:"dry_run"`
	TemplateAdditions         []UpgradeChange         `json:"template_additions"`
	TemplateUpdates           []UpgradeChange         `json:"template_updates"`
	SectionAwareUpdates       []UpgradeChange         `json:"section_aware_updates"`
	TemplateRenames           []UpgradeRename         `json:"template_renames"`
	TemplateRemovalsOrOrphans []UpgradeChange         `json:"template_removals_or_orphans"`
	ConfigKeyMigrations       []ConfigKeyMigration    `json:"config_key_migrations"`
	PinVersionChange          UpgradePinVersionDiff   `json:"pin_version_change"`
	ReadinessChecks           []UpgradeReadinessCheck `json:"readiness_checks"`
}

// UpgradeChange describes a single template delta entry.
type UpgradeChange struct {
	Path                    string               `json:"path"`
	Ownership               OwnershipLabel       `json:"ownership"`
	OwnershipState          OwnershipState       `json:"ownership_state"`
	OwnershipConfidence     *OwnershipConfidence `json:"ownership_confidence,omitempty"`
	OwnershipBaselineSource *BaselineStateSource `json:"ownership_baseline_source,omitempty"`
	OwnershipReasonCodes    []string             `json:"ownership_reason_codes,omitempty"`
}

// UpgradeRename describes a rename detected by the dry-run planner.
type UpgradeRename struct {
	From                    string               `json:"from"`
	To                      string               `json:"to"`
	Ownership               OwnershipLabel       `json:"ownership"`
	OwnershipState          OwnershipState       `json:"ownership_state"`
	OwnershipConfidence     *OwnershipConfidence `json:"ownership_confidence,omitempty"`
	OwnershipBaselineSource *BaselineStateSource `json:"ownership_baseline_source,omitempty"`
	OwnershipReasonCodes    []string             `json:"ownership_reason_codes,omitempty"`
	Confidence              string               `json:"confidence"`
	Detection               string               `json:"detection"`
}

// ConfigKeyMigration is reserved for explicit config migrations.
type ConfigKeyMigration struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

// UpgradePinVersionDiff captures current->target pin movement for upgrade planning.
type UpgradePinVersionDiff struct {
	Current string           `json:"current"`
	Target  string           `json:"target"`
	Action  UpgradePinAction `json:"action"`
}

type templatedPath struct {
	relPath      string
	templatePath string
}

type upgradeChangeWithTemplate struct {
	path         string
	templatePath string
	ownership    ownershipClassification
}

// BuildUpgradePlan computes a dry-run upgrade plan against the running binary's embedded templates.
func BuildUpgradePlan(root string, opts UpgradePlanOptions) (UpgradePlan, error) {
	if root == "" {
		return UpgradePlan{}, fmt.Errorf(messages.InstallRootRequired)
	}
	if opts.System == nil {
		return UpgradePlan{}, fmt.Errorf(messages.InstallSystemRequired)
	}

	targetPinVersion := strings.TrimSpace(opts.TargetPinVersion)
	if targetPinVersion != "" {
		normalized, err := version.Normalize(targetPinVersion)
		if err != nil {
			return UpgradePlan{}, fmt.Errorf(messages.InstallInvalidPinVersionFmt, err)
		}
		targetPinVersion = normalized
	}

	inst := &installer{
		root:       root,
		pinVersion: targetPinVersion,
		sys:        opts.System,
	}

	templateEntries, err := inst.currentTemplateEntries()
	if err != nil {
		return UpgradePlan{}, err
	}

	additions := make([]upgradeChangeWithTemplate, 0)
	updates := make([]upgradeChangeWithTemplate, 0)
	for _, entry := range templateEntries {
		absPath := filepath.Join(root, filepath.FromSlash(entry.relPath))
		info, err := inst.sys.Stat(absPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				additions = append(additions, upgradeChangeWithTemplate{
					path:         entry.relPath,
					templatePath: entry.templatePath,
					ownership: ownershipClassification{
						Label: OwnershipUpstreamTemplateDelta,
						State: OwnershipStateUpstreamTemplateDelta,
					},
				})
				continue
			}
			return UpgradePlan{}, fmt.Errorf(messages.InstallFailedStatFmt, absPath, err)
		}
		matches, err := inst.matchTemplate(inst.sys, absPath, entry.templatePath, info)
		if err != nil {
			return UpgradePlan{}, err
		}
		if matches {
			continue
		}
		// For section-aware files, only the managed section (above the marker)
		// determines upgrade eligibility. User entries below the marker are
		// expected to differ and do not require an upgrade action.
		if sectionMatch, sErr := inst.sectionAwareTemplateMatch(entry.relPath, absPath, entry.templatePath); sErr == nil && sectionMatch {
			continue
		}
		ownership, err := inst.classifyOwnershipDetail(entry.relPath, entry.templatePath)
		if err != nil {
			return UpgradePlan{}, err
		}
		updates = append(updates, upgradeChangeWithTemplate{
			path:         entry.relPath,
			templatePath: entry.templatePath,
			ownership:    ownership,
		})
	}

	orphans, err := inst.templateOrphans(templateEntries)
	if err != nil {
		return UpgradePlan{}, err
	}

	renames, additions, orphans, err := detectUpgradeRenames(inst, additions, orphans)
	if err != nil {
		return UpgradePlan{}, err
	}

	pinDiff, err := inst.pinVersionDiff()
	if err != nil {
		return UpgradePlan{}, err
	}

	regularUpdates, sectionUpdates := splitSectionAwareUpdates(updates)
	readinessChecks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		return UpgradePlan{}, err
	}

	return UpgradePlan{
		SchemaVersion:             UpgradePlanSchemaVersion,
		DryRun:                    true,
		TemplateAdditions:         toUpgradeChanges(additions),
		TemplateUpdates:           toUpgradeChanges(regularUpdates),
		SectionAwareUpdates:       toUpgradeChanges(sectionUpdates),
		TemplateRenames:           renames,
		TemplateRemovalsOrOrphans: toUpgradeChanges(orphans),
		ConfigKeyMigrations:       []ConfigKeyMigration{},
		PinVersionChange:          pinDiff,
		ReadinessChecks:           readinessChecks,
	}, nil
}

func toUpgradeChanges(changes []upgradeChangeWithTemplate) []UpgradeChange {
	if len(changes) == 0 {
		return []UpgradeChange{}
	}
	out := make([]UpgradeChange, 0, len(changes))
	for _, change := range changes {
		out = append(out, UpgradeChange{
			Path:                    change.path,
			Ownership:               change.ownership.Label,
			OwnershipState:          change.ownership.State,
			OwnershipConfidence:     change.ownership.Confidence,
			OwnershipBaselineSource: change.ownership.BaselineSource,
			OwnershipReasonCodes:    change.ownership.ReasonCodes,
		})
	}
	return out
}

func (inst *installer) currentTemplateEntries() ([]templatedPath, error) {
	files := inst.managedTemplateFiles()
	entries := make([]templatedPath, 0, len(files))
	for _, file := range files {
		entries = append(entries, templatedPath{
			relPath:      filepath.ToSlash(inst.relativePath(file.path)),
			templatePath: file.template,
		})
	}

	allDirs := inst.allTemplateDirs()
	for _, dir := range allDirs {
		dirEntries, err := inst.templateDirEntries(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range dirEntries {
			entries = append(entries, templatedPath{
				relPath:      filepath.ToSlash(inst.relativePath(entry.destPath)),
				templatePath: entry.templatePath,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})
	return entries, nil
}

func (inst *installer) templateOrphans(templateEntries []templatedPath) ([]upgradeChangeWithTemplate, error) {
	templatePaths := make(map[string]struct{}, len(templateEntries))
	for _, entry := range templateEntries {
		templatePaths[entry.relPath] = struct{}{}
	}

	managedRoots := []string{
		filepath.Join(inst.root, ".agent-layer", "instructions"),
		filepath.Join(inst.root, ".agent-layer", "slash-commands"),
		filepath.Join(inst.root, ".agent-layer", "templates", "docs"),
		filepath.Join(inst.root, "docs", "agent-layer"),
	}
	orphanSet := make(map[string]struct{})
	for _, root := range managedRoots {
		if err := inst.walkTemplateOrphans(root, templatePaths, orphanSet); err != nil {
			return nil, err
		}
	}

	orphans := make([]upgradeChangeWithTemplate, 0, len(orphanSet))
	for relPath := range orphanSet {
		ownership, err := inst.classifyOrphanOwnershipDetail(relPath)
		if err != nil {
			return nil, err
		}
		orphans = append(orphans, upgradeChangeWithTemplate{
			path:      relPath,
			ownership: ownership,
		})
	}
	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].path < orphans[j].path
	})
	return orphans, nil
}

func (inst *installer) walkTemplateOrphans(root string, templatePaths map[string]struct{}, orphanSet map[string]struct{}) error {
	if _, err := inst.sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, root, err)
	}

	return inst.sys.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(inst.relativePath(path))
		if _, ok := templatePaths[rel]; ok {
			return nil
		}
		orphanSet[rel] = struct{}{}
		return nil
	})
}

func detectUpgradeRenames(
	inst *installer,
	additions []upgradeChangeWithTemplate,
	orphans []upgradeChangeWithTemplate,
) ([]UpgradeRename, []upgradeChangeWithTemplate, []upgradeChangeWithTemplate, error) {
	if len(additions) == 0 || len(orphans) == 0 {
		return []UpgradeRename{}, additions, orphans, nil
	}

	additionsByHash := make(map[string][]int)
	for idx, addition := range additions {
		templateBytes, err := templates.Read(addition.templatePath)
		if err != nil {
			return nil, nil, nil, err
		}
		hash := hashNormalizedContent(templateBytes)
		additionsByHash[hash] = append(additionsByHash[hash], idx)
	}

	orphansByHash := make(map[string][]int)
	for idx, orphan := range orphans {
		path := filepath.Join(inst.root, filepath.FromSlash(orphan.path))
		data, err := inst.sys.ReadFile(path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
		}
		hash := hashNormalizedContent(data)
		orphansByHash[hash] = append(orphansByHash[hash], idx)
	}

	usedAdditions := make(map[int]struct{})
	usedOrphans := make(map[int]struct{})
	renames := make([]UpgradeRename, 0)
	for hash, additionIndexes := range additionsByHash {
		orphanIndexes := orphansByHash[hash]
		if len(additionIndexes) != 1 || len(orphanIndexes) != 1 {
			continue
		}
		addIdx := additionIndexes[0]
		orphanIdx := orphanIndexes[0]
		addition, ok := upgradeChangeAt(additions, addIdx)
		if !ok {
			continue
		}
		orphan, ok := upgradeChangeAt(orphans, orphanIdx)
		if !ok {
			continue
		}
		usedAdditions[addIdx] = struct{}{}
		usedOrphans[orphanIdx] = struct{}{}
		renames = append(renames, UpgradeRename{
			From:           orphan.path,
			To:             addition.path,
			Ownership:      OwnershipUpstreamTemplateDelta,
			OwnershipState: OwnershipStateUpstreamTemplateDelta,
			Confidence:     UpgradeRenameConfidenceHigh,
			Detection:      UpgradeRenameDetectionUniqueExactHash,
		})
	}

	sort.Slice(renames, func(i, j int) bool {
		if renames[i].From == renames[j].From {
			return renames[i].To < renames[j].To
		}
		return renames[i].From < renames[j].From
	})

	filteredAdditions := make([]upgradeChangeWithTemplate, 0, len(additions)-len(usedAdditions))
	for idx, addition := range additions {
		if _, used := usedAdditions[idx]; used {
			continue
		}
		filteredAdditions = append(filteredAdditions, addition)
	}
	filteredOrphans := make([]upgradeChangeWithTemplate, 0, len(orphans)-len(usedOrphans))
	for idx, orphan := range orphans {
		if _, used := usedOrphans[idx]; used {
			continue
		}
		filteredOrphans = append(filteredOrphans, orphan)
	}

	return renames, filteredAdditions, filteredOrphans, nil
}

// sectionAwareTemplateMatch returns true when a file has a section-aware policy
// and the managed section (above the marker) matches the template. User entries
// below the marker are expected to differ and are not considered for matching.
// Returns (false, nil) for non-section-aware files so the caller falls through
// to the standard full-content comparison path.
func (inst *installer) sectionAwareTemplateMatch(relPath string, absPath string, templatePath string) (bool, error) {
	policy := ownershipPolicyForPath(relPath)
	if policy != ownershipPolicyMemoryEntries && policy != ownershipPolicyMemoryRoadmap {
		return false, nil
	}
	localBytes, err := inst.sys.ReadFile(absPath)
	if err != nil {
		return false, err
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return false, err
	}

	parseComparable := func(content []byte) (ownershipComparable, bool) {
		comp, _, err := classifyComparable(relPath, content)
		if err != nil {
			return ownershipComparable{}, false
		}
		return comp, true
	}

	localComp, ok := parseComparable(localBytes)
	if !ok {
		return false, nil // parse error; fall through to full classification
	}
	targetComp, ok := parseComparable(templateBytes)
	if !ok {
		return false, nil
	}
	return comparableKey(localComp) == comparableKey(targetComp), nil
}

// splitSectionAwareUpdates partitions updates into regular template updates and
// section-aware updates (memory files with marker-based managed/user sections).
func splitSectionAwareUpdates(updates []upgradeChangeWithTemplate) (regular []upgradeChangeWithTemplate, sectionAware []upgradeChangeWithTemplate) {
	regular = make([]upgradeChangeWithTemplate, 0, len(updates))
	sectionAware = make([]upgradeChangeWithTemplate, 0)
	for _, u := range updates {
		policy := ownershipPolicyForPath(u.path)
		if policy == ownershipPolicyMemoryEntries || policy == ownershipPolicyMemoryRoadmap {
			sectionAware = append(sectionAware, u)
		} else {
			regular = append(regular, u)
		}
	}
	return regular, sectionAware
}

func upgradeChangeAt(changes []upgradeChangeWithTemplate, idx int) (upgradeChangeWithTemplate, bool) {
	if idx < 0 || idx >= len(changes) {
		return upgradeChangeWithTemplate{}, false
	}
	return changes[idx], true
}

func (inst *installer) pinVersionDiff() (UpgradePinVersionDiff, error) {
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	data, err := inst.sys.ReadFile(path)
	current := ""
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return UpgradePinVersionDiff{}, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
		}
	} else {
		current = strings.TrimSpace(string(data))
		if current != "" {
			normalized, normalizeErr := version.Normalize(current)
			if normalizeErr == nil {
				current = normalized
			}
		}
	}

	target := inst.pinVersion
	switch {
	case current == target:
		return UpgradePinVersionDiff{Current: current, Target: target, Action: UpgradePinActionNone}, nil
	case current == "" && target != "":
		return UpgradePinVersionDiff{Current: current, Target: target, Action: UpgradePinActionSet}, nil
	case current != "" && target == "":
		return UpgradePinVersionDiff{Current: current, Target: target, Action: UpgradePinActionRemove}, nil
	default:
		return UpgradePinVersionDiff{Current: current, Target: target, Action: UpgradePinActionUpdate}, nil
	}
}

func hashNormalizedContent(content []byte) string {
	normalized := normalizeTemplateContent(string(content))
	sum := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", sum[:])
}
