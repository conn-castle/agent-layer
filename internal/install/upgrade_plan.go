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
	StatuslineSourceAdditions []UpgradeChange         `json:"statusline_source_additions"`
	StatuslineSourceUpdates   []UpgradeChange         `json:"statusline_source_updates"`
	SectionAwareUpdates       []UpgradeChange         `json:"section_aware_updates"`
	TemplateRenames           []UpgradeRename         `json:"template_renames"`
	TemplateRemovalsOrOrphans []UpgradeChange         `json:"template_removals_or_orphans"`
	ConfigKeyMigrations       []ConfigKeyMigration    `json:"config_key_migrations"`
	MigrationReport           UpgradeMigrationReport  `json:"migration_report"`
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
	migrationPlan, err := inst.planUpgradeMigrations()
	if err != nil {
		return UpgradePlan{}, err
	}

	templateEntries, err := inst.templates().currentTemplateEntries()
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
		update, changed, err := inst.templateUpdate(entry.relPath, absPath, entry.templatePath, info)
		if err != nil {
			return UpgradePlan{}, err
		}
		if changed {
			updates = append(updates, update)
		}
	}

	orphans, err := inst.templates().templateOrphans(templateEntries)
	if err != nil {
		return UpgradePlan{}, err
	}
	additions = filterCoveredUpgradeChanges(additions, migrationPlan.coveredPaths)
	updates = filterCoveredUpgradeChanges(updates, migrationPlan.coveredPaths)
	orphans = filterCoveredUpgradeChanges(orphans, migrationPlan.coveredPaths)
	additions, updates, err = inst.movedFileUpdates(migrationPlan, additions, updates)
	if err != nil {
		return UpgradePlan{}, err
	}
	kept, err := inst.loadUpgradeKeepList()
	if err != nil {
		return UpgradePlan{}, err
	}
	orphans = filterKeptUpgradeChanges(orphans, kept)

	renames, additions, orphans, err := detectUpgradeRenames(inst, additions, orphans)
	if err != nil {
		return UpgradePlan{}, err
	}
	unknownDeletions, err := inst.planUnknownDeletions(orphans, renames, migrationPlan.executable, kept)
	if err != nil {
		return UpgradePlan{}, err
	}
	orphans = append(orphans, unknownDeletions...)
	sort.Slice(orphans, func(i, j int) bool {
		return orphans[i].path < orphans[j].path
	})
	statuslineAdditions, statuslineUpdates, err := inst.planStatuslineSourceChanges(migrationPlan)
	if err != nil {
		return UpgradePlan{}, err
	}

	pinDiff, err := inst.templates().pinVersionDiff()
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
		StatuslineSourceAdditions: toUpgradeChanges(statuslineAdditions),
		StatuslineSourceUpdates:   toUpgradeChanges(statuslineUpdates),
		SectionAwareUpdates:       toUpgradeChanges(sectionUpdates),
		TemplateRenames:           renames,
		TemplateRemovalsOrOrphans: toUpgradeChanges(orphans),
		ConfigKeyMigrations:       migrationPlan.configMigrations,
		MigrationReport:           migrationPlan.report,
		PinVersionChange:          pinDiff,
		ReadinessChecks:           readinessChecks,
	}, nil
}

// templateUpdate reports whether the file at absPath differs from relPath's
// template and classifies that difference against relPath's baseline.
func (inst *installer) templateUpdate(relPath, absPath, templatePath string, info fs.FileInfo) (upgradeChangeWithTemplate, bool, error) {
	matches, err := inst.templates().matchTemplate(inst.sys, absPath, templatePath, info)
	if err != nil || matches {
		return upgradeChangeWithTemplate{}, false, err
	}
	// For section-aware files, only the managed section (above the marker)
	// determines upgrade eligibility. User entries below the marker are
	// expected to differ and do not require an upgrade action.
	if sectionMatch, sErr := inst.templates().sectionAwareTemplateMatch(relPath, absPath, templatePath); sErr == nil && sectionMatch {
		return upgradeChangeWithTemplate{}, false, nil
	}
	localBytes, err := inst.sys.ReadFile(absPath)
	if err != nil {
		return upgradeChangeWithTemplate{}, false, err
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return upgradeChangeWithTemplate{}, false, err
	}
	ownership, err := inst.ownership().classifyAgainstBaseline(relPath, localBytes, templateBytes, false)
	if err != nil {
		return upgradeChangeWithTemplate{}, false, err
	}
	return upgradeChangeWithTemplate{path: relPath, templatePath: templatePath, ownership: ownership}, true, nil
}

// movedFileUpdates compares post-migration destinations with their current bytes.
func (inst *installer) movedFileUpdates(plan migrationPlan, additions, updates []upgradeChangeWithTemplate) ([]upgradeChangeWithTemplate, []upgradeChangeWithTemplate, error) {
	if !hasRenameMigration(plan.executable) {
		return additions, updates, nil
	}
	_, origins, err := inst.pathsAfterMigrations(plan.executable)
	if err != nil {
		return nil, nil, err
	}
	templatePaths, err := inst.templates().ungatedTemplatePathByRel()
	if err != nil {
		return nil, nil, err
	}
	renameOnlyCovered := make(map[string]struct{})
	for path := range plan.coveredPaths {
		if _, covered := plan.postMigrationCoveredPaths[path]; !covered {
			renameOnlyCovered[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(origins))
	for path := range origins {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	removePath := func(changes []upgradeChangeWithTemplate, path string) []upgradeChangeWithTemplate {
		out := changes[:0]
		for _, change := range changes {
			if change.path != path {
				out = append(out, change)
			}
		}
		return out
	}
	for _, path := range paths {
		origin := origins[path]
		templatePath := templatePaths[path]
		if templatePath == "" || isCoveredByMigration(path, plan.postMigrationCoveredPaths) {
			continue
		}
		if origin == filepath.Join(inst.root, filepath.FromSlash(path)) && !isCoveredByMigration(path, renameOnlyCovered) {
			continue
		}
		// Stat follows symlinks, as apply does when it diffs the moved file.
		info, err := inst.sys.Stat(origin)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf(messages.InstallFailedStatFmt, origin, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		additions = removePath(additions, path)
		updates = removePath(updates, path)
		update, changed, err := inst.templateUpdate(path, origin, templatePath, info)
		if err != nil {
			return nil, nil, err
		}
		if changed {
			updates = append(updates, update)
		}
	}
	sort.Slice(updates, func(i, j int) bool { return updates[i].path < updates[j].path })
	return additions, updates, nil
}

// planUnknownDeletions mirrors the non-tmp --apply-deletions set, skipping
// paths already represented by template orphans or renames. Apply scans for
// unknown paths after migrations run, so the plan classifies the paths the
// planned migrations will leave, using the rules of walkUnknownsInRoot.
func (inst *installer) planUnknownDeletions(existing []upgradeChangeWithTemplate, renames []UpgradeRename, ops []upgradeMigrationOperation, kept upgradeKeepList) ([]upgradeChangeWithTemplate, error) {
	known, err := inst.buildKnownPaths()
	if err != nil {
		return nil, err
	}
	paths, _, err := inst.pathsAfterMigrations(ops)
	if err != nil {
		return nil, err
	}
	represented := make(map[string]struct{})
	addRepresented := func(path string) {
		for path != "." && path != "" {
			represented[filepath.ToSlash(path)] = struct{}{}
			path = filepath.Dir(path)
		}
	}
	for _, change := range existing {
		addRepresented(change.path)
	}
	for _, rename := range renames {
		addRepresented(rename.From)
	}
	isKnown := func(rel string) bool {
		_, ok := known[filepath.Join(inst.root, filepath.FromSlash(rel))]
		return ok
	}
	units := make(map[string]struct{})
	for rel, isDir := range paths {
		if unit, ok := unknownDeletionUnit(rel, isDir, isKnown, kept); ok {
			if _, skip := represented[unit]; !skip {
				units[unit] = struct{}{}
			}
		}
	}
	changes := make([]upgradeChangeWithTemplate, 0, len(units))
	for unit := range units {
		ownership, err := inst.classifyRemovalOwnership(unit)
		if err != nil {
			return nil, err
		}
		changes = append(changes, upgradeChangeWithTemplate{path: unit, ownership: ownership})
	}
	return changes, nil
}

// unknownDeletionUnit returns the path walkUnknownsInRoot would report for rel:
// its first ancestor (or rel itself) that is neither known nor a directory
// holding a kept descendant. It reports nothing for kept paths.
func unknownDeletionUnit(rel string, isDir bool, isKnown func(string) bool, kept upgradeKeepList) (string, bool) {
	parts := strings.Split(rel, "/")
	rootParts := 0
	switch {
	case strings.HasPrefix(rel, ".agent-layer/"):
		rootParts = 1
	case strings.HasPrefix(rel, docsAgentLayerDir+"/"):
		rootParts = len(strings.Split(docsAgentLayerDir, "/"))
	default:
		return "", false
	}
	for i := rootParts + 1; i <= len(parts); i++ {
		path := strings.Join(parts[:i], "/")
		if isKnown(path) {
			continue
		}
		if upgradePathIsKept(path, kept) {
			return "", false
		}
		if (i < len(parts) || isDir) && upgradePathHasKeptDescendant(path, kept) {
			continue
		}
		return path, true
	}
	return "", false
}

// pathsAfterMigrations lists every path under the unknown-scan roots, except
// .agent-layer/tmp, as it will exist after ops run in order. The value reports
// whether the path is a directory; origins maps paths to their current absolute
// location. It models only the file effects that can change which paths are
// unknown; a rename onto an occupied destination, which fails the upgrade, is
// modelled as a merge.
func (inst *installer) pathsAfterMigrations(ops []upgradeMigrationOperation) (map[string]bool, map[string]string, error) {
	paths := make(map[string]bool)
	// Chained renames must stat the original path while planning leaves the tree untouched.
	origins := make(map[string]string)
	for _, root := range inst.unknownScanRoots() {
		if _, err := inst.sys.Stat(root); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, fmt.Errorf(messages.InstallFailedStatFmt, root, err)
		}
		err := inst.sys.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel := filepath.ToSlash(inst.relativePath(path))
			if rel == agentLayerTmpKeepPath {
				return filepath.SkipDir
			}
			paths[rel] = entry.IsDir()
			origins[rel] = path
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	under := func(path, root string) (string, bool) {
		if path == root {
			return "", true
		}
		rest, ok := strings.CutPrefix(path, root+"/")
		return "/" + rest, ok
	}
	clean := func(path string) string {
		return normalizeRelPath(filepath.Clean(filepath.FromSlash(path)))
	}
	for _, op := range ops {
		switch op.Kind {
		case upgradeMigrationKindDeleteFile:
			target := clean(op.Path)
			for path := range paths {
				if _, ok := under(path, target); ok {
					delete(paths, path)
					delete(origins, path)
				}
			}
		case upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact:
			from, to := clean(op.From), clean(op.To)
			if _, ok := paths[from]; !ok || from == to {
				continue
			}
			if origin := origins[from]; origin != "" {
				if _, err := inst.sys.Stat(origin); err != nil {
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					return nil, nil, fmt.Errorf(messages.InstallFailedStatFmt, origin, err)
				}
			}
			moved := make(map[string]bool)
			movedOrigins := make(map[string]string)
			for path, isDir := range paths {
				if rest, ok := under(path, from); ok {
					moved[to+rest] = isDir
					movedOrigins[to+rest] = origins[path]
					delete(paths, path)
					delete(origins, path)
				}
			}
			for path, isDir := range moved {
				paths[path] = isDir
				origins[path] = movedOrigins[path]
			}
		case upgradeMigrationKindMigrateSkillsFormat:
			dir := clean(op.Path)
			for path, isDir := range paths {
				name, ok := strings.CutPrefix(path, dir+"/")
				if !ok || isDir || strings.Contains(name, "/") || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
					continue
				}
				delete(paths, path)
				delete(origins, path)
				skillDir := dir + "/" + strings.TrimSuffix(name, ".md")
				paths[skillDir] = true
				paths[skillDir+"/"+skillManifestFileName] = false
				delete(origins, skillDir)
				delete(origins, skillDir+"/"+skillManifestFileName)
			}
		case upgradeMigrationKindAppendToFile:
			target := clean(op.Path)
			if _, ok := paths[target]; !ok {
				paths[target] = false
			}
		}
	}
	return paths, origins, nil
}

func (inst *installer) classifyRemovalOwnership(rel string) (ownershipClassification, error) {
	path := filepath.Join(inst.root, filepath.FromSlash(rel))
	unknown := unknownOwnershipClassification(nil, []string{ownershipReasonBaselineMissing})
	info, err := inst.sys.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		// A path a migration has yet to create has no content to classify.
		return unknown, nil
	}
	if err != nil {
		return ownershipClassification{}, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	if !info.Mode().IsRegular() {
		return unknown, nil
	}
	return inst.ownership().classifyOrphanOwnershipDetail(rel)
}

func filterCoveredUpgradeChanges(
	changes []upgradeChangeWithTemplate,
	covered map[string]struct{},
) []upgradeChangeWithTemplate {
	if len(changes) == 0 || len(covered) == 0 {
		return changes
	}
	filtered := make([]upgradeChangeWithTemplate, 0, len(changes))
	for _, change := range changes {
		if isCoveredByMigration(change.path, covered) {
			continue
		}
		filtered = append(filtered, change)
	}
	return filtered
}

// isCoveredByMigration returns true when path or any of its ancestor
// directories appears in the covered set.
func isCoveredByMigration(path string, covered map[string]struct{}) bool {
	normalized := normalizeRelPath(path)
	if _, ok := covered[normalized]; ok {
		return true
	}
	for dir := filepath.Dir(normalized); dir != "." && dir != ""; dir = filepath.Dir(dir) {
		if _, ok := covered[dir]; ok {
			return true
		}
	}
	return false
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

func (inst templateManager) currentTemplateEntries() ([]templatedPath, error) {
	files := inst.managedTemplateFiles()
	entries := make([]templatedPath, 0, len(files))
	for _, file := range files {
		entries = append(entries, templatedPath{
			relPath:      filepath.ToSlash(inst.relativePath(file.path)),
			templatePath: file.template,
		})
	}

	allDirs, err := inst.activeAllTemplateDirs()
	if err != nil {
		return nil, err
	}
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

func (inst templateManager) templateOrphans(templateEntries []templatedPath) ([]upgradeChangeWithTemplate, error) {
	templatePaths := make(map[string]struct{}, len(templateEntries))
	for _, entry := range templateEntries {
		templatePaths[entry.relPath] = struct{}{}
	}

	dirs, err := inst.activeAllTemplateDirs()
	if err != nil {
		return nil, err
	}
	managedRoots := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		managedRoots = append(managedRoots, dir.destRoot)
	}
	orphanSet := make(map[string]struct{})
	for _, root := range managedRoots {
		if err := inst.walkTemplateOrphans(root, templatePaths, orphanSet); err != nil {
			return nil, err
		}
	}

	orphans := make([]upgradeChangeWithTemplate, 0, len(orphanSet))
	for relPath := range orphanSet {
		ownership, err := inst.classifyRemovalOwnership(relPath)
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

func (inst templateManager) walkTemplateOrphans(root string, templatePaths map[string]struct{}, orphanSet map[string]struct{}) error {
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
		info, err := inst.sys.Lstat(path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
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
func (inst templateManager) sectionAwareTemplateMatch(relPath string, absPath string, templatePath string) (bool, error) {
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

func (inst templateManager) pinVersionDiff() (UpgradePinVersionDiff, error) {
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	data, err := inst.sys.ReadFile(path)
	current := ""
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return UpgradePinVersionDiff{}, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
		}
	} else {
		// A valid pin (or one with no version line) reports its parsed version;
		// an invalid pin keeps its raw text so the plan shows what is replaced.
		current = strings.TrimSpace(string(data))
		if normalized, _, parseErr := version.ParsePin(data); parseErr == nil {
			current = normalized
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
