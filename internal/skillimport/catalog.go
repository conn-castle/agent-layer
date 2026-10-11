package skillimport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projectlock"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// CatalogMember describes offline evidence for one exact catalog path.
// Configured missing content remains the responsibility of ordinary import commands.
type CatalogMember struct {
	Name         string
	Selector     string
	Imported     bool
	Legacy       bool
	Configured   bool
	DefaultExact bool
	// AddBlocked explains why an uncovered selector cannot be added by the wizard.
	AddBlocked string
	Problem    string
	// ForeignOwned distinguishes a valid configured import from another source.
	// It still blocks canonical catalog edits, but is not an ownership defect.
	ForeignOwned bool
}

const catalogManualBlockMessage = "the default-policy import block is manually managed; the wizard cannot add or remove its selectors; use al skills add/remove/pull explicitly with the existing policy"

// catalogBlockEditable applies eligibility to the entire block, not just the
// selector being edited. Wildcards and exclusions make a block manually managed.
func catalogBlockEditable(block config.SkillImport, repository string) bool {
	if block.Identity() != (config.SkillImport{Repository: repository}).Identity() {
		return false
	}
	for _, selector := range block.Selectors {
		if config.IsSkillExclusionSelector(selector) || strings.ContainsAny(config.NormalizeSkillSelector(selector), "*?[") {
			return false
		}
	}
	return true
}

// requireCatalogPolicy is called on freshly recovered state under the project lock.
func requireCatalogPolicy(st *state) error {
	identity := (config.SkillImport{Repository: templates.GeneralSkillsRepository}).Identity()
	if block, _, ok := findBlockByIdentity(st.cfg, identity); ok && !catalogBlockEditable(block, identity.Repository) {
		return errors.New(catalogManualBlockMessage)
	}
	return nil
}

// CatalogState recovers interrupted imports under the project lock before reading
// local config, lock, and content for wizard edits. It never fetches. Missing
// config is allowed for install previews; malformed lock evidence is fatal.
func CatalogState(root string, entry templates.CLISkillCatalogEntry) ([]CatalogMember, error) {
	return catalogState(root, entry, true)
}

// ErrPendingRecovery prevents diagnostics from reading a transitional generation.
var ErrPendingRecovery = errors.New("skill import pending recovery")

// InspectCatalogState reads stable local evidence without fetching or recovering.
// Any staging node requires an explicit sync before diagnostic inspection.
func InspectCatalogState(root string, entry templates.CLISkillCatalogEntry) ([]CatalogMember, error) {
	return catalogState(root, entry, false)
}

func catalogState(root string, entry templates.CLISkillCatalogEntry, recoverImports bool) ([]CatalogMember, error) {
	if _, err := os.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return readCatalogState(root, entry)
	} else if err != nil {
		return nil, err
	}
	var members []CatalogMember
	err := projectlock.With(sync.RealSystem{}, root, func() error {
		paths := config.DefaultPaths(root)
		if err := validateTierRoots(paths); err != nil {
			// Wizard previews diagnose bad roots per member without recovery.
			if !recoverImports {
				return err
			}
		} else if recoverImports {
			if err := sync.RecoverInterruptedImport(root); err != nil {
				return err
			}
		} else {
			stage := skilljournal.StagingRoot(paths.ImportedSkillsDir)
			if _, err := os.Lstat(stage); err == nil {
				return fmt.Errorf("%w in %s; preserve live data and staging evidence and run al sync", ErrPendingRecovery, stage)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("cannot inspect staging %s; preserve recovery evidence: %w", stage, err)
			}
		}
		var err error
		members, err = readCatalogState(root, entry)
		return err
	})
	return members, err
}

// readCatalogState requires a stable generation and the project lock when installed.
func readCatalogState(root string, entry templates.CLISkillCatalogEntry) ([]CatalogMember, error) {
	paths := config.DefaultPaths(root)
	cfg := &config.Config{}
	raw, err := os.ReadFile(paths.ConfigPath)
	if err == nil {
		cfg, err = config.ParseConfig(raw, paths.ConfigPath)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	lock, _, err := loadLock(paths.SkillsLockPath, false)
	if err != nil {
		return nil, fmt.Errorf("cannot inspect catalog imports: %w; repair skills.lock.json before changing imports", err)
	}
	var members []CatalogMember
	for _, selector := range entry.Selectors {
		name := path.Base(selector)
		userEntries, userErr := catalogCandidateEntries(paths.SkillsDir, name)
		importedEntries, importedErr := catalogCandidateEntries(paths.ImportedSkillsDir, name)
		user, err := observeUserSkillNames(paths.SkillsDir, userEntries, false)
		if userErr == nil {
			userErr = err
		}
		imported, err := observeImportedSkills(paths.ImportedSkillsDir, importedEntries, false)
		if importedErr == nil {
			importedErr = err
		}
		st := &state{cfg: cfg, lock: lock, userSkills: user, local: imported, paths: paths}
		row := catalogMember(st, entry.Repository, selector)
		if problem := errors.Join(userErr, importedErr); problem != nil {
			row.Problem = problem.Error() + "; preserve the tier nodes and move or repair them before changing imports"
			row.ForeignOwned = false
		}
		members = append(members, row)
	}
	return members, nil
}

// Catalog diagnostics are scoped to this exact candidate and its case variants.
// A bad tier root becomes a row problem; mutations still refuse the root.
func catalogCandidateEntries(dir, name string) ([]os.DirEntry, error) {
	entries, err := readTierEntries(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var candidates []os.DirEntry
	for _, entry := range entries {
		if collisionName(entry.Name()) == collisionName(name) {
			candidates = append(candidates, entry)
		}
	}
	return candidates, nil
}

func catalogMember(st *state, repository, selector string) CatalogMember {
	identity := config.SkillImport{Repository: repository}.Identity()
	defaultBlock, _, hasDefaultBlock := findBlockByIdentity(st.cfg, identity)
	name := path.Base(selector)
	member := CatalogMember{Name: name, Selector: selector}
	if hasDefaultBlock && !catalogBlockEditable(defaultBlock, repository) {
		member.AddBlocked = catalogManualBlockMessage
	}
	if local, exists := st.userSkills[collisionName(name)]; exists {
		member.Legacy = true
		if _, err := readLegacySkill(local, name); err != nil {
			member.Problem = err.Error()
		}
		if filepath.Base(local) != name {
			member.Problem = "case variant local name conflicts with " + name
		}
	}
	for _, block := range st.cfg.Skills.Imports {
		if config.NormalizeSkillRepository(block.Repository) != config.NormalizeSkillRepository(repository) {
			continue
		}
		if _, selected := selectingPositiveSelector(block, selector); selected {
			member.Configured = true
			if catalogBlockEditable(block, repository) && containsSelector(block.Selectors, selector) {
				member.DefaultExact = true
			}
		}
	}
	for _, locked := range st.lock.Skills {
		if collisionName(locked.Name) != collisionName(name) {
			continue
		}
		if locked.Name != name || config.NormalizeSkillRepository(locked.Repository) != config.NormalizeSkillRepository(repository) || locked.SelectedPath != selector {
			member.Problem = "another locked source owns " + name + "; preserve it and reconcile explicitly with al skills commands"
			observed := st.skill(locked.Name)
			member.ForeignOwned = locked.Name == name && observed.Present && observed.Err == nil && st.configuredSelectionCount(locked) == 1 && !member.Legacy && !member.Configured
			if observed.Err != nil {
				member.Problem = observed.Err.Error()
			}
			continue
		}
		observed := st.skill(name)
		member.Imported = observed.Present && observed.Err == nil && member.Configured
		if !member.Configured {
			member.Problem = "locked source has no matching configured selection; repair ownership with al skills commands"
		}
		if observed.Err != nil {
			member.Problem = observed.Err.Error()
		}
	}
	for candidate := range st.local {
		if collisionName(candidate) != collisionName(name) {
			continue
		}
		_, owned := st.lock.Entry(candidate)
		if !owned {
			member.Problem = "unowned imported node " + candidate + "; resolve it explicitly before importing"
		}
		if candidate != name {
			member.ForeignOwned = false
			member.Problem = "case variant imported name conflicts with " + name
		}
	}
	if member.Legacy && (member.Imported || st.skill(name).Present) {
		member.ForeignOwned = false
		member.Problem = "both local and imported tiers occupy " + name + "; preserve and resolve both copies explicitly"
	}
	return member
}

// InstallCatalog adds absent exact catalog selectors and, when approved, retires validated legacy
// slots together. It is intentionally restricted to the migration's default
// policy and declared catalog coordinates, not a collision bypass for ordinary Add.
func (s *Service) InstallCatalog(ctx context.Context, selectors, approvedLegacy []string) (*Report, error) {
	return s.withLockedReport(func(st *state, report *Report) error {
		if err := requireCatalogPolicy(st); err != nil {
			return err
		}
		catalog, err := templates.LoadCLISkillCatalog()
		if err != nil {
			return err
		}
		allowed := map[string]bool{}
		for _, entry := range catalog {
			if entry.Repository == templates.GeneralSkillsRepository {
				for _, selector := range entry.Selectors {
					allowed[selector] = true
				}
			}
		}
		legacy := map[string]skilltree.Tree{}
		var pending []string
		for _, selector := range selectors {
			member := catalogMember(st, templates.GeneralSkillsRepository, selector)
			if !allowed[member.Selector] {
				return fmt.Errorf("%q is not a migration catalog selector", member.Selector)
			}
			if member.Problem != "" {
				return fmt.Errorf("cannot adopt %s: %s", member.Name, member.Problem)
			}
			if member.Legacy && !containsSelector(approvedLegacy, member.Selector) {
				return fmt.Errorf("legacy %s appeared after preview; rerun the wizard to review adoption", member.Name)
			}
			if member.Configured {
				if member.Legacy {
					return fmt.Errorf("%s has manually configured import coverage and a legacy local copy; resolve explicitly with al skills commands", member.Name)
				}
				continue
			}
			pending = append(pending, member.Selector)
			if !member.Legacy {
				continue
			}
			dir := filepath.Join(st.paths.SkillsDir, member.Name)
			tree, err := readLegacySkill(dir, member.Name)
			if err != nil {
				return err
			}
			legacy[member.Name] = tree
		}
		if len(pending) == 0 {
			return nil
		}
		return s.addLocked(ctx, st, AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: pending}, report, legacy)
	})
}

// Preview and adoption apply the same preservation and identity checks.
func readLegacySkill(dir, name string) (skilltree.Tree, error) {
	tree, err := skilltree.ReadStrict(skilltree.OSFS{}, dir)
	if err != nil {
		return tree, err
	}
	_, err = skilltree.ValidateSkill(tree, name)
	return tree, err
}
