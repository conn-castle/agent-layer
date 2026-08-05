package skillimport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Condition classifies one imported skill's local directory against its
// recorded upstream state.
type Condition string

const (
	// ConditionClean means the local tree still matches the locked upstream hash.
	ConditionClean Condition = "clean"
	// ConditionModified means the local tree diverged from the locked hash.
	ConditionModified Condition = "modified"
	// ConditionMissing means the imported directory is absent.
	ConditionMissing Condition = "missing"
	// ConditionInvalid means the directory exists but is not a readable, valid
	// Agent Skill.
	ConditionInvalid Condition = "invalid"
	// ConditionCollided means a user-managed skill owns the same name.
	ConditionCollided Condition = "collided"
)

// localSkill is one imported directory's observed state.
type localSkill struct {
	Name    string
	Dir     string
	Present bool
	Tree    skilltree.Tree
	// Err records why the directory could not be read or validated. It never
	// aborts the whole operation: an invalid import fails only its own skill.
	Err error
}

// Valid reports whether the directory read and validated cleanly.
func (l localSkill) Valid() bool { return l.Present && l.Err == nil }

// state is the immutable observation an operation is planned against.
type state struct {
	paths config.Paths
	// configRaw is the exact configuration file content, preserved so a
	// selector edit rewrites nothing else.
	configRaw string
	cfg       *config.Config
	lock      *skilllock.File
	// lockPresent distinguishes "no imports yet" from a lockfile that failed to
	// load, which is fatal.
	lockPresent bool
	// local maps skill name to its observed imported directory state.
	local map[string]localSkill
	// userSkills holds normalized user-managed skill names, which block an
	// import of the same name.
	userSkills map[string]string
	// env is the AL_-filtered `.agent-layer/.env` map that repository
	// references resolve their `${AL_*}` placeholders from.
	env map[string]string
}

// loadState reads configuration, lock state, and every imported directory.
// Callers must already hold the project lock.
func loadState(root string) (*state, error) {
	paths := config.DefaultPaths(root)

	raw, err := os.ReadFile(paths.ConfigPath) // #nosec G304 -- paths.ConfigPath is the resolved repository configuration file.
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", paths.ConfigPath, err)
	}
	cfg, err := config.ParseConfig(raw, paths.ConfigPath)
	if err != nil {
		return nil, err
	}

	lock, present, err := loadLock(paths.SkillsLockPath)
	if err != nil {
		return nil, err
	}

	local, err := readImportedSkills(paths.ImportedSkillsDir)
	if err != nil {
		return nil, err
	}

	userSkills, err := readUserSkillNames(paths.SkillsDir)
	if err != nil {
		return nil, err
	}

	// Secret values are read here but never enter configuration identity, lock
	// state, or any report; they are handed to the Git access boundary alone.
	env, err := config.LoadEnv(paths.EnvPath)
	if err != nil {
		return nil, err
	}

	return &state{
		paths:       paths,
		configRaw:   string(raw),
		cfg:         cfg,
		lock:        lock,
		lockPresent: present,
		local:       local,
		userSkills:  userSkills,
		env:         env,
	}, nil
}

// loadLock reads the lockfile. A missing file yields an empty document because
// a project with no imports has no lock; a malformed file fails loudly so no
// operation invents a merge base.
func loadLock(path string) (*skilllock.File, bool, error) {
	file, err := skilllock.Load(path)
	if err == nil {
		return file, true, nil
	}
	if errors.Is(err, skilllock.ErrMissing) {
		return skilllock.New(), false, nil
	}
	return nil, false, err
}

// readImportedSkills observes every directory in the imported tier without
// failing the operation for an individual unreadable or invalid skill.
func readImportedSkills(dir string) (map[string]localSkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]localSkill{}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", dir, err)
	}
	local := make(map[string]localSkill, len(entries))
	for _, entry := range entries {
		// Hidden entries are Agent Layer's own transaction staging area.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		skillDir := filepath.Join(dir, name)
		observed := localSkill{Name: name, Dir: skillDir, Present: true}
		tree, readErr := skilltree.Read(skilltree.OSFS{}, skillDir, skilltree.PolicyStrict)
		if readErr != nil {
			observed.Err = readErr
			local[name] = observed
			continue
		}
		observed.Tree = tree
		if _, validateErr := skilltree.ValidateSkill(tree, name); validateErr != nil {
			observed.Err = validateErr
		}
		local[name] = observed
	}
	return local, nil
}

// readUserSkillNames lists user-managed skill directory names by normalized
// name so a same-name import can be blocked without loading skill content.
func readUserSkillNames(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", dir, err)
	}
	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		normalized := skilltree.NormalizeName(entry.Name())
		if existing, duplicate := names[normalized]; duplicate {
			return nil, fmt.Errorf("user-managed skill directories %s and %s normalize to the same name %q; rename one directory before retrying",
				existing, path, normalized)
		}
		names[normalized] = path
	}
	return names, nil
}

// skill returns the observed local state for a name.
func (s *state) skill(name string) localSkill {
	if observed, ok := s.local[name]; ok {
		return observed
	}
	return localSkill{Name: name, Dir: filepath.Join(s.paths.ImportedSkillsDir, name)}
}

// classify returns the condition of a locked skill.
func (s *state) classify(entry skilllock.Entry) Condition {
	if _, collides := s.userSkills[skilltree.NormalizeName(entry.Name)]; collides {
		if s.skill(entry.Name).Present {
			return ConditionCollided
		}
	}
	observed := s.skill(entry.Name)
	switch {
	case !observed.Present:
		return ConditionMissing
	case observed.Err != nil:
		return ConditionInvalid
	case observed.Tree.Hash() == entry.TreeHash:
		return ConditionClean
	default:
		return ConditionModified
	}
}

// orphanDirectories lists imported directories with no lock entry, sorted.
func (s *state) orphanDirectories() []string {
	locked := make(map[string]struct{}, len(s.lock.Skills))
	for _, entry := range s.lock.Skills {
		locked[entry.Name] = struct{}{}
	}
	var orphans []string
	for name := range s.local {
		if _, ok := locked[name]; !ok {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans
}

// entriesForBlock returns the independent lock entries currently selected by a
// configured block. Matching the recorded path as well as the recorded
// selector lets a manual selector rewrite keep ownership of an already-selected
// path without inventing a block identifier or shared block state.
func (s *state) entriesForBlock(block config.SkillImport) []skilllock.Entry {
	var entries []skilllock.Entry
	for _, entry := range s.lock.Skills {
		if entryBelongsToBlock(s.cfg, block, entry) {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// entryBelongsToBlock maps an independent entry back to configuration without
// persisted block identity. Its recorded repository-and-selector pair remains
// authoritative while configured. If the selector was manually replaced, the
// entry follows its recorded path only when exactly one current block in that
// repository selects it; ambiguous ownership is left for desired-set
// validation instead of being guessed.
func entryBelongsToBlock(cfg *config.Config, block config.SkillImport, entry skilllock.Entry) bool {
	if config.NormalizeSkillRepository(entry.Repository) != config.NormalizeSkillRepository(block.Repository) {
		return false
	}
	recorded := config.NormalizeSkillSelector(entry.Selector)
	for _, selector := range block.PositiveSelectors() {
		if config.NormalizeSkillSelector(selector) == recorded {
			return true
		}
	}
	if _, _, configured := (&state{cfg: cfg}).blockForSelector(entry.Repository, entry.Selector); configured {
		return false
	}
	var owner *config.SkillImport
	for i := range cfg.Skills.Imports {
		candidate := &cfg.Skills.Imports[i]
		if config.NormalizeSkillRepository(candidate.Repository) != config.NormalizeSkillRepository(entry.Repository) {
			continue
		}
		if _, selected := selectingPositiveSelector(*candidate, entry.SelectedPath); !selected {
			continue
		}
		if owner != nil {
			return false
		}
		owner = candidate
	}
	return owner != nil && owner.Identity() == block.Identity()
}

// blockForSelector finds the configured block that owns a repository and
// selector pair.
func (s *state) blockForSelector(repository string, selector string) (config.SkillImport, int, bool) {
	normalizedRepository := config.NormalizeSkillRepository(repository)
	normalizedSelector := config.NormalizeSkillSelector(selector)
	for i, block := range s.cfg.Skills.Imports {
		if config.NormalizeSkillRepository(block.Repository) != normalizedRepository {
			continue
		}
		for _, candidate := range block.Selectors {
			if config.NormalizeSkillSelector(candidate) == normalizedSelector {
				return block, i, true
			}
		}
	}
	return config.SkillImport{}, 0, false
}

// configuredBlockForEntry finds the unique current block that owns an
// independent entry, including after a manual selector replacement that still
// selects the recorded path.
func (s *state) configuredBlockForEntry(entry skilllock.Entry) (config.SkillImport, int, bool) {
	if block, index, ok := s.blockForSelector(entry.Repository, entry.Selector); ok {
		return block, index, true
	}
	for index, block := range s.cfg.Skills.Imports {
		if entryBelongsToBlock(s.cfg, block, entry) {
			return block, index, true
		}
	}
	return config.SkillImport{}, 0, false
}

// configuredSelectionCount returns how many current blocks in the recorded
// repository select an entry's path after exclusions. A count above one is an
// ownership error; callers must not choose one block arbitrarily.
func (s *state) configuredSelectionCount(entry skilllock.Entry) int {
	count := 0
	for _, block := range s.cfg.Skills.Imports {
		if config.NormalizeSkillRepository(block.Repository) != config.NormalizeSkillRepository(entry.Repository) {
			continue
		}
		if _, selected := selectingPositiveSelector(block, entry.SelectedPath); selected {
			count++
		}
	}
	return count
}
