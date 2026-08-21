package skillimport

import (
	"fmt"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
)

// StatusEntry is one resolved skill's local, network-free status.
type StatusEntry struct {
	Name         string
	Repository   string
	SelectedPath string
	// Ref is the recorded resolved ref, or the configured ref when no lock
	// evidence exists yet.
	Ref string
	// Tracking is the recorded tracking mode, empty when no lock evidence
	// exists yet.
	Tracking     string
	WritePolicy  string
	Condition    Condition
	WriteEnabled bool
	// Workspace is the conflict workspace path relative to the repository root
	// when Condition is conflicted.
	Workspace string
}

// StatusExclusion is one configured exclusion selector.
type StatusExclusion struct {
	Repository string
	Selector   string
}

// Status is the complete local view of configured skill imports.
type Status struct {
	Entries    []StatusEntry
	Exclusions []StatusExclusion
	// MissingRefEvidence lists configured blocks with no locked ref-kind
	// evidence. Status never guesses a ref kind offline.
	MissingRefEvidence []string
}

// Status reports local skill import state without contacting any remote.
func (s *Service) Status() (*Status, error) {
	var status *Status
	err := s.withLockedState(func(st *state) error {
		built, buildErr := buildStatus(st)
		if buildErr != nil {
			return buildErr
		}
		status = built
		return nil
	})
	return status, err
}

func buildStatus(st *state) (*Status, error) {
	if err := failOnOrphans(st); err != nil {
		return nil, err
	}
	status := &Status{}

	for _, block := range st.cfg.Skills.Imports {
		repository := config.NormalizeSkillRepository(block.Repository)
		for _, selector := range block.ExclusionSelectors() {
			status.Exclusions = append(status.Exclusions, StatusExclusion{Repository: repository, Selector: selector})
		}
		if len(st.entriesForBlock(block)) == 0 {
			ref := block.Ref
			if ref == "" {
				ref = "(default branch)"
			}
			status.MissingRefEvidence = append(status.MissingRefEvidence,
				fmt.Sprintf("%s @ %s has no recorded source state; run 'al skills pull'", repository, ref))
		}
	}

	for _, entry := range st.lock.Skills {
		if err := validateConflictWorkspaceMetadata(st, entry.Name); err != nil {
			return nil, err
		}
		block, _, configured := st.configuredBlockForEntry(entry)
		writePolicy := config.SkillWritePolicyNone
		writeEnabled := false
		if configured {
			writePolicy = block.EffectiveWritePolicy()
			writeEnabled = block.WriteEnabled()
		}
		condition := st.classify(entry)
		workspace := ""
		if condition == ConditionConflicted {
			if path, ok := matchingConflictWorkspace(st, entry); ok {
				workspace = relativeTo(st.paths.Root, path)
			}
		}
		status.Entries = append(status.Entries, StatusEntry{
			Name:         entry.Name,
			Repository:   entry.Repository,
			SelectedPath: entry.SelectedPath,
			Ref:          entry.ResolvedRef,
			Tracking:     entry.Tracking,
			WritePolicy:  writePolicy,
			Condition:    condition,
			WriteEnabled: writeEnabled,
			Workspace:    workspace,
		})
	}

	sort.Slice(status.Entries, func(i, j int) bool { return status.Entries[i].Name < status.Entries[j].Name })
	sort.Slice(status.Exclusions, func(i, j int) bool {
		if status.Exclusions[i].Repository != status.Exclusions[j].Repository {
			return status.Exclusions[i].Repository < status.Exclusions[j].Repository
		}
		return status.Exclusions[i].Selector < status.Exclusions[j].Selector
	})
	sort.Strings(status.MissingRefEvidence)
	return status, nil
}

// Render returns the default summary, or the expanded per-skill listing when
// all is true.
func (s *Status) Render(all bool) string {
	var builder strings.Builder
	counts := map[Condition]int{}
	tracked, pinned, writeEnabled := 0, 0, 0
	for _, entry := range s.Entries {
		counts[entry.Condition]++
		switch entry.Tracking {
		case config.SkillTrackingTracked:
			tracked++
		case config.SkillTrackingPinned:
			pinned++
		}
		if entry.WriteEnabled {
			writeEnabled++
		}
	}

	fmt.Fprintf(&builder, "imported skills: %d total, %d clean, %d modified, %d missing, %d invalid, %d collided, %d conflicted\n",
		len(s.Entries), counts[ConditionClean], counts[ConditionModified], counts[ConditionMissing],
		counts[ConditionInvalid], counts[ConditionCollided], counts[ConditionConflicted])
	fmt.Fprintf(&builder, "tracking: %d tracked, %d pinned, %d write-enabled\n", tracked, pinned, writeEnabled)
	fmt.Fprintf(&builder, "configured exclusions: %d\n", len(s.Exclusions))

	if all {
		for _, entry := range s.Entries {
			fmt.Fprintf(&builder, "%s\t%s\t%s\t%s\t%s\t%s\twrite=%s",
				entry.Name, entry.Condition, entry.Repository, entry.SelectedPath,
				entry.Ref, entry.Tracking, entry.WritePolicy)
			if entry.Workspace != "" {
				fmt.Fprintf(&builder, "\t%s", entry.Workspace)
			}
			builder.WriteString("\n")
		}
		for _, exclusion := range s.Exclusions {
			fmt.Fprintf(&builder, "exclusion\t%s\t!%s\n", exclusion.Repository, exclusion.Selector)
		}
	}
	for _, missing := range s.MissingRefEvidence {
		fmt.Fprintf(&builder, "no recorded state: %s\n", missing)
	}
	return builder.String()
}
