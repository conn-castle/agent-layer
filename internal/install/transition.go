package install

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
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

// CheckTransitionState is read-only. Sync and skills commands deliberately do
// not use this gate: pristine local content remains usable there, as in v1.1.
// Only transition-related mutations require completed adoption.
func CheckTransitionState(root string, sys System) error {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		return err
	}
	candidates := map[string]string{}
	for _, name := range templates.RetiredSkillNames {
		candidates[".agent-layer/skills/"+name+"/SKILL.md"] = messages.TransitionPristineSkill
	}
	for _, name := range []string{"00_rules.md", "01_memory.md"} {
		candidates[".agent-layer/instructions/"+name] = messages.TransitionPristineInstruction
	}
	var findings TransitionFindings
	for _, rel := range slices.Sorted(maps.Keys(candidates)) {
		data, err := transitionReadFile(root, rel, sys)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		hash := hashNormalizedContent(data)
		matched := false
		for _, manifest := range manifests {
			for _, file := range manifest.Files {
				if file.Path == rel && file.FullHashNormalized == hash {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			findings = append(findings, TransitionFinding{rel, candidates[rel]})
		}
	}
	raw, err := transitionReadFile(root, upgradeMigrationConfigPath, sys)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if _, err := config.ParseConfig(raw, upgradeMigrationConfigPath); errors.Is(err, config.ErrConfigNeedsUpgrade) {
			findings = append(findings, TransitionFinding{upgradeMigrationConfigPath, err.Error()})
		}
	}
	for _, tier := range []string{"skills", "instructions"} {
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

// CheckWizardSource checks the source floor and transition state without an
// instruction-order preflight: the wizard still assigns orders on accepted edits.
func CheckWizardSource(root string, sys System) error {
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

// CheckUpgradeSource checks an installed project's boundary before recovery or writes.
func CheckUpgradeSource(root string, sys System) error {
	if err := CheckWizardSource(root, sys); err != nil {
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
		return nil
	}
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
		if config.LocalInstructionEntry(entry) {
			return config.UnconfiguredLocalInstruction(path, instructions)
		}
		return nil
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
