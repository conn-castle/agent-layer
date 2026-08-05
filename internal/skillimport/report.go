// Package skillimport orchestrates Git-backed Agent Skill imports: selector
// resolution, desired-set reconciliation, atomic local state transactions,
// pull merges, grouped upstream pushes, and status reporting.
//
// Every operation reads and mutates skill sources inside the shared project
// lock so ordinary projection can never observe a half-applied import.
package skillimport

import (
	"fmt"
	"sort"
	"strings"
)

// Outcome names what happened to one skill in an operation. The set is closed
// so reports stay comparable across commands.
type Outcome string

const (
	// OutcomeImported reports a newly imported skill.
	OutcomeImported Outcome = "imported"
	// OutcomeUpdated reports a skill advanced to new upstream content.
	OutcomeUpdated Outcome = "updated"
	// OutcomeUnchanged reports a skill that already matched its target state.
	OutcomeUnchanged Outcome = "unchanged"
	// OutcomeRestored reports a missing imported directory rebuilt from source.
	OutcomeRestored Outcome = "restored"
	// OutcomeReset reports a skill whose local edits were discarded in favor of
	// the current configured upstream content.
	OutcomeReset Outcome = "reset"
	// OutcomeRetired reports a skill removed from the desired set and deleted.
	OutcomeRetired Outcome = "retired"
	// OutcomePruned reports a lock entry dropped because its directory was
	// already absent, including adoption into the user-managed tier.
	OutcomePruned Outcome = "pruned"
	// OutcomePushed reports a skill contributed upstream.
	OutcomePushed Outcome = "pushed"
	// OutcomeSkipped reports a skill excluded from the operation by policy.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed reports a skill-level failure that blocked only that skill.
	OutcomeFailed Outcome = "failed"
)

// SkillResult is one skill's outcome.
type SkillResult struct {
	Name         string
	Repository   string
	SelectedPath string
	Outcome      Outcome
	// Detail carries operation-specific context such as a merge conflict list
	// or the destination a change was pushed to.
	Detail string
	Err    error
}

// SourceResult is one import block's source-level outcome. A fetch,
// authentication, or ref failure blocks every skill in that block but leaves
// other sources unaffected.
type SourceResult struct {
	Repository string
	Ref        string
	Err        error
}

// Report is the complete outcome of one import operation.
type Report struct {
	Sources []SourceResult
	Skills  []SkillResult
	// ProjectionErr records a projection failure that happened after valid
	// source state was already committed. It never rolls that state back.
	ProjectionErr error
}

// Add records a skill result, replacing any earlier result for the same skill.
// One operation can touch a skill in more than one stage (membership
// reconciliation then branch advancement); the report keeps exactly one final
// line per skill so identical state always renders identically.
//
// A skill is identified by its repository and selected path, not by its name:
// two import blocks can resolve different paths to the same name, and that is
// precisely the case a report must show, because one of them succeeded and the
// other was rejected for colliding with it.
func (r *Report) Add(result SkillResult) {
	for i := range r.Skills {
		if r.Skills[i].identity() == result.identity() {
			r.Skills[i] = result
			return
		}
	}
	r.Skills = append(r.Skills, result)
}

// identity returns the repository and selected path pair that distinguishes one
// managed skill from another.
func (s SkillResult) identity() string {
	return skillKey(s.Repository, s.SelectedPath)
}

// AddSourceFailure records a source-level failure.
func (r *Report) AddSourceFailure(repository string, ref string, err error) {
	r.Sources = append(r.Sources, SourceResult{Repository: repository, Ref: ref, Err: err})
}

// Sort orders results deterministically so identical state always renders the
// same report.
func (r *Report) Sort() {
	sort.SliceStable(r.Sources, func(i, j int) bool {
		if r.Sources[i].Repository != r.Sources[j].Repository {
			return r.Sources[i].Repository < r.Sources[j].Repository
		}
		return r.Sources[i].Ref < r.Sources[j].Ref
	})
	sort.SliceStable(r.Skills, func(i, j int) bool {
		if r.Skills[i].Name != r.Skills[j].Name {
			return r.Skills[i].Name < r.Skills[j].Name
		}
		// Two blocks can resolve different paths to one name, so the name alone
		// is not a total order. Falling through to the identity keeps identical
		// state rendering identically.
		return r.Skills[i].identity() < r.Skills[j].identity()
	})
}

// Failed reports whether any scoped unit of work failed.
func (r *Report) Failed() bool {
	if r.ProjectionErr != nil || len(r.Sources) > 0 {
		return true
	}
	for _, skill := range r.Skills {
		if skill.Outcome == OutcomeFailed {
			return true
		}
	}
	return false
}

// Succeeded returns the number of skills whose work completed.
func (r *Report) Succeeded() int {
	count := 0
	for _, skill := range r.Skills {
		if skill.Outcome != OutcomeFailed {
			count++
		}
	}
	return count
}

// Partial reports whether the operation both completed and failed work, which
// callers surface differently from a total failure.
func (r *Report) Partial() bool {
	return r.Failed() && r.Succeeded() > 0
}

// Render returns the deterministic human-readable operation report.
func (r *Report) Render(operation string) string {
	r.Sort()
	var builder strings.Builder
	for _, source := range r.Sources {
		ref := source.Ref
		if ref == "" {
			ref = "(default branch)"
		}
		fmt.Fprintf(&builder, "source %s @ %s failed: %v\n", source.Repository, ref, source.Err)
	}
	for _, skill := range r.Skills {
		fmt.Fprintf(&builder, "%s %s", skill.Outcome, skill.Name)
		if skill.Detail != "" {
			fmt.Fprintf(&builder, " (%s)", skill.Detail)
		}
		if skill.Err != nil {
			fmt.Fprintf(&builder, ": %v", skill.Err)
		}
		builder.WriteString("\n")
	}
	if r.ProjectionErr != nil {
		fmt.Fprintf(&builder, "projection failed after source state was committed: %v\n", r.ProjectionErr)
	}
	switch {
	case r.Partial():
		fmt.Fprintf(&builder, "%s partially succeeded: %d of %d skills completed\n", operation, r.Succeeded(), len(r.Skills))
	case r.Failed():
		fmt.Fprintf(&builder, "%s failed\n", operation)
	default:
		fmt.Fprintf(&builder, "%s succeeded: %d skills\n", operation, len(r.Skills))
	}
	return builder.String()
}
