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
	// reviewDocName is the human-facing review list written into the root.
	reviewDocName = "ORGANIZE-REVIEW.md"
	// destColumnWidth aligns the destination column of the plan summary.
	destColumnWidth = 38
	// worktreeLinePrefix introduces a path in `git worktree list --porcelain`.
	worktreeLinePrefix = "worktree "

	createdDirPerm = 0o755
	reviewDocPerm  = 0o644
)

// Options configures a single organize run.
type Options struct {
	// Root is the directory to organize. It must already exist.
	Root string
	// Apply performs the moves. When false the run is a dry run that prints the
	// plan and writes the review list without touching a single entry.
	Apply bool
	// Keep lists top-level names to leave in place, for tool-managed paths whose
	// location other software resolves on its own.
	Keep []string
	// MoveWorktrees also relocates registered git worktrees, repairing their
	// registration afterwards. Off by default: moving a worktree edits real git
	// state, which is not something a tidy-up run should do unasked.
	MoveWorktrees bool
	// MinGroup is how many entries a filename prefix needs before it earns its
	// own reports folder. See DefaultMinGroup.
	MinGroup int
}

// resolveRoot validates the options and returns the absolute root directory.
func (o Options) resolveRoot() (string, error) {
	if strings.TrimSpace(o.Root) == "" {
		return "", errors.New("a root directory is required")
	}
	if o.MinGroup < 1 {
		return "", fmt.Errorf("min group must be a positive integer, got %d", o.MinGroup)
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

// Run organizes the scratch directory named by opts, reporting the plan on
// stdout, warnings about disabled checks on stderr, and the entries that still
// need a human decision in ORGANIZE-REVIEW.md inside the root.
//
// Nothing is ever deleted, overwritten, or merged: an entry whose destination
// path is already taken is reported as a collision and left where it is.
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

	classifyCtx := classifyContext{
		skillPrefixes: skillPrefixes(entries, opts.MinGroup),
		worktrees:     git.worktrees,
		tracked:       git.tracked,
	}
	plan := make([]placement, 0, len(entries))
	for _, item := range entries {
		plan = append(plan, classify(item, classifyCtx))
	}

	// Registered worktrees are left alone unless explicitly requested, because
	// relocating one rewrites git's worktree registration.
	movable := make([]placement, 0, len(plan))
	var skipped []placement
	for _, candidate := range plan {
		if candidate.worktree && !opts.MoveWorktrees {
			skipped = append(skipped, candidate)
			continue
		}
		movable = append(movable, candidate)
	}

	if err := printPlan(stdout, len(entries), movable, skipped); err != nil {
		return err
	}

	reviewPath := filepath.Join(root, reviewDocName)

	if !opts.Apply {
		if _, err := fmt.Fprintln(stdout, "\nDRY RUN — nothing moved. Re-run with --apply."); err != nil {
			return err
		}
		if err := writeReviewDoc(reviewPath, plan, skipped, false); err != nil {
			return err
		}
		// A dry run moves nothing, so nothing can collide; skipped is the whole
		// left-in-place set.
		// Say where it landed: a dry run still leaves this file behind.
		_, err := fmt.Fprintf(stdout, "review list: %s\n", reviewPath)
		return err
	}

	moved, collisions, err := applyMoves(root, movable)
	// Written the moment the moves stop, whether they all succeeded or one failed
	// partway. Everything below can fail on a closed output stream — `al
	// organize-scratch --apply | head` is enough — and a partial move with no
	// record of what moved is the worst outcome this command can produce.
	if writeErr := writeReviewDoc(reviewPath, plan, leftInPlace(skipped, collisions), true); writeErr != nil && err == nil {
		err = writeErr
	}
	if err != nil {
		return err
	}
	// Only entries that actually moved are repaired. A worktree left behind by a
	// collision still has a correct registration, and repairing it would point
	// git at whatever already occupies the destination.
	if opts.MoveWorktrees {
		if err := repairWorktrees(ctx, root, moved, stderr); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(stdout, "\nmoved %d/%d\n", len(moved), len(movable)); err != nil {
		return err
	}
	warn := &errWriter{w: stderr}
	for _, collision := range collisions {
		warn.printf("COLLISION, left in place: %s -> %s\n", collision.name, collision.dest)
	}
	if warn.err != nil {
		return warn.err
	}
	_, err = fmt.Fprintf(stdout, "review list: %s\n", reviewPath)
	return err
}

// leftInPlace merges the entries that were deliberately skipped with the ones a
// collision blocked, so the review document records everything still sitting at
// the root. Stderr carries the same collisions, but stderr is transient and this
// document is the durable record of the run.
func leftInPlace(skipped, collisions []placement) []placement {
	combined := make([]placement, 0, len(skipped)+len(collisions))
	combined = append(combined, skipped...)
	for _, blocked := range collisions {
		blocked.reason = fmt.Sprintf("destination `%s/%s` was already taken, so it was not moved (was: %s)",
			blocked.dest, blocked.name, blocked.reason)
		combined = append(combined, blocked)
	}
	return combined
}

// reservedNames returns the top-level names the run must not touch: its own
// destination folders, its review list, and anything the caller asked to keep.
func reservedNames(keep []string) map[string]struct{} {
	reserved := newSet("reports", "artifacts", "review", reviewDocName)
	for _, name := range keep {
		// Trim here rather than trusting the caller: a stray space in a kept name
		// silently fails to match a directory entry and moves the very path the
		// caller asked to protect.
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			reserved[trimmed] = struct{}{}
		}
	}
	return reserved
}

// readTopLevel lists the entries of root that are eligible to move, in name
// order. A symlink is reported as a file, so it is relocated as an opaque entry
// rather than walked.
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
		entries = append(entries, newEntry(root, canonicalRoot, child.Name(), child.IsDir()))
	}
	return entries, nil
}

// skillPrefixes returns the filename prefixes that earn their own reports
// folder. A prefix qualifies only when it recurs at least minGroup times AND
// produced markdown; that keeps pure data prefixes from masquerading as agent
// reports.
func skillPrefixes(entries []entry, minGroup int) map[string]struct{} {
	total := map[string]int{}
	markdown := map[string]int{}
	for _, item := range entries {
		if strings.HasPrefix(item.name, ".") {
			continue
		}
		prefix := prefixOf(item.name)
		total[prefix]++
		if !item.isDir && extOf(item.name) == "md" {
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

// printPlan writes the destination summary, ordered by how many entries each
// destination receives.
func printPlan(w io.Writer, total int, movable, skipped []placement) error {
	counts := map[string]int{}
	order := make([]string, 0, len(movable))
	for _, candidate := range movable {
		if _, seen := counts[candidate.dest]; !seen {
			order = append(order, candidate.dest)
		}
		counts[candidate.dest]++
	}
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })

	out := &errWriter{w: w}
	out.printf("%d entries — %d to move, %d left in place\n\n", total, len(movable), len(skipped))
	out.printf("%-*s %s\n", destColumnWidth, "DESTINATION", "COUNT")
	for _, dest := range order {
		out.printf("%-*s %d\n", destColumnWidth, dest, counts[dest])
	}
	for _, left := range skipped {
		out.printf("\nLEFT IN PLACE  %s\n   %s (pass --move-worktrees to relocate)\n", left.name, left.reason)
	}
	return out.err
}

// applyMoves relocates each planned entry, returning the entries that actually
// moved and the ones that could not. A destination path that already exists is a
// collision: the entry stays where it is, because overwriting is exactly what
// this tool promises never to do.
func applyMoves(root string, movable []placement) (moved, collisions []placement, err error) {
	for _, candidate := range movable {
		destDir := filepath.Join(root, candidate.dest)
		if mkErr := os.MkdirAll(destDir, createdDirPerm); mkErr != nil {
			return moved, collisions, fmt.Errorf("create %s: %w", destDir, mkErr)
		}
		target := filepath.Join(destDir, candidate.name)
		// Lstat, not Stat: a dangling symlink at the target is still something
		// a rename would destroy.
		//
		// This is a check followed by a rename, not one atomic operation: a
		// process that creates target in between would have its file replaced.
		// Go exposes no portable no-replace rename (RENAME_NOREPLACE and
		// RENAME_EXCL are per-platform syscalls, and os.Link cannot move a
		// directory), so the guarantee holds against earlier runs and existing
		// files, not against another writer racing this one inside the scratch
		// root.
		if _, statErr := os.Lstat(target); statErr == nil {
			collisions = append(collisions, candidate)
			continue
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return moved, collisions, fmt.Errorf("inspect %s: %w", target, statErr)
		}
		if renameErr := os.Rename(candidate.abs, target); renameErr != nil {
			return moved, collisions, fmt.Errorf("move %s to %s: %w", candidate.name, candidate.dest, renameErr)
		}
		moved = append(moved, candidate)
	}
	return moved, collisions, nil
}

// repairWorktrees points git at the new location of every relocated worktree and
// then verifies that no registration is left dangling. It must be given only the
// entries that actually moved.
func repairWorktrees(ctx context.Context, root string, moved []placement, stderr io.Writer) error {
	warn := &errWriter{w: stderr}
	for _, relocated := range moved {
		if !relocated.worktree {
			continue
		}
		movedTo := filepath.Join(root, relocated.dest, relocated.name)
		// Repair the entry itself, or every worktree nested beneath it. Git
		// needs each real worktree path; pointing it at a parent directory
		// leaves the nested ones registered at their old location as prunable.
		targets := []string{movedTo}
		if len(relocated.nested) > 0 {
			targets = nil
			for _, rel := range relocated.nested {
				targets = append(targets, filepath.Join(movedTo, rel))
			}
		}
		for _, target := range targets {
			if _, err := gitOutput(ctx, root, "worktree", "repair", target); err != nil {
				warn.printf("WARNING: git worktree repair failed for %s — run it by hand\n", displayPath(root, target))
			}
		}
	}
	// Surface any registration still pointing at a missing path. A root outside
	// a repository has nothing to verify.
	if list, err := gitOutput(ctx, root, "worktree", "list"); err == nil {
		stale := 0
		for _, line := range strings.Split(list, "\n") {
			if strings.Contains(line, "prunable") {
				stale++
			}
		}
		if stale > 0 {
			warn.printf("WARNING: %d worktree registration(s) still stale — run `git worktree repair`\n", stale)
		}
	}
	return warn.err
}

// reviewGuides tell the reader what each review destination demands of them.
var reviewGuides = map[string]string{
	destReviewCheckouts:    "Confirm no unmerged or unpushed work before removing (`git status`, `git log @{u}..HEAD`, diff against main).",
	destReviewRegenerable:  "Reproducible from a package manager, a build, or the repo itself — usually safe to remove.",
	destReviewUniqueAssets: "Contains authored files not tracked in the repo. Check each before removing; these may exist nowhere else.",
	destReviewBulkSamples:  "Mostly machine-generated samples. Extract any written analysis first, then remove the bulk.",
	destReviewSecrets:      "Possible key material. Remove regardless of value, after confirming the credential is revoked.",
	destReviewUnknown:      "Could not be classified — inspect by hand.",
}

// writeReviewDoc records what landed where, and what still needs a decision.
func writeReviewDoc(path string, plan, skipped []placement, applied bool) error {
	grouped := map[string][]placement{}
	needsReview := 0
	for _, candidate := range plan {
		if !strings.HasPrefix(candidate.dest, reviewPrefix) {
			continue
		}
		grouped[candidate.dest] = append(grouped[candidate.dest], candidate)
		needsReview++
	}
	dests := make([]string, 0, len(grouped))
	for dest := range grouped {
		dests = append(dests, dest)
	}
	sort.Strings(dests)

	var doc strings.Builder
	doc.WriteString("# Scratch organization review\n\n")
	if applied {
		doc.WriteString("Entries have been moved.\n\n")
	} else {
		doc.WriteString("DRY RUN — nothing was moved yet.\n\n")
	}
	doc.WriteString("This command only moves files. Nothing here has been deleted; every removal\n" +
		"decision below is yours. Folders outside `review/` were routed by an\n" +
		"unambiguous rule and need no attention.\n\n")

	if len(skipped) > 0 {
		doc.WriteString("## Left in place\n\n")
		for _, left := range skipped {
			fmt.Fprintf(&doc, "- `%s` — %s\n", left.name, left.reason)
		}
		doc.WriteString("\n")
	}

	doc.WriteString("## Needs review\n\n")
	if needsReview == 0 {
		doc.WriteString("Nothing required a judgement call.\n\n")
	}
	for _, dest := range dests {
		items := grouped[dest]
		fmt.Fprintf(&doc, "### `%s` (%d)\n\n", dest, len(items))
		if guide, ok := reviewGuides[dest]; ok {
			doc.WriteString(guide + "\n\n")
		}
		for _, item := range items {
			fmt.Fprintf(&doc, "- `%s` — %s\n", item.name, item.reason)
		}
		doc.WriteString("\n")
	}
	// Exactly one trailing newline, however many blank separators the sections
	// above contributed.
	body := strings.TrimRight(doc.String(), "\n") + "\n"
	return fsutil.WriteFileAtomic(path, []byte(body), reviewDocPerm)
}

// gitFacts is what git knows about the scratch root. A field left empty disables
// the checks that depend on it, which is always announced on stderr rather than
// applied silently.
type gitFacts struct {
	worktrees map[string]struct{}
	tracked   map[string]struct{}
}

func gatherGitFacts(ctx context.Context, root string, stderr io.Writer) (gitFacts, error) {
	facts := gitFacts{worktrees: map[string]struct{}{}, tracked: map[string]struct{}{}}
	warn := &errWriter{w: stderr}

	top, err := gitOutput(ctx, root, "rev-parse", "--show-toplevel")
	top = strings.TrimSpace(top)
	if err != nil || top == "" {
		warn.printf("WARNING: not a git repository; copy, asset, and worktree detection are disabled\n")
		return facts, warn.err
	}

	// `git ls-files` scopes to the current directory, and a scratch dir is
	// typically gitignored, so this must run from the repository top level.
	// Running it in place silently yields an empty set — which would quietly
	// disable copy detection.
	// -z, because `git ls-files` applies core.quotePath by default: without it a
	// path holding non-ASCII characters arrives as "\303\251tude.md" and its
	// basename never matches the file on disk, silently shrinking the tracked set
	// that copy and asset detection compare against.
	if files, lsErr := gitOutput(ctx, top, "ls-files", "-z"); lsErr != nil {
		warn.printf("WARNING: git ls-files failed (%v); copy and asset detection are disabled\n", lsErr)
	} else {
		for _, line := range strings.Split(files, "\x00") {
			if line != "" {
				facts.tracked[filepath.Base(line)] = struct{}{}
			}
		}
		if len(facts.tracked) == 0 {
			warn.printf("WARNING: git reported no tracked files; copy and asset detection are disabled\n")
		}
	}

	// -z for the same reason, and because the porcelain format otherwise quotes a
	// worktree path containing unusual characters. An unparsed path would leave
	// that worktree unrecognised and moved without repair.
	if list, wtErr := gitOutput(ctx, root, "worktree", "list", "--porcelain", "-z"); wtErr != nil {
		warn.printf("WARNING: git worktree list failed (%v); registered worktrees are not protected\n", wtErr)
	} else {
		for _, line := range strings.Split(list, "\x00") {
			if strings.HasPrefix(line, worktreeLinePrefix) {
				facts.worktrees[canonicalPath(strings.TrimPrefix(line, worktreeLinePrefix))] = struct{}{}
			}
		}
	}
	return facts, warn.err
}

// gitOutput runs git inside dir and returns its standard output. Errors are
// returned so each caller can decide whether missing git information matters.
//
// The repository is always resolved from dir, never from an inherited GIT_DIR
// (see gitenv): running inside a git hook, an ambient GIT_DIR would silently
// redirect every read — and the `worktree repair` writes — at a different
// repository than the scratch root the caller named.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git subcommands; dir is the caller's own directory.
	command.Env = gitenv.WithoutDiscovery()
	out, err := command.Output()
	if err != nil {
		// Without this a caller's warning reads "exit status 128", which does not
		// say why a check was disabled.
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

// canonicalPath resolves symlinks so that a path git reports and a path built
// from the scratch root compare equal. On macOS /tmp is a symlink to
// /private/tmp, and a mismatch here would relocate a registered worktree without
// repairing it. A path that cannot be resolved — a registration whose directory
// is already gone — is returned absolute, which is the spelling git itself
// reported.
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

// displayPath shortens a path for a warning, falling back to the absolute form.
func displayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

// errWriter collapses a run of writes into a single error check, so reporting
// code stays readable without dropping I/O failures.
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
