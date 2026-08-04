package skillimports

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// Skill status states reported by `al skills status`.
const (
	// StatusClean means the local tree matches the locked upstream hash.
	StatusClean = "clean"
	// StatusModified means the local tree differs from the locked upstream hash.
	// It is an ordinary successful state, not a failure.
	StatusModified = "modified"
	// StatusMissing means the locked skill directory is absent.
	StatusMissing = "missing"
	// StatusInvalid means the local tree could not be read or is not a valid skill.
	StatusInvalid = "invalid"
	// StatusCollided means the same name exists in the user-managed skill root.
	StatusCollided = "collided"
)

// SkillStatus is one locked import's local state.
type SkillStatus struct {
	Repository string
	SourcePath string
	SkillName  string
	State      string
	Tracking   string
	Write      string
	Ref        string
	Commit     string
	Detail     string
}

// ExclusionStatus is one configured exclusion selector.
type ExclusionStatus struct {
	Repository string
	Selector   string
}

// Status is the complete local, read-only view of skill imports.
type Status struct {
	Skills     []SkillStatus
	Exclusions []ExclusionStatus
	// Orphans are managed directories with no lock entry. They are actionable
	// errors, not an ordinary state.
	Orphans []string
	// Problems are conditions that make status exit nonzero.
	Problems []string
	// Recovered records that an interrupted publish was resolved before reading.
	Recovered bool
}

// Totals summarizes a status view.
type Totals struct {
	Total        int
	Clean        int
	Modified     int
	Missing      int
	Invalid      int
	Collided     int
	Tracked      int
	Pinned       int
	WriteEnabled int
	Exclusions   int
}

// Totals computes the summary counts.
func (s Status) Totals() Totals {
	totals := Totals{Total: len(s.Skills), Exclusions: len(s.Exclusions)}
	for _, skill := range s.Skills {
		switch skill.State {
		case StatusClean:
			totals.Clean++
		case StatusModified:
			totals.Modified++
		case StatusMissing:
			totals.Missing++
		case StatusInvalid:
			totals.Invalid++
		case StatusCollided:
			totals.Collided++
		}
		switch skill.Tracking {
		case config.SkillTrackingTracked:
			totals.Tracked++
		case config.SkillTrackingPinned:
			totals.Pinned++
		}
		if skill.Write != config.SkillWriteNone {
			totals.WriteEnabled++
		}
	}
	return totals
}

// Failed reports whether the status view represents a state the user must fix.
// Clean and modified are ordinary successful states; a malformed lock, an orphan
// directory, a collision, and a missing or invalid tree are not.
func (s Status) Failed() bool {
	if len(s.Orphans) > 0 || len(s.Problems) > 0 {
		return true
	}
	totals := s.Totals()
	return totals.Missing > 0 || totals.Invalid > 0 || totals.Collided > 0
}

// Status computes the local import state. It reads only configuration, lock
// state, and local directories: it never contacts a remote.
func (s *Service) Status() (Status, error) {
	var view Status
	err := s.lockProject(s.Root, func() error {
		loaded, loadErr := s.statusLocked()
		view = loaded
		return loadErr
	})
	return view, err
}

func (s *Service) statusLocked() (Status, error) {
	outcome, err := RecoverTransaction(s.Root)
	if err != nil {
		return Status{}, err
	}
	view := Status{Recovered: outcome.Recovered}

	paths := config.DefaultPaths(s.Root)
	rawConfig, err := os.ReadFile(paths.ConfigPath) // #nosec G304 -- path is rooted in the resolved project.
	if err != nil {
		return Status{}, fmt.Errorf("read %s: %w", paths.ConfigPath, err)
	}
	cfg, err := config.ParseConfig(rawConfig, paths.ConfigPath)
	if err != nil {
		return Status{}, err
	}
	lock, err := config.LoadSkillImportLock(paths.SkillImportLockPath)
	if err != nil {
		return Status{}, err
	}
	userNames, err := s.userManagedSkillNames()
	if err != nil {
		return Status{}, err
	}

	for _, imp := range cfg.Skills.Imports {
		repository := config.NormalizeSkillRepository(imp.Repository)
		for _, exclusion := range imp.ExclusionSelectors() {
			view.Exclusions = append(view.Exclusions, ExclusionStatus{Repository: repository, Selector: exclusion})
		}
	}
	sort.Slice(view.Exclusions, func(i, j int) bool {
		if view.Exclusions[i].Repository != view.Exclusions[j].Repository {
			return view.Exclusions[i].Repository < view.Exclusions[j].Repository
		}
		return view.Exclusions[i].Selector < view.Exclusions[j].Selector
	})

	locked := map[string]struct{}{}
	for _, entry := range lock.Entries {
		locked[entry.SkillName] = struct{}{}
		status := SkillStatus{
			Repository: entry.Repository,
			SourcePath: entry.SourcePath,
			SkillName:  entry.SkillName,
			Tracking:   entry.Tracking,
			Write:      entry.Write,
			Ref:        entry.ResolvedRefName,
			Commit:     shortCommit(entry.SourceCommit),
		}
		if userPath, collision := userNames[entry.SkillName]; collision {
			status.State = StatusCollided
			status.Detail = "also present in " + userPath
			view.Skills = append(view.Skills, status)
			continue
		}
		local := s.readLocalSkill(entry)
		var validationErr error
		if local.Err == nil && local.Present && local.Tree.HasManifest() {
			_, validationErr = ValidateImportedTree(local.Tree, entry.SourcePath)
		}
		switch {
		case local.Err != nil:
			status.State = StatusInvalid
			status.Detail = RedactSecrets(local.Err.Error())
		case !local.Present:
			status.State = StatusMissing
			status.Detail = "run 'al skills pull' to restore it"
		case !local.Tree.HasManifest():
			status.State = StatusInvalid
			status.Detail = "missing " + SkillManifestName
		case validationErr != nil:
			status.State = StatusInvalid
			status.Detail = RedactSecrets(validationErr.Error())
		case local.Modified:
			status.State = StatusModified
		default:
			status.State = StatusClean
		}
		view.Skills = append(view.Skills, status)
		imp, configured := configuredBlockForEntry(cfg.Skills.Imports, entry)
		if !configured || !lockPolicyMatchesConfig(entry, imp) {
			view.Problems = append(view.Problems, fmt.Sprintf(
				"locked skill %s no longer matches current configuration; run 'al skills pull' to reconcile", entry.SkillName,
			))
		}
	}
	sort.Slice(view.Skills, func(i, j int) bool { return view.Skills[i].SkillName < view.Skills[j].SkillName })

	dirs, err := s.importedSkillDirs()
	if err != nil {
		return Status{}, err
	}
	for _, dir := range dirs {
		if _, ok := locked[dir]; !ok {
			view.Orphans = append(view.Orphans, s.localSkillDir(dir))
		}
	}
	lockedPaths := make(map[config.SkillImportEntryKey]struct{}, len(lock.Entries))
	for _, entry := range lock.Entries {
		lockedPaths[entry.Key()] = struct{}{}
	}
	for _, imp := range cfg.Skills.Imports {
		for _, selector := range imp.PositiveSelectors() {
			if config.IsSkillSelectorWildcard(selector) {
				continue
			}
			excluded := false
			for _, exclusion := range imp.ExclusionSelectors() {
				if matchSelectorPath(exclusion, selector) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			key := config.SkillImportEntryKey{Repository: config.NormalizeSkillRepository(imp.Repository), SourcePath: selector}
			if _, ok := lockedPaths[key]; !ok {
				view.Problems = append(view.Problems, fmt.Sprintf("configured exact selector %s/%s has no lock entry; run 'al skills pull'", RedactSecrets(key.Repository), key.SourcePath))
			}
		}
	}
	sort.Strings(view.Problems)
	return view, nil
}

// WriteStatus renders the status view. The default output is the required
// summary; all expands to one line per resolved skill plus the configured
// exclusions.
func WriteStatus(w io.Writer, view Status, all bool) {
	totals := view.Totals()
	if view.Recovered {
		_, _ = fmt.Fprintln(w, "recovered an interrupted skill import publish before reading local state")
	}
	_, _ = fmt.Fprintf(w,
		"skills: %d total, %d clean, %d modified, %d missing, %d invalid, %d collided\n",
		totals.Total, totals.Clean, totals.Modified, totals.Missing, totals.Invalid, totals.Collided,
	)
	_, _ = fmt.Fprintf(w,
		"policy: %d tracked, %d pinned, %d write-enabled, %d exclusion selectors\n",
		totals.Tracked, totals.Pinned, totals.WriteEnabled, totals.Exclusions,
	)

	if all {
		for _, skill := range view.Skills {
			detail := skill.Detail
			if detail != "" {
				detail = " — " + detail
			}
			_, _ = fmt.Fprintf(w, "  %s  %s  %s  %s/%s  %s@%s  write=%s%s\n",
				skill.SkillName, skill.State, skill.Tracking,
				RedactSecrets(skill.Repository), skill.SourcePath,
				skill.Ref, skill.Commit, skill.Write, detail,
			)
		}
		for _, exclusion := range view.Exclusions {
			_, _ = fmt.Fprintf(w, "  exclusion  %s  %s%s\n",
				RedactSecrets(exclusion.Repository), config.SkillSelectorExclusionPrefix, exclusion.Selector)
		}
	}

	for _, orphan := range view.Orphans {
		_, _ = fmt.Fprintf(w,
			"orphan: %s has no lock entry; adopt it by moving it into .agent-layer/skills/, or remove it\n",
			orphan,
		)
	}
	for _, problem := range view.Problems {
		_, _ = fmt.Fprintf(w, "problem: %s\n", problem)
	}
}

// StatusError returns the aggregate error for a status view, or nil when the
// state is ordinary.
func StatusError(view Status) error {
	if !view.Failed() {
		return nil
	}
	var reasons []string
	totals := view.Totals()
	if len(view.Orphans) > 0 {
		reasons = append(reasons, fmt.Sprintf("%d unmanaged directory in the imported-skills root", len(view.Orphans)))
	}
	if totals.Missing > 0 {
		reasons = append(reasons, fmt.Sprintf("%d missing", totals.Missing))
	}
	if totals.Invalid > 0 {
		reasons = append(reasons, fmt.Sprintf("%d invalid", totals.Invalid))
	}
	if totals.Collided > 0 {
		reasons = append(reasons, fmt.Sprintf("%d colliding", totals.Collided))
	}
	reasons = append(reasons, view.Problems...)
	return fmt.Errorf("skill imports need attention: %s", strings.Join(reasons, ", "))
}
