package organizescratch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/gitenv"
)

// DefaultMinGroup is how many entries must share a filename prefix before that
// prefix earns its own reports/<prefix> folder.
const DefaultMinGroup = 5

const (
	reviewDocName      = "ORGANIZE-REVIEW.md"
	destColumnWidth    = 38
	worktreeLinePrefix = "worktree "

	createdDirPerm = 0o755
	reviewDocPerm  = 0o644
)

// Options configures one scratch organization run.
type Options struct {
	Root          string
	Apply         bool
	Keep          []string
	MoveWorktrees bool
	MinGroup      int
}

func (o Options) resolveRoot() (string, error) {
	if strings.TrimSpace(o.Root) == "" {
		return "", errors.New("a root directory is required")
	}
	if o.MinGroup < 1 {
		return "", fmt.Errorf("min group must be a positive integer, got %d", o.MinGroup)
	}
	for _, raw := range o.Keep {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." {
			return "", fmt.Errorf("keep value %q must be a top-level name without path separators", raw)
		}
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", o.Root, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("read root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root is not a directory: %s", root)
	}
	return root, nil
}

type outcomeStatus string

const (
	statusPlanned     outcomeStatus = "planned"
	statusMoved       outcomeStatus = "moved"
	statusSkipped     outcomeStatus = "skipped"
	statusStationary  outcomeStatus = "stationary"
	statusCollision   outcomeStatus = "collision"
	statusFailed      outcomeStatus = "failed"
	statusUnattempted outcomeStatus = "unattempted"
)

type moveOutcome struct {
	placement
	status outcomeStatus
	detail string
}

// Run classifies a root, prints a read-only preview by default, and applies
// top-level moves only with --apply. It never deletes, overwrites, or merges.
func Run(ctx context.Context, opts Options, stdout, stderr io.Writer) error {
	root, err := opts.resolveRoot()
	if err != nil {
		return err
	}

	git, err := gatherGitFacts(ctx, root, stderr)
	if err != nil {
		return err
	}
	entries, err := readTopLevel(root, reservedNames(opts.Keep))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(stdout, "nothing to organize — root contains only reserved/kept entries")
		return err
	}

	prior, err := loadPriorReview(root)
	if err != nil {
		return err
	}
	classifyCtx := classifyContext{
		skillPrefixes: skillPrefixes(entries, opts.MinGroup),
		worktrees:     git.worktrees,
		tracked:       git.tracked,
	}
	plan := make([]placement, 0, len(entries))
	for _, item := range entries {
		plan = append(plan, classify(item, classifyCtx))
	}
	stationary, err := readStationaryOwners(root, opts.Keep)
	if err != nil {
		return err
	}
	plan = append(plan, stationary...)
	if err := resolveCheckoutWorktrees(ctx, plan); err != nil {
		return err
	}
	_, outcomes, err := applySymlinkSafety(root, plan, opts.MoveWorktrees)
	if err != nil {
		return err
	}

	if err := printPlan(stdout, outcomes); err != nil {
		return err
	}
	if !opts.Apply {
		body := renderReviewDoc(prior, outcomes, true, nil)
		out := &errWriter{w: stdout}
		out.printf("\nDRY RUN — filesystem unchanged. Re-run with --apply.\n\n")
		out.printf("Proposed review list:\n\n%s", body)
		if out.err != nil {
			return out.err
		}
		return outcomeError(outcomes, nil)
	}

	moveErr := applyMoves(root, outcomes)
	var repairErr error
	if opts.MoveWorktrees {
		repairErr = repairWorktrees(ctx, root, outcomes, stderr)
	}
	refreshActualSymlinkEvidence(root, outcomes)
	operationErrors := errorStrings(moveErr, repairErr)
	reviewPath := filepath.Join(root, reviewDocName)
	writeErr := writeReviewDoc(reviewPath, renderReviewDoc(prior, outcomes, false, operationErrors))

	out := &errWriter{w: stdout}
	moved, movable := outcomeCounts(outcomes)
	out.printf("\nmoved %d/%d\n", moved, movable)
	out.printf("review list: %s\n", reviewPath)
	warn := &errWriter{w: stderr}
	for _, outcome := range outcomes {
		if outcome.status == statusCollision {
			warn.printf("COLLISION, left in place: %s -> %s (%s)\n", outcome.name, outcome.dest, outcome.detail)
		}
	}

	return errors.Join(outcomeError(outcomes, moveErr), repairErr, writeErr, out.err, warn.err)
}

func errorStrings(errs ...error) []string {
	var messages []string
	for _, err := range errs {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}
	return messages
}

func outcomeCounts(outcomes []moveOutcome) (moved, movable int) {
	for _, outcome := range outcomes {
		if outcome.status != statusSkipped && outcome.status != statusStationary {
			movable++
		}
		if outcome.status == statusMoved {
			moved++
		}
	}
	return moved, movable
}

func outcomeError(outcomes []moveOutcome, primary error) error {
	if primary != nil {
		return primary
	}
	collisions := 0
	for _, outcome := range outcomes {
		if outcome.status == statusCollision {
			collisions++
		}
	}
	if collisions > 0 {
		return fmt.Errorf("%d destination collision(s); colliding entries were left in place", collisions)
	}
	return nil
}

func reservedNames(keep []string) map[string]struct{} {
	reserved := newSet(
		"reports", "artifacts", "review", reviewDocName,
		".git", ".gitignore", "README.md", ".DS_Store",
	)
	for _, name := range keep {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			reserved[trimmed] = struct{}{}
		}
	}
	return reserved
}

func readTopLevel(root string, reserved map[string]struct{}) ([]entry, error) {
	children, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read root %q: %w", root, err)
	}
	canonicalRoot := canonicalPath(root)
	entries := make([]entry, 0, len(children))
	for _, child := range children {
		if inSet(reserved, child.Name()) {
			continue
		}
		isLink := child.Type()&os.ModeSymlink != 0
		entries = append(entries, newEntry(root, canonicalRoot, child.Name(), child.IsDir(), isLink))
	}
	return entries, nil
}

func readStationaryOwners(root string, keep []string) ([]placement, error) {
	// Organized destination trees and Git metadata are deliberately excluded:
	// interpreting their old state as fresh kept input would recurse indefinitely
	// or inspect repository internals. These control files and explicit keeps are
	// the stable top-level paths whose links can be affected by this run.
	names := newSet(".gitignore", "README.md", ".DS_Store")
	excluded := newSet("reports", "artifacts", "review", reviewDocName, ".git")
	callerKept := map[string]struct{}{}
	for _, raw := range keep {
		name := strings.TrimSpace(raw)
		if name == "" || filepath.Base(name) != name || inSet(excluded, name) {
			continue
		}
		names[name] = struct{}{}
		callerKept[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	canonicalRoot := canonicalPath(root)
	owners := make([]placement, 0, len(ordered))
	for _, name := range ordered {
		path := filepath.Join(root, name)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect stationary owner %s: %w", path, err)
		}
		item := newEntry(root, canonicalRoot, name, info.IsDir(), info.Mode()&os.ModeSymlink != 0)
		var links []scannedLink
		if item.isSymlink {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				target = unreadableLinkTarget
			}
			links = []scannedLink{{target: target}}
		} else if item.isDir {
			links = scanTree(path).symlinks
		}
		if len(links) == 0 {
			continue
		}
		reason := "stable control entry inspected only as a stationary symlink owner"
		if inSet(callerKept, name) {
			reason = "caller-kept entry inspected only as a stationary symlink owner"
		}
		owners = append(owners, placement{entry: item, dest: destReviewSymlinks, reason: reason, stationary: true, links: links})
	}
	return owners, nil
}

func skillPrefixes(entries []entry, minGroup int) map[string]struct{} {
	total := map[string]int{}
	markdown := map[string]int{}
	for _, item := range entries {
		if strings.HasPrefix(item.name, ".") {
			continue
		}
		prefix := prefixOf(item.name)
		total[prefix]++
		if !item.isDir && !item.isSymlink && extOf(item.name) == "md" {
			markdown[prefix]++
		}
	}
	prefixes := map[string]struct{}{}
	for prefix, count := range total {
		if count >= minGroup && markdown[prefix] >= 1 {
			prefixes[prefix] = struct{}{}
		}
	}
	return prefixes
}

func printPlan(w io.Writer, outcomes []moveOutcome) error {
	counts := map[string]int{}
	var order []string
	left := 0
	stationary := 0
	for _, outcome := range outcomes {
		if outcome.status == statusStationary {
			stationary++
			continue
		}
		if outcome.status != statusPlanned {
			left++
			continue
		}
		if _, seen := counts[outcome.dest]; !seen {
			order = append(order, outcome.dest)
		}
		counts[outcome.dest]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })

	out := &errWriter{w: w}
	out.printf("%d movable entries — %d to move, %d left in place", len(outcomes)-stationary, len(outcomes)-stationary-left, left)
	if stationary > 0 {
		out.printf(", %d stationary symlink owner(s) affected", stationary)
	}
	out.printf("\n\n")
	out.printf("%-*s %s\n", destColumnWidth, "DESTINATION", "COUNT")
	for _, dest := range order {
		out.printf("%-*s %d\n", destColumnWidth, dest, counts[dest])
	}
	for _, outcome := range outcomes {
		switch outcome.status {
		case statusSkipped:
			out.printf("\nLEFT IN PLACE  %s\n   %s (pass --move-worktrees to relocate)\n", outcome.name, outcome.reason)
		case statusStationary:
			out.printf("\nSTAYS IN PLACE  %s\n   %s\n", outcome.name, outcome.reason)
		case statusCollision:
			out.printf("\nLEFT IN PLACE  %s\n   destination collision: %s\n", outcome.name, outcome.detail)
		}
	}
	return out.err
}

func predictOutcomes(root string, plan []placement, moveWorktrees bool) ([]moveOutcome, error) {
	outcomes := make([]moveOutcome, 0, len(plan))
	for _, candidate := range plan {
		outcome := moveOutcome{placement: candidate, status: statusPlanned}
		if candidate.stationary {
			outcome.status = statusStationary
			outcomes = append(outcomes, outcome)
			continue
		}
		if candidate.worktree && !moveWorktrees {
			outcome.status = statusSkipped
			outcome.detail = "registered worktree move requires --move-worktrees; classification: " + candidate.reason
			outcomes = append(outcomes, outcome)
			continue
		}
		target := filepath.Join(root, candidate.dest, candidate.name)
		occupied, err := destinationOccupied(root, candidate.dest, target)
		if err != nil {
			return nil, err
		}
		if occupied {
			outcome.status = statusCollision
			outcome.detail = fmt.Sprintf("`%s` already exists, including as a symlink; classification: %s", displayPath(root, target), candidate.reason)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func destinationOccupied(root, dest, target string) (bool, error) {
	if err := validateDestinationComponents(root, dest); err != nil {
		return false, err
	}
	_, err := os.Lstat(target)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect destination %s: %w", target, err)
}

func validateDestinationComponents(root, dest string) error {
	current := root
	for _, component := range strings.Split(filepath.ToSlash(dest), "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect destination component %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("destination component is not a real directory: %s", current)
		}
	}
	return nil
}

// The bounded convergence loop is required because routing one link owner into
// review/symlinks changes the planned layout used to evaluate every other link.
func applySymlinkSafety(root string, plan []placement, moveWorktrees bool) ([]placement, []moveOutcome, error) {
	for range len(plan) + 1 {
		outcomes, err := predictOutcomes(root, plan, moveWorktrees)
		if err != nil {
			return nil, nil, err
		}
		warnings := symlinkWarnings(root, outcomes)
		changed := false
		for index := range plan {
			if plan[index].stationary || len(warnings[index]) == 0 || strings.HasPrefix(plan[index].dest, reviewPrefix) {
				continue
			}
			plan[index].dest = destReviewSymlinks
			changed = true
		}
		if changed {
			continue
		}
		for index := range plan {
			if len(warnings[index]) > 0 {
				plan[index].reason += plannedSymlinkEvidence + strings.Join(firstN(warnings[index], examplesShown), "; ")
			}
		}
		final, err := predictOutcomes(root, plan, moveWorktrees)
		if err != nil {
			return nil, nil, err
		}
		activePlan := make([]placement, 0, len(plan))
		activeOutcomes := make([]moveOutcome, 0, len(final))
		for index := range final {
			if plan[index].stationary && len(warnings[index]) == 0 {
				continue
			}
			activePlan = append(activePlan, plan[index])
			activeOutcomes = append(activeOutcomes, final[index])
		}
		return activePlan, activeOutcomes, nil
	}
	return nil, nil, errors.New("symlink routing did not converge")
}

type plannedLocation struct {
	original string
	planned  string
}

func symlinkWarnings(root string, outcomes []moveOutcome) map[int][]string {
	locations := map[string]plannedLocation{}
	for _, outcome := range outcomes {
		planned := outcome.abs
		if outcome.status == statusPlanned || outcome.status == statusMoved {
			planned = filepath.Join(root, outcome.dest, outcome.name)
		}
		locations[outcome.name] = plannedLocation{original: outcome.abs, planned: planned}
	}

	warnings := map[int][]string{}
	for index, outcome := range outcomes {
		owner := locations[outcome.name]
		for _, link := range outcome.links {
			if link.target == unreadableLinkTarget {
				continue
			}
			originalLink := owner.original
			plannedLink := owner.planned
			if link.rel != "" {
				originalLink = filepath.Join(owner.original, link.rel)
				plannedLink = filepath.Join(owner.planned, link.rel)
			}
			originalTarget := link.target
			if !filepath.IsAbs(originalTarget) {
				originalTarget = filepath.Clean(filepath.Join(filepath.Dir(originalLink), originalTarget))
			}
			intendedTarget := originalTarget
			if targetName, rel, ok := topLevelTarget(root, originalTarget); ok {
				if target, exists := locations[targetName]; exists {
					intendedTarget = filepath.Join(target.planned, rel)
				}
			}
			actualTarget := link.target
			if !filepath.IsAbs(actualTarget) {
				actualTarget = filepath.Clean(filepath.Join(filepath.Dir(plannedLink), actualTarget))
			}
			if canonicalComparisonPath(actualTarget) != canonicalComparisonPath(intendedTarget) {
				shown := link.rel
				if shown == "" {
					shown = outcome.name
				}
				warnings[index] = append(warnings[index], fmt.Sprintf("%s -> %s (would resolve to %s)", filepath.ToSlash(shown), link.target, displayPath(root, actualTarget)))
			}
		}
	}
	return warnings
}

const plannedSymlinkEvidence = "; planned moves would break symlink(s): "

func refreshActualSymlinkEvidence(root string, outcomes []moveOutcome) {
	warnings := symlinkWarnings(root, outcomes)
	for index := range outcomes {
		if before, _, found := strings.Cut(outcomes[index].reason, plannedSymlinkEvidence); found {
			outcomes[index].reason = before
		}
		if len(warnings[index]) > 0 {
			outcomes[index].reason += "; actual outcomes broke or may have broken symlink(s): " +
				strings.Join(firstN(warnings[index], examplesShown), "; ")
		}
	}
}

func topLevelTarget(root, target string) (name, rel string, ok bool) {
	relToRoot, err := filepath.Rel(canonicalPath(root), canonicalComparisonPath(target))
	if err != nil || relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(os.PathSeparator)) {
		return "", "", false
	}
	parts := strings.SplitN(relToRoot, string(os.PathSeparator), 2)
	name = parts[0]
	if len(parts) == 2 {
		rel = parts[1]
	}
	return name, rel, true
}

func applyMoves(root string, outcomes []moveOutcome) error {
	for index := range outcomes {
		outcome := &outcomes[index]
		if outcome.status != statusPlanned {
			continue
		}
		destDir := filepath.Join(root, outcome.dest)
		// Validate both sides of MkdirAll so a destination component cannot be a
		// symlink before creation or be swapped to one during directory creation.
		if err := validateDestinationComponents(root, outcome.dest); err != nil {
			markFailure(outcomes, index, err.Error())
			return err
		}
		if err := os.MkdirAll(destDir, createdDirPerm); err != nil {
			markFailure(outcomes, index, fmt.Sprintf("create %s: %v", destDir, err))
			return fmt.Errorf("create %s: %w", destDir, err)
		}
		if err := validateDestinationComponents(root, outcome.dest); err != nil {
			markFailure(outcomes, index, err.Error())
			return err
		}
		target := filepath.Join(destDir, outcome.name)
		// Lstat plus Rename is not an atomic no-overwrite primitive. It protects
		// against stable destinations and earlier moves, but a concurrent writer
		// can still race between this check and Rename on platforms that overwrite.
		if _, err := os.Lstat(target); err == nil {
			outcome.status = statusCollision
			outcome.detail = fmt.Sprintf("`%s` appeared before the move", displayPath(root, target))
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			markFailure(outcomes, index, fmt.Sprintf("inspect %s: %v", target, err))
			return fmt.Errorf("inspect %s: %w", target, err)
		}
		if err := os.Rename(outcome.abs, target); err != nil {
			markFailure(outcomes, index, fmt.Sprintf("move %s to %s: %v", outcome.name, outcome.dest, err))
			return fmt.Errorf("move %s to %s: %w", outcome.name, outcome.dest, err)
		}
		outcome.status = statusMoved
		outcome.detail = "moved successfully"
	}
	return nil
}

func markFailure(outcomes []moveOutcome, failed int, detail string) {
	outcomes[failed].status = statusFailed
	outcomes[failed].detail = detail
	for index := failed + 1; index < len(outcomes); index++ {
		if outcomes[index].status == statusSkipped || outcomes[index].status == statusStationary || outcomes[index].status == statusCollision {
			continue
		}
		outcomes[index].status = statusUnattempted
		outcomes[index].detail = fmt.Sprintf("not attempted after failure of %s", outcomes[failed].name)
	}
}

type worktreeRepair struct {
	context string
	target  string
}

func resolveCheckoutWorktrees(ctx context.Context, plan []placement) error {
	for index := range plan {
		candidate := &plan[index]
		for _, rel := range candidate.gitDirTargets {
			target := checkoutTarget(candidate.abs, rel)
			registrations, err := checkoutRegistrations(ctx, target)
			if err != nil {
				return fmt.Errorf("inspect main-checkout marker at %s: %w", target, err)
			}
			if registrations == nil {
				continue
			}
			if _, registered := registrations[canonicalPath(target)]; !registered {
				continue
			}
			markRegisteredWorktree(candidate, rel, "main checkout", target)
			registeredPaths := sortedRegistrationPaths(registrations)
			var external []string
			for _, registered := range registeredPaths {
				if registrations[registered] == "" {
					candidate.worktreeRepairs = appendUniqueRepair(candidate.worktreeRepairs, worktreeRepair{context: target, target: registered})
				}
				if !pathWithin(candidate.abs, registered) {
					external = append(external, displayPath(candidate.abs, registered))
				}
			}
			if len(external) > 0 {
				candidate.reason += fmt.Sprintf("; main checkout has %d externally registered linked worktree(s): %s", len(external), strings.Join(firstN(external, examplesShown), ", "))
			}
		}
		for _, rel := range candidate.gitFileTargets {
			target := checkoutTarget(candidate.abs, rel)
			registrations, err := checkoutRegistrations(ctx, target)
			if err != nil {
				return fmt.Errorf("inspect linked-worktree marker at %s: %w", target, err)
			}
			if registrations == nil {
				continue
			}
			if _, registered := registrations[canonicalPath(target)]; !registered {
				continue
			}
			markRegisteredWorktree(candidate, rel, "linked worktree", target)
			if !repairTargets(candidate.worktreeRepairs, target) {
				candidate.worktreeRepairs = append(candidate.worktreeRepairs, worktreeRepair{context: target, target: target})
			}
		}
		sort.Strings(candidate.worktreeTargets)
	}
	return nil
}

func sortedRegistrationPaths(registrations map[string]string) []string {
	paths := make([]string, 0, len(registrations))
	for path := range registrations {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(canonicalComparisonPath(root), canonicalComparisonPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func checkoutTarget(base, rel string) string {
	if rel == "" {
		return base
	}
	return filepath.Join(base, rel)
}

func checkoutRegistrations(ctx context.Context, target string) (map[string]string, error) {
	if _, err := runGit(ctx, target, "rev-parse", "--show-toplevel"); err != nil {
		if isNotGitRepository(err) {
			return nil, nil
		}
		return nil, err
	}
	list, err := runGit(ctx, target, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	return parseWorktreeList(list), nil
}

func markRegisteredWorktree(candidate *placement, rel, kind, target string) {
	normalized := normalizedWorktreeRel(rel)
	if containsString(candidate.worktreeTargets, normalized) {
		return
	}
	candidate.worktreeTargets = append(candidate.worktreeTargets, normalized)
	candidate.worktree = true
	candidate.reason += fmt.Sprintf("; registered %s at %s", kind, displayPath(candidate.abs, target))
}

func appendUniqueRepair(repairs []worktreeRepair, repair worktreeRepair) []worktreeRepair {
	for _, existing := range repairs {
		if canonicalPath(existing.context) == canonicalPath(repair.context) && canonicalPath(existing.target) == canonicalPath(repair.target) {
			return repairs
		}
	}
	return append(repairs, repair)
}

func repairTargets(repairs []worktreeRepair, target string) bool {
	for _, repair := range repairs {
		if canonicalPath(repair.target) == canonicalPath(target) {
			return true
		}
	}
	return false
}

func normalizedWorktreeRel(rel string) string {
	if rel == "" {
		return "."
	}
	return rel
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func repairWorktrees(ctx context.Context, root string, outcomes []moveOutcome, stderr io.Writer) error {
	var failures []error
	var repairs []worktreeRepair
	for _, outcome := range outcomes {
		if outcome.status != statusMoved || !outcome.worktree {
			continue
		}
		movedTo := filepath.Join(root, outcome.dest, outcome.name)
		for _, repair := range outcome.worktreeRepairs {
			repair.context = relocatedPath(outcome.abs, movedTo, repair.context)
			repair.target = relocatedPath(outcome.abs, movedTo, repair.target)
			repairs = append(repairs, repair)
			if _, err := runGit(ctx, repair.context, "worktree", "repair", repair.target); err != nil {
				failure := fmt.Errorf("repair worktree %s: run `git -C %s worktree repair %s`: %w", repair.target, repair.context, repair.target, err)
				failures = append(failures, failure)
				_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", failure)
			}
		}
	}
	for _, repair := range repairs {
		list, err := runGit(ctx, repair.context, "worktree", "list", "--porcelain", "-z")
		if err != nil {
			failure := fmt.Errorf("verify worktree registration from %s: %w", repair.context, err)
			failures = append(failures, failure)
			_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", failure)
			continue
		}
		registrations := parseWorktreeList(list)
		registration, ok := registrations[canonicalPath(repair.target)]
		if !ok {
			failure := fmt.Errorf("worktree registration for %s is missing after repair; run `git -C %s worktree repair %s`", repair.target, repair.context, repair.target)
			failures = append(failures, failure)
			_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", failure)
		} else if registration != "" {
			failure := fmt.Errorf("worktree registration for %s remains prunable after repair: %s", repair.target, registration)
			failures = append(failures, failure)
			_, _ = fmt.Fprintf(stderr, "ERROR: %v\n", failure)
		}
	}
	return errors.Join(failures...)
}

func relocatedPath(originalRoot, movedRoot, path string) string {
	rel, err := filepath.Rel(canonicalComparisonPath(originalRoot), canonicalComparisonPath(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return path
	}
	return filepath.Join(canonicalPath(movedRoot), rel)
}

var reviewGuides = map[string]string{ // #nosec G101 -- human guidance strings, not credentials.
	destReviewCheckouts:    "Confirm no unmerged or unpushed work before removing (`git status`, `git log @{u}..HEAD`, and a diff against the intended base).",
	destReviewRegenerable:  "Reproducible in principle, but review the named size, sampling, or inspection evidence before removal.",
	destReviewUniqueAssets: "Contains authored files not tracked in the repo. Check each before removing; these may exist nowhere else.",
	destReviewBulkSamples:  "Mostly machine-generated samples. Extract any written analysis first, then decide whether to remove the bulk.",
	destReviewOversized:    "Over the automatic-clearance size limits. Inspect before removal even when the underlying file type or naming rule looks routine.",
	destReviewSymlinks:     "Inspect every link and the move-break evidence. Links are never rewritten or repaired automatically.",
	destReviewSecrets:      "Credential candidate content. Inspect values, revoke or rotate any live credentials, and handle the files as secrets until cleared.",
	destReviewUnknown:      "Could not be completely classified — inspect by hand.",
}

func writeReviewDoc(path, body string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("review document path is not a regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect review document %s: %w", path, err)
	}
	return fsutil.WriteFileAtomic(path, []byte(body), reviewDocPerm)
}

func renderReviewDoc(prior []placement, outcomes []moveOutcome, dryRun bool, operationErrors []string) string {
	grouped := map[string][]placement{}
	for _, item := range prior {
		grouped[item.dest] = append(grouped[item.dest], item)
	}
	for _, outcome := range outcomes {
		if !isReviewDestination(outcome.dest) {
			continue
		}
		if dryRun && outcome.status == statusPlanned {
			item := outcome.placement
			item.reason = "would move here — " + item.reason
			grouped[item.dest] = append(grouped[item.dest], item)
		}
		if !dryRun && outcome.status == statusMoved {
			grouped[outcome.dest] = append(grouped[outcome.dest], outcome.placement)
		}
		if outcome.status == statusStationary {
			item := outcome.placement
			item.reason = "stays in place — " + item.reason
			grouped[item.dest] = append(grouped[item.dest], item)
		}
	}

	var doc strings.Builder
	doc.WriteString("# Scratch organization review\n\n")
	if dryRun {
		doc.WriteString("DRY RUN — proposed state only; the filesystem is unchanged.\n\n")
	} else {
		doc.WriteString("This records actual outcomes from the latest apply run.\n\n")
	}
	doc.WriteString("This command only moves entries. Nothing has been deleted, overwritten, or merged; every removal decision remains human-owned.\n\n")

	var left []moveOutcome
	for _, outcome := range outcomes {
		if outcome.status != statusPlanned && outcome.status != statusMoved && outcome.status != statusStationary {
			left = append(left, outcome)
		}
	}
	if len(left) > 0 {
		doc.WriteString("## Not moved successfully\n\n")
		for _, outcome := range left {
			detail := outcome.detail
			if !strings.Contains(detail, "classification:") {
				detail += "; classification: " + outcome.reason
			}
			fmt.Fprintf(&doc, "- `%s` — **%s**: %s\n", outcome.name, outcome.status, detail)
		}
		doc.WriteString("\n")
	}
	if len(operationErrors) > 0 {
		doc.WriteString("## Operational failures\n\n")
		for _, failure := range operationErrors {
			fmt.Fprintf(&doc, "- %s\n", failure)
		}
		doc.WriteString("\n")
	}
	var bounded []moveOutcome
	for _, outcome := range outcomes {
		if strings.Contains(outcome.reason, "bounded credential inspection:") {
			bounded = append(bounded, outcome)
		}
	}
	if len(bounded) > 0 {
		doc.WriteString("## Bounded inspection disclosures\n\n")
		for _, outcome := range bounded {
			fmt.Fprintf(&doc, "- `%s` — %s\n", outcome.name, outcome.reason)
		}
		doc.WriteString("\n")
	}
	if !dryRun {
		var moved []moveOutcome
		for _, outcome := range outcomes {
			if outcome.status == statusMoved {
				moved = append(moved, outcome)
			}
		}
		if len(moved) > 0 {
			doc.WriteString("## Successfully moved\n\n")
			for _, outcome := range moved {
				fmt.Fprintf(&doc, "- `%s` → `%s`\n", outcome.name, filepath.ToSlash(filepath.Join(outcome.dest, outcome.name)))
			}
			doc.WriteString("\n")
		}
	}

	doc.WriteString("## Needs review\n\n")
	dests := make([]string, 0, len(grouped))
	for dest := range grouped {
		dests = append(dests, dest)
	}
	sort.Strings(dests)
	if len(dests) == 0 {
		doc.WriteString("Nothing currently filed under `review/` requires a judgement call.\n\n")
	}
	for _, dest := range dests {
		items := grouped[dest]
		sort.SliceStable(items, func(i, j int) bool { return items[i].name < items[j].name })
		fmt.Fprintf(&doc, "### `%s` (%d)\n\n", dest, len(items))
		if guide, ok := reviewGuides[dest]; ok {
			doc.WriteString(guide + "\n\n")
		}
		for _, item := range items {
			fmt.Fprintf(&doc, "- `%s` — %s\n", item.name, item.reason)
		}
		doc.WriteString("\n")
	}
	return strings.TrimRight(doc.String(), "\n") + "\n"
}

func isReviewDestination(dest string) bool {
	return dest == "review" || strings.HasPrefix(dest, reviewPrefix)
}

func loadPriorReview(root string) ([]placement, error) {
	reasons, err := parsePriorReasons(filepath.Join(root, reviewDocName))
	if err != nil {
		return nil, err
	}
	reviewRoot := filepath.Join(root, "review")
	info, err := os.Lstat(reviewRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect existing review directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("existing review path is not a real directory: %s", reviewRoot)
	}
	children, err := os.ReadDir(reviewRoot)
	if err != nil {
		return nil, fmt.Errorf("read existing review directory: %w", err)
	}
	var prior []placement
	for _, child := range children {
		childPath := filepath.Join(reviewRoot, child.Name())
		if child.IsDir() && child.Type()&os.ModeSymlink == 0 {
			items, readErr := os.ReadDir(childPath)
			if readErr != nil {
				return nil, fmt.Errorf("read existing review bucket %s: %w", childPath, readErr)
			}
			dest := filepath.ToSlash(filepath.Join("review", child.Name()))
			for _, item := range items {
				prior = append(prior, priorPlacement(dest, item.Name(), reasons))
			}
			continue
		}
		prior = append(prior, priorPlacement("review", child.Name(), reasons))
	}
	return prior, nil
}

func priorPlacement(dest, name string, reasons map[string]string) placement {
	reason := reasons[dest+"\x00"+name]
	if reason == "" {
		reason = "filed by an earlier run; reason not recorded"
	}
	return placement{entry: entry{name: name}, dest: dest, reason: reason}
}

func parsePriorReasons(path string) (map[string]string, error) {
	reasons := map[string]string{}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("prior review document is not a regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect prior review document: %w", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- fixed document in caller-selected root.
	if errors.Is(err, os.ErrNotExist) {
		return reasons, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read prior review document: %w", err)
	}
	currentDest := ""
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "### `") {
			rest := strings.TrimPrefix(line, "### `")
			if dest, _, ok := strings.Cut(rest, "`"); ok && isReviewDestination(dest) && filepath.Clean(dest) == dest {
				currentDest = dest
			} else {
				currentDest = ""
			}
			continue
		}
		if currentDest == "" || !strings.HasPrefix(line, "- `") {
			continue
		}
		rest := strings.TrimPrefix(line, "- `")
		name, reason, ok := strings.Cut(rest, "` — ")
		if !ok || name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "`\r\n") || strings.TrimSpace(reason) == "" {
			continue
		}
		reasons[currentDest+"\x00"+name] = reason
	}
	return reasons, nil
}

type gitFacts struct {
	repository bool
	top        string
	worktrees  map[string]struct{}
	tracked    map[string]struct{}
}

func gatherGitFacts(ctx context.Context, root string, stderr io.Writer) (gitFacts, error) {
	facts := gitFacts{worktrees: map[string]struct{}{}, tracked: map[string]struct{}{}}
	top, err := runGit(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		if isNotGitRepository(err) {
			_, writeErr := fmt.Fprintln(stderr, "NOTICE: root is outside a git repository; repository-copy and registered-worktree facts are unavailable")
			return facts, writeErr
		}
		return facts, fmt.Errorf("inspect git repository for %s: %w", root, err)
	}
	top = strings.TrimSpace(top)
	if top == "" {
		return facts, errors.New("git rev-parse recognized a repository but returned an empty top level")
	}
	facts.repository = true
	facts.top = canonicalPath(top)

	trackedUnder, err := trackedPathsBelow(ctx, facts.top, root)
	if err != nil {
		return facts, err
	}
	if len(trackedUnder) > 0 {
		return facts, fmt.Errorf("refusing to organize %s: git tracks content at or below the requested root (%s)", root, strings.Join(firstN(trackedUnder, examplesShown), ", "))
	}

	files, err := runGit(ctx, facts.top, "ls-files", "-z")
	if err != nil {
		return facts, fmt.Errorf("list tracked repository files: %w", err)
	}
	for _, path := range strings.Split(files, "\x00") {
		if path != "" {
			facts.tracked[filepath.Base(path)] = struct{}{}
		}
	}
	list, err := runGit(ctx, root, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return facts, fmt.Errorf("list registered git worktrees: %w", err)
	}
	for path := range parseWorktreeList(list) {
		facts.worktrees[path] = struct{}{}
	}
	return facts, nil
}

func trackedPathsBelow(ctx context.Context, top, root string) ([]string, error) {
	rel, err := filepath.Rel(top, canonicalPath(root))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("resolve requested root %s relative to git top level %s", root, top)
	}
	literal := ":(literal)" + filepath.ToSlash(rel)
	out, err := runGit(ctx, top, "ls-files", "-z", "--", literal)
	if err != nil {
		return nil, fmt.Errorf("check tracked content below requested root: %w", err)
	}
	var paths []string
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			paths = append(paths, filepath.ToSlash(path))
		}
	}
	return paths, nil
}

type gitCommandError struct {
	cause  error
	stderr string
}

func (e *gitCommandError) Error() string {
	if e.stderr == "" {
		return e.cause.Error()
	}
	return fmt.Sprintf("%v: %s", e.cause, e.stderr)
}

func (e *gitCommandError) Unwrap() error { return e.cause }

func isNotGitRepository(err error) bool {
	var commandErr *gitCommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	var exit *exec.ExitError
	return errors.As(commandErr.cause, &exit) && exit.ExitCode() == 128 &&
		strings.Contains(commandErr.stderr, "fatal: not a git repository")
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands in caller-selected directories.
	env := gitenv.WithoutDiscovery()
	stable := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, "LC_ALL=") {
			stable = append(stable, item)
		}
	}
	stable = append(stable, "LC_ALL=C")
	command.Env = stable
	out, err := command.Output()
	if err != nil {
		stderr := ""
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			stderr = strings.TrimSpace(string(exit.Stderr))
		}
		return "", &gitCommandError{cause: err, stderr: stderr}
	}
	return string(out), nil
}

var runGit = gitOutput

func parseWorktreeList(output string) map[string]string {
	registrations := map[string]string{}
	current := ""
	for _, field := range strings.Split(output, "\x00") {
		for _, line := range strings.Split(field, "\n") {
			switch {
			case strings.HasPrefix(line, worktreeLinePrefix):
				current = canonicalPath(strings.TrimPrefix(line, worktreeLinePrefix))
				registrations[current] = ""
			case current != "" && strings.HasPrefix(line, "prunable"):
				registrations[current] = strings.TrimSpace(strings.TrimPrefix(line, "prunable"))
			}
		}
	}
	return registrations
}

func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return resolved
}

// canonicalComparisonPath resolves the nearest existing parent while leaving
// the final path component untouched. This normalizes aliases such as macOS's
// /tmp and /private/tmp without dereferencing or rewriting the link being
// evaluated, and it also works for planned destinations that do not yet exist.
func canonicalComparisonPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	parent := filepath.Dir(abs)
	suffix := []string{filepath.Base(abs)}
	for {
		resolved, resolveErr := filepath.EvalSymlinks(parent)
		if resolveErr == nil {
			parts := append([]string{resolved}, suffix...)
			return filepath.Clean(filepath.Join(parts...))
		}
		next := filepath.Dir(parent)
		if next == parent {
			return filepath.Clean(abs)
		}
		suffix = append([]string{filepath.Base(parent)}, suffix...)
		parent = next
	}
}

func displayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, args ...any) {
	if e.err != nil {
		return
	}
	_, e.err = fmt.Fprintf(e.w, format, args...)
}
