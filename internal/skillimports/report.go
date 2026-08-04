package skillimports

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Skill-level outcomes reported by import operations.
const (
	// ActionImported records a newly imported skill.
	ActionImported = "imported"
	// ActionUpdated records a skill advanced to new upstream content.
	ActionUpdated = "updated"
	// ActionUnchanged records a skill that already matched its desired content.
	ActionUnchanged = "unchanged"
	// ActionRestored records a desired skill re-created from its source because
	// its local directory was missing.
	ActionRestored = "restored"
	// ActionRetired records a skill removed because it left the desired set.
	ActionRetired = "retired"
	// ActionPruned records a lock entry dropped because its directory was already
	// gone when it left the desired set.
	ActionPruned = "pruned"
	// ActionPushed records skill content written to a destination repository.
	ActionPushed = "pushed"
	// ActionSkipped records a candidate deliberately not acted on.
	ActionSkipped = "skipped"
	// ActionFailed records a skill-level failure that blocked only that skill.
	ActionFailed = "failed"
)

// SkillResult is one skill's outcome.
type SkillResult struct {
	// Repository is the source repository the skill came from.
	Repository string
	// SourcePath is the selected repository-relative path.
	SourcePath string
	// SkillName is the local directory name.
	SkillName string
	// Action is one of the Action* constants.
	Action string
	// Detail adds user-facing context, such as why a skill was skipped.
	Detail string
	// Err is set when Action is ActionFailed.
	Err error
}

// SourceResult is one import block's source-level outcome. A source-level
// failure blocks every skill in that block but never another source.
type SourceResult struct {
	// Repository is the block's source repository.
	Repository string
	// Ref describes the configured ref, or "(default branch)" when omitted.
	Ref string
	// Detail adds user-facing context for a successful resolution.
	Detail string
	// Err is set when the source could not be resolved, fetched, or authenticated.
	Err error
}

// Report is the structured outcome of one import operation.
type Report struct {
	// Sources holds one entry per import block that was contacted.
	Sources []SourceResult
	// Skills holds one entry per skill that was considered.
	Skills []SkillResult
	// Notes hold operation-level messages that belong to no single skill.
	Notes []string
	// ProjectionErr records a projection failure that happened after valid source
	// state was already published. The source state is deliberately retained.
	ProjectionErr error
}

// AddSource records a source-level outcome.
func (r *Report) AddSource(result SourceResult) {
	r.Sources = append(r.Sources, result)
}

// AddSkill records a skill-level outcome.
func (r *Report) AddSkill(result SkillResult) {
	r.Skills = append(r.Skills, result)
}

// Failf records a skill-level failure.
func (r *Report) Failf(repository string, sourcePath string, skillName string, err error) {
	r.AddSkill(SkillResult{
		Repository: repository,
		SourcePath: sourcePath,
		SkillName:  skillName,
		Action:     ActionFailed,
		Err:        err,
	})
}

// Note records an operation-level message.
func (r *Report) Note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// Failed reports whether anything in the operation failed, including a
// post-publish projection failure.
func (r *Report) Failed() bool {
	if r.ProjectionErr != nil {
		return true
	}
	for _, source := range r.Sources {
		if source.Err != nil {
			return true
		}
	}
	for _, skill := range r.Skills {
		if skill.Err != nil {
			return true
		}
	}
	return false
}

// Succeeded reports whether any skill-level work completed successfully. It
// distinguishes a partial failure from a complete one.
func (r *Report) Succeeded() bool {
	for _, skill := range r.Skills {
		if skill.Err == nil && skill.Action != ActionSkipped {
			return true
		}
	}
	return false
}

// Err returns an aggregate error when the operation failed, distinguishing
// partial from complete failure, or nil when everything succeeded.
func (r *Report) Err() error {
	if !r.Failed() {
		return nil
	}
	if r.ProjectionErr != nil {
		return fmt.Errorf(
			"skill import state was updated but projecting it into the clients failed: %w",
			r.ProjectionErr,
		)
	}
	if r.Succeeded() {
		return fmt.Errorf("some skill imports failed; see the report above for what succeeded and what did not")
	}
	return fmt.Errorf("skill import failed; see the report above")
}

// Write renders the report deterministically. Sources come first because a
// source failure explains every skill under it.
func (r *Report) Write(w io.Writer) {
	sources := append([]SourceResult(nil), r.Sources...)
	sort.SliceStable(sources, func(i, j int) bool {
		if sources[i].Repository != sources[j].Repository {
			return sources[i].Repository < sources[j].Repository
		}
		return sources[i].Ref < sources[j].Ref
	})
	for _, source := range sources {
		label := RedactSecrets(source.Repository)
		if source.Ref != "" {
			label += " @ " + source.Ref
		}
		if source.Err != nil {
			_, _ = fmt.Fprintf(w, "source %s: failed: %s\n", label, RedactSecrets(source.Err.Error()))
			continue
		}
		detail := source.Detail
		if detail != "" {
			detail = ": " + detail
		}
		_, _ = fmt.Fprintf(w, "source %s: ok%s\n", label, detail)
	}

	skills := append([]SkillResult(nil), r.Skills...)
	sort.SliceStable(skills, func(i, j int) bool {
		if skills[i].SkillName != skills[j].SkillName {
			return skills[i].SkillName < skills[j].SkillName
		}
		return skills[i].SourcePath < skills[j].SourcePath
	})
	for _, skill := range skills {
		name := skill.SkillName
		if name == "" {
			name = skill.SourcePath
		}
		if skill.Err != nil {
			_, _ = fmt.Fprintf(w, "skill %s: failed: %s\n", name, indentContinuation(RedactSecrets(skill.Err.Error())))
			continue
		}
		detail := skill.Detail
		if detail != "" {
			detail = " (" + detail + ")"
		}
		_, _ = fmt.Fprintf(w, "skill %s: %s%s\n", name, skill.Action, detail)
	}

	for _, note := range r.Notes {
		_, _ = fmt.Fprintf(w, "%s\n", note)
	}
	if r.ProjectionErr != nil {
		_, _ = fmt.Fprintf(w,
			"projection failed after skill import state was updated; the imported skills and lock are valid, rerun 'al sync': %s\n",
			RedactSecrets(r.ProjectionErr.Error()),
		)
	}
}

// indentContinuation indents wrapped lines so a multi-line failure stays visibly
// attached to its skill.
func indentContinuation(text string) string {
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}
