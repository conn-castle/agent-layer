package install

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

// ErrUnsupportedSource identifies sources below the supported installed-project floor.
var ErrUnsupportedSource = errors.New("unsupported installed-project source")

// ErrUnfinishedTransition identifies positively recognized unfinished transition state.
var ErrUnfinishedTransition = errors.New("unfinished v1.0.0 transition")

// TransitionFinding reports one preserved path and how to finish its transition.
type TransitionFinding struct{ Path, Remediation string }

// TransitionFindings reports all recognized unfinished state in a project.
type TransitionFindings []TransitionFinding

func (f TransitionFindings) Error() string {
	lines := make([]string, 0, len(f))
	for _, finding := range f {
		lines = append(lines, finding.Path+": "+finding.Remediation)
	}
	return ErrUnfinishedTransition.Error() + ":\n" + strings.Join(lines, "\n")
}
func (f TransitionFindings) Unwrap() error { return ErrUnfinishedTransition }

// CheckTransitionState is read-only and reports only blocking transition state:
// configuration and adoption evidence this release can no longer convert.
// Unmodified shipped local copies are not blocking; PlanPristineConversions
// lists them so al upgrade can convert them. Sync and skills commands
// deliberately do not use this gate.
func CheckTransitionState(root string, sys System) error {
	var findings TransitionFindings
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if _, err := config.ParseConfig(raw, upgradeMigrationConfigPath); errors.Is(err, config.ErrConfigNeedsUpgrade) {
			findings = append(findings, TransitionFinding{upgradeMigrationConfigPath, err.Error()})
		}
	}
	for _, tier := range []string{"skills", instructionsDirName} {
		staging := filepath.Join(root, ".agent-layer", tier+"-imported", skilljournal.StagingDirName)
		if err := skilljournal.CheckAdoptionStaging(staging); err != nil {
			if !errors.Is(err, skilljournal.ErrAdoptionJournal) {
				return err
			}
			findings = append(findings, TransitionFinding{staging, err.Error()})
		}
	}
	if len(findings) > 0 {
		return findings
	}
	return nil
}

// PristineConversionKind distinguishes converted skill directories from instruction files.
type PristineConversionKind string

const (
	// PristineConversionSkill converts a moved skill directory to a catalog skill import.
	PristineConversionSkill PristineConversionKind = "skill"
	// PristineConversionInstruction converts a standard instruction file to an instruction import.
	PristineConversionInstruction PristineConversionKind = "instruction"
)

// PristineConversion is one unmodified shipped local copy that al upgrade
// replaces with the Git import a fresh setup creates.
type PristineConversion struct {
	Kind PristineConversionKind `json:"kind"`
	// Path is the repository-relative local skill directory or instruction file.
	Path string `json:"path"`
	// Name is the imported skill name or instruction file name.
	Name string `json:"name"`
	// Selector is the import selector in templates.GeneralSkillsRepository.
	Selector string `json:"selector"`
	// LocalName is the instruction file whose [[instructions.local]] block the
	// import replaces, reusing its order. Empty for skills.
	LocalName string `json:"local_name,omitempty"`
	// DeleteOnly means the import already committed; only the local copy remains.
	DeleteOnly bool `json:"delete_only,omitempty"`
}

// PlanPristineConversions is read-only. It lists local copies that equal a
// complete shipped version: a moved skill directory whose entire tree equals
// one published manifest version, or a standard instruction file with a
// published hash whose own [[instructions.local]] block selects it. Anything
// else (edited or extra files, links, unreferenced files, foreign imports) is
// user-managed local content and is not listed.
func PlanPristineConversions(root string, sys System) ([]PristineConversion, error) {
	if _, err := sys.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return nil, err
	}
	selectors, err := catalogSkillSelectors()
	if err != nil {
		return nil, err
	}
	var skills []PristineConversion
	for _, name := range templates.RetiredSkillNames {
		rel := ".agent-layer/skills/" + name
		pristine, err := pristineSkillTree(root, rel, sys, manifests)
		if err != nil {
			return nil, err
		}
		if pristine && selectors[name] != "" {
			skills = append(skills, PristineConversion{Kind: PristineConversionSkill, Path: rel, Name: name, Selector: selectors[name]})
		}
	}
	var instructions []PristineConversion
	for _, name := range []string{"rules.md", "memory.md"} {
		local := skilljournal.LegacyInstruction(name)
		rel := ".agent-layer/instructions/" + local
		data, err := transitionReadFile(root, rel, sys)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if pristineFile(manifests, rel, data) {
			instructions = append(instructions, PristineConversion{Kind: PristineConversionInstruction, Path: rel, Name: name, Selector: "instructions/" + name, LocalName: local})
		}
	}
	if len(skills)+len(instructions) == 0 {
		return nil, nil
	}
	cfg := &config.Config{}
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if err == nil {
		cfg, err = config.ParseConfigLenient(raw, upgradeMigrationConfigPath)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	active, err := skillmigration.ActiveNames(root)
	if err != nil {
		return nil, err
	}
	var conversions []PristineConversion
	for _, item := range skills {
		switch {
		case active[item.Name]:
			if err := requireIntactImport(root, cfg, item); err != nil {
				return nil, err
			}
			item.DeleteOnly = true
		case skillImportConfigured(cfg, item.Selector):
			continue // Configured but not imported: ordinary import commands own it.
		case !catalogBlockEditable(cfg):
			continue // Imports never edit a manually managed catalog block.
		default:
			if exists, err := transitionPathExists(root, ".agent-layer/skills-imported/"+item.Name, sys); err != nil {
				return nil, err
			} else if exists {
				continue // Another source occupies the imported slot.
			}
		}
		conversions = append(conversions, item)
	}
	for _, item := range instructions {
		hasLocal := false
		for _, local := range cfg.Instructions.Local {
			if len(local.Selectors) == 1 && local.Selectors[0] == item.LocalName {
				hasLocal = true
			}
		}
		switch {
		case active[item.Name]:
			if hasLocal {
				continue // Both copies are configured: the project chose to keep both.
			}
			if err := requireIntactImport(root, cfg, item); err != nil {
				return nil, err
			}
			item.DeleteOnly = true
		case !hasLocal || instructionImportConfigured(cfg, item.Name):
			continue // Unreferenced local file or a foreign same-name import.
		default:
			if raw == nil {
				continue
			}
			if occupied, err := localInstructionNamed(root, cfg, item.Name, sys); err != nil {
				return nil, err
			} else if occupied {
				continue // A local rules.md or memory.md already uses the import's name.
			}
			if _, order, err := config.RemoveLocalInstruction(string(raw), item.LocalName); err != nil || order == nil {
				continue //nolint:nilerr // A block that cannot be edited stays user-managed.
			}
			if exists, err := transitionPathExists(root, ".agent-layer/instructions-imported/"+item.Name, sys); err != nil {
				return nil, err
			} else if exists {
				continue
			}
		}
		conversions = append(conversions, item)
	}
	return conversions, nil
}

// loadTransitionConfig reads the project configuration leniently; a missing
// file yields an empty configuration.
func loadTransitionConfig(root string, sys System) (*config.Config, error) {
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if errors.Is(err, os.ErrNotExist) {
		return &config.Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	return config.ParseConfigLenient(raw, upgradeMigrationConfigPath)
}

// requireIntactImport refuses deleting a local copy unless configuration owns
// its import and the imported content still equals the locked tree, so the
// deletion can never leave a project that sync refuses.
func requireIntactImport(root string, cfg *config.Config, item PristineConversion) error {
	if err := intactImport(root, cfg, item); err != nil {
		kind := "skills"
		if item.Kind == PristineConversionInstruction {
			kind = "instructions"
		}
		return fmt.Errorf(messages.UpgradeConvertPristineInconsistentFmt, item.Path, item.Selector, err, kind, kind)
	}
	return nil
}

// intactImport reports why item's import cannot replace its local copy: the
// configuration must own the import and the imported tree must match its lock
// entry. A nil error means deleting the local copy loses nothing.
func intactImport(root string, cfg *config.Config, item PristineConversion) error {
	paths := config.DefaultPaths(root)
	var (
		lock *skilllock.File
		err  error
		tree skilltree.Tree
	)
	imported := filepath.Join(paths.ImportedSkillsDir, item.Name)
	if item.Kind == PristineConversionInstruction {
		if !instructionImportOwned(cfg, item.Name) {
			return errors.New("configuration does not own the import")
		}
		imported = filepath.Join(paths.ImportedInstructionsDir, item.Name)
		lock, err = skilllock.LoadInstructions(paths.InstructionsLockPath)
		if err == nil {
			tree, err = skilltree.ReadFileNode(skilltree.OSFS{}, imported, item.Name)
		}
	} else {
		if !skillImportConfigured(cfg, item.Selector) {
			return errors.New("configuration does not own the import")
		}
		lock, err = skilllock.Load(paths.SkillsLockPath)
		if err == nil {
			err = requireRealDirectory(imported)
		}
		if err == nil {
			tree, err = skilltree.Read(skilltree.OSFS{}, imported)
		}
		if err == nil {
			_, err = skilltree.ValidateSkill(tree, item.Name)
		}
	}
	if err != nil {
		return err
	}
	entry, ok := lock.Entry(item.Name)
	if !ok || tree.Hash() != entry.TreeHash {
		return errors.New("imported content does not match its lock entry")
	}
	return nil
}

func requireRealDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf(messages.TransitionRealDirectoryFmt, path)
	}
	return nil
}

// pristineFile reports whether data equals the shipped content of rel in any
// published manifest version, after line-ending and trailing-newline
// normalization.
func pristineFile(manifests map[string]templateManifest, rel string, data []byte) bool {
	hash := hashNormalizedContent(data)
	for _, manifest := range manifests {
		for _, file := range manifest.Files {
			if file.Path == rel && file.FullHashNormalized == hash {
				return true
			}
		}
	}
	return false
}

// pristineSkillTree reports whether rel is a real directory of regular files
// that equals the complete shipped file set of one manifest version.
func pristineSkillTree(root, rel string, sys System, manifests map[string]templateManifest) (bool, error) {
	info, err := transitionLstat(root, rel, sys)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	return pristineSkillDir(filepath.Join(root, filepath.FromSlash(rel)), rel, sys, manifests)
}

// pristineSkillDir reports whether dir, a real directory of regular files,
// equals the complete shipped file set of rel in one manifest version.
func pristineSkillDir(dir, rel string, sys System, manifests map[string]templateManifest) (bool, error) {
	if info, err := sys.Lstat(dir); err != nil || !info.IsDir() {
		return false, err
	}
	local := map[string]string{}
	regular := true
	err := sys.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			regular = false
			return fs.SkipAll
		}
		data, err := sys.ReadFile(path)
		if err != nil {
			return err
		}
		sub, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		local[rel+"/"+filepath.ToSlash(sub)] = hashNormalizedContent(data)
		return nil
	})
	if err != nil || !regular || len(local) == 0 {
		return false, err
	}
	for _, manifest := range manifests {
		shipped := map[string]string{}
		for _, file := range manifest.Files {
			if strings.HasPrefix(file.Path, rel+"/") {
				shipped[file.Path] = file.FullHashNormalized
			}
		}
		if maps.Equal(shipped, local) {
			return true, nil
		}
	}
	return false, nil
}

// catalogSkillSelectors maps each general-repository catalog skill name to
// the exact selector a fresh setup imports it with.
func catalogSkillSelectors() (map[string]string, error) {
	catalog, err := templates.LoadCLISkillCatalog()
	if err != nil {
		return nil, err
	}
	selectors := map[string]string{}
	for _, entry := range catalog {
		if entry.Repository == templates.GeneralSkillsRepository {
			for i, name := range entry.SkillNames() {
				selectors[name] = entry.Selectors[i]
			}
		}
	}
	return selectors, nil
}

// skillImportConfigured reports a general-repository import block whose
// positive selectors, including wildcards, select selector.
func skillImportConfigured(cfg *config.Config, selector string) bool {
	for _, imp := range cfg.Skills.Imports {
		if _, selected := imp.SelectingPositiveSelector(selector); selected && config.NormalizeSkillRepository(imp.Repository) == templates.GeneralSkillsRepository {
			return true
		}
	}
	return false
}

// instructionImportOwned reports the exact general-repository import a fresh
// setup configures for a standard instruction.
func instructionImportOwned(cfg *config.Config, name string) bool {
	for _, imp := range cfg.Instructions.Imports {
		if len(imp.Selectors) == 1 && imp.Selectors[0] == "instructions/"+name && config.NormalizeSkillRepository(imp.Repository) == templates.GeneralSkillsRepository {
			return true
		}
	}
	return false
}

// localInstructionNamed reports a local instruction, configured or on disk,
// that already uses the import's file name, which instruction validation
// requires to be unique across the local and imported tiers.
func localInstructionNamed(root string, cfg *config.Config, name string, sys System) (bool, error) {
	for _, local := range cfg.Instructions.Local {
		for _, selector := range local.Selectors {
			if strings.EqualFold(path.Base(selector), name) {
				return true, nil
			}
		}
	}
	return transitionPathExists(root, ".agent-layer/instructions/"+name, sys)
}

// catalogBlockEditable applies the import policy (config.ManualSkillSelectors):
// conversion never edits a manually managed default-policy catalog block, so
// its shipped copies stay user-managed.
func catalogBlockEditable(cfg *config.Config) bool {
	identity := config.SkillImport{Repository: templates.GeneralSkillsRepository}.Identity()
	for _, imp := range cfg.Skills.Imports {
		if imp.Identity() == identity && config.ManualSkillSelectors(imp.Selectors) {
			return false
		}
	}
	return true
}

// instructionImportConfigured reports any instruction import, from any
// repository, whose selector names a file called name.
func instructionImportConfigured(cfg *config.Config, name string) bool {
	for _, imp := range cfg.Instructions.Imports {
		for _, selector := range imp.Selectors {
			if strings.EqualFold(path.Base(selector), name) {
				return true
			}
		}
	}
	return false
}

func transitionPathExists(root, rel string, sys System) (bool, error) {
	_, err := transitionLstat(root, rel, sys)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// transitionLstat inspects rel after requiring every ancestor to be a real directory.
func transitionLstat(root, rel string, sys System) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	current := root
	var info fs.FileInfo
	for i, part := range parts {
		current = filepath.Join(current, part)
		var err error
		info, err = sys.Lstat(current)
		if err != nil {
			return nil, err
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf(messages.TransitionRealDirectoryFmt, current)
		}
	}
	return info, nil
}

// CheckWizardSource checks the source floor and transition state without an
// instruction-order preflight: the wizard still assigns orders on accepted edits.
// Unmodified shipped copies refuse here because only al upgrade converts them.
func CheckWizardSource(root string, sys System) error {
	if err := checkSupportedSource(root, sys); err != nil {
		return err
	}
	conversions, err := PlanPristineConversions(root, sys)
	if err != nil {
		return err
	}
	if len(conversions) > 0 {
		return fmt.Errorf("%w: "+messages.TransitionConvertBeforeWizardFmt, ErrUnfinishedTransition, len(conversions))
	}
	return nil
}

// checkSupportedSource refuses sources below the floor and blocking transition
// state. Known pre-1.0 pins are classified here, never in CheckTransitionState,
// so the wizard and upgrade cannot diverge.
func checkSupportedSource(root string, sys System) error {
	info, err := sys.Lstat(filepath.Join(root, ".agent-layer"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf(messages.TransitionRealDirectoryFmt, filepath.Join(root, ".agent-layer"))
	}
	if err := validateUpgradeStateDirs(root, sys); err != nil {
		return err
	}
	// Validate source evidence ancestors before the resolver can read them.
	for _, rel := range []string{pinVersionRelPath, baselineStateRelPath} {
		if _, err := transitionReadFile(root, rel, sys); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	resolution := (&installer{root: root, sys: sys}).resolveUpgradeMigrationSourceVersion()
	if resolution.origin != UpgradeMigrationSourceUnknown {
		cmp, err := version.Compare(resolution.version, skillmigration.MinimumCLI)
		if err != nil {
			return err
		}
		if cmp < 0 {
			return fmt.Errorf("%w: "+messages.TransitionUnsupportedSourceFmt, ErrUnsupportedSource, resolution.version, resolution.origin)
		}
	}
	return CheckTransitionState(root, sys)
}

// CheckUpgradeSource checks an installed project's boundary and its local
// instruction files. Unmodified shipped copies do not block: upgrade converts
// them.
func CheckUpgradeSource(root string, sys System) error {
	if err := checkUpgradeBoundary(root, sys); err != nil {
		return err
	}
	return checkUnconfiguredInstructions(root, sys)
}

// checkUpgradeSourceBeforeRecovery refuses before the project lock is created
// whenever no import transaction awaits recovery. A pending transaction may
// have removed a converted file's local block, so its instruction check waits
// for recovery under the lock.
func checkUpgradeSourceBeforeRecovery(root string, sys System) error {
	if err := checkUpgradeBoundary(root, sys); err != nil {
		return err
	}
	paths := config.DefaultPaths(root)
	for _, tier := range []string{paths.ImportedSkillsDir, paths.ImportedInstructionsDir} {
		if _, err := sys.Lstat(skilljournal.StagingRoot(tier)); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return checkUnconfiguredInstructions(root, sys)
}

// checkUpgradeBoundary checks the source version and instruction configuration
// before recovery or writes.
func checkUpgradeBoundary(root string, sys System) error {
	if err := checkSupportedSource(root, sys); err != nil {
		return err
	}
	if _, err := sys.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := config.ValidateInstructionRootsFS(os.DirFS(root), root); err != nil {
		return err
	}
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if errors.Is(err, os.ErrNotExist) {
		return nil // Upgrade seeds config; the wizard can then order tracked local files.
	}
	if err != nil {
		return err
	}
	cfg, err := config.ParseConfigLenient(raw, upgradeMigrationConfigPath)
	if err != nil {
		return err
	}
	instructions := cfg.Instructions
	if len(instructions.Local)+len(instructions.Imports) > 0 {
		return config.ValidateInstructions(upgradeMigrationConfigPath, instructions)
	}
	return nil
}

// checkUnconfiguredInstructions refuses local instruction files that sync would
// reject after the pin advanced. An interrupted import transaction can leave a
// file whose local block it already removed, so upgrade runs this only after
// journal recovery restores the configuration.
func checkUnconfiguredInstructions(root string, sys System) error {
	if _, err := sys.Lstat(filepath.Join(root, ".agent-layer")); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg, err := config.ParseConfigLenient(raw, upgradeMigrationConfigPath)
	if err != nil {
		return err
	}
	instructions := cfg.Instructions
	if len(instructions.Local)+len(instructions.Imports) > 0 {
		// Recovery may have restored configuration after the boundary check.
		if err := config.ValidateInstructions(upgradeMigrationConfigPath, instructions); err != nil {
			return err
		}
	}
	configured := map[string]bool{}
	for _, local := range instructions.Local {
		configured[local.Selectors[0]] = true // Validated: exactly one selector.
	}
	// Sync loads only configured local instructions, so an unconfigured file
	// (even an unmodified shipped one) would fail after the pin advanced. Only a
	// leftover copy of an intact import, which conversion deletes, is exempt.
	var deleteOnly map[string]bool
	return sys.WalkDir(filepath.Join(root, ".agent-layer", "instructions"), func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != filepath.Join(root, ".agent-layer", "instructions") {
				return fs.SkipDir
			}
			return nil
		}
		if !config.LocalInstructionEntry(entry) || configured[entry.Name()] {
			return nil
		}
		if deleteOnly == nil {
			conversions, err := PlanPristineConversions(root, sys)
			if err != nil {
				return err
			}
			deleteOnly = map[string]bool{}
			for _, item := range conversions {
				deleteOnly[item.Path] = item.DeleteOnly && item.Kind == PristineConversionInstruction
			}
		}
		if deleteOnly[".agent-layer/instructions/"+entry.Name()] {
			return nil
		}
		return config.UnconfiguredLocalInstruction(path)
	})
}

func transitionReadFile(root, rel string, sys System) ([]byte, error) {
	parts := strings.Split(rel, "/")
	path := root
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := sys.Lstat(path)
		if err != nil {
			return nil, err
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return nil, fmt.Errorf(messages.TransitionRealDirectoryFmt, path)
			}
		} else if rel == upgradeMigrationConfigPath {
			target, err := sys.Stat(path)
			if err != nil {
				return nil, err
			}
			if err := fsutil.RequireRegularFile(target, path); err != nil {
				return nil, err
			}
		} else if err := fsutil.RequireRegularFile(info, path); err != nil {
			return nil, err
		}
	}
	return sys.ReadFile(path)
}
