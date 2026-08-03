// Package organizescratch sorts a scratch directory into a reviewable
// structure.
//
// It NEVER deletes, overwrites, or merges anything. It only moves top-level
// entries into destination folders, and it refuses to move an entry whose
// destination path is already taken. Deletion is always a separate,
// human-driven step — that separation is the whole point of this tool.
//
// The destinations encode how much review an entry needs, so a later cleanup
// pass can skip whole folders:
//
//	reports/<skill>/   no review — matched a strict skill naming convention
//	artifacts/<kind>/  no review — routed purely by file extension
//	review/...         needs a human decision, subdivided by the reason why
//
// Design note: an earlier version of this logic classified a directory as
// disposable when it contained no Markdown. That rule was wrong and nearly
// discarded a set of logo SVGs that existed nowhere else. Value is judged here
// by reproducibility — whether a tree is a copy of the repo, a dependency
// install, or a build cache — not by whether an agent happened to write prose
// next to it.
package organizescratch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Destinations. Every value is a slash-separated path relative to the scratch
// root; `review/` prefixes the ones that need a human decision.
const (
	destReviewSecrets        = "review/secrets"
	destReviewCheckouts      = "review/checkouts"
	destReviewRegenerable    = "review/regenerable"
	destReviewUniqueAssets   = "review/unique-assets"
	destReviewBulkSamples    = "review/bulk-samples"
	destReviewUnknown        = "review/unknown"
	destArtifactsLogs        = "artifacts/logs"
	destArtifactsScreenshots = "artifacts/screenshots"
	destArtifactsDiffs       = "artifacts/diffs"
	destArtifactsScripts     = "artifacts/scripts"
	destArtifactsData        = "artifacts/data"
	destArtifactsEvidence    = "artifacts/evidence"

	reviewPrefix       = "review/"
	reportsPrefix      = "reports/"
	reportsAdhocPrefix = "reports/adhoc/"

	// Ad-hoc Markdown topic folders. adhocFallbackFolder receives anything that
	// matches no topic rule.
	adhocFolderPR         = "pr"
	adhocFolderIncidents  = "incidents"
	adhocFolderReviews    = "reviews"
	adhocFolderPlansSpecs = "plans-specs"
	adhocFallbackFolder   = "misc"

	reasonSkillConvention = "skill naming convention"
)

// Bounds that keep classification cheap on very large trees.
const (
	// scanFileLimit stops a walk so that a 100k-file dependency tree does not
	// stall classification.
	scanFileLimit = 4000
	// scanNameSample is how many filenames a walk retains for the name-based
	// heuristics.
	scanNameSample = 400
	// repoCopySample is how many retained filenames the repo-copy check reads.
	repoCopySample = 300
	// privateKeyProbeBytes is how much of a candidate key file is inspected for
	// a PEM private key header.
	privateKeyProbeBytes = 4096
	// vendoredMinFiles is the file count above which a JS-dominated tree is
	// treated as a dependency install.
	vendoredMinFiles = 500
	// vendoredJSRatio is the share of files that must be JS/TS for the same.
	vendoredJSRatio = 0.5
	// repoCopyMinFiles is the file count below which repo-copy detection is
	// pointless.
	repoCopyMinFiles = 50
	// repoCopyRatio is the share of sampled filenames that must also be tracked
	// in the repo before a tree counts as a copy of it.
	repoCopyRatio = 0.6
	// bulkSamplesMinFiles and bulkSamplesMaxTypes describe the signature of a
	// machine-generated sample dump.
	bulkSamplesMinFiles = 300
	bulkSamplesMaxTypes = 6
	// examplesShown caps how many names a reason string enumerates.
	examplesShown = 3
)

var (
	logExt   = newSet("log", "out", "err", "stdout", "stderr")
	imageExt = newSet("png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff")
	diffExt  = newSet("diff", "patch")
	codeExt  = newSet("mjs", "cjs", "js", "ts", "tsx", "sh", "py", "go", "rb", "sql")
	// assetExt lists design/source assets that are typically authored, not
	// generated. Fonts are deliberately excluded: web fonts in a scratch tree
	// are downloaded or build-generated subsets, never hand-authored, and
	// including them floods the review list with false positives.
	assetExt = newSet("svg", "psd", "ai", "sketch", "fig", "xcf", "eps")
)

var (
	// extSuffix and noiseSuffix strip trailing artifact-type and timestamp
	// segments so that topic patterns anchored with `$` can match. Without
	// this, "foo-pr-body.md" never matches a /pr-body$/ rule.
	extSuffix   = regexp.MustCompile(`(?i)\.[a-z0-9]+$`)
	noiseSuffix = regexp.MustCompile(`(?i)\.(prompt|report|plan|task|context|findings|output|evidence|summary|bak|\d{8}[-\dT]*[a-z0-9-]*)$`)

	// toolAsset matches assets shipped by tooling (Playwright reports, editor
	// icon fonts), not by us.
	toolAsset = regexp.MustCompile(`(?i)(^(playwright-logo|codicon|favicon)|^[0-9a-f]{32,}\.)`)
	// secretName matches filenames that may indicate key material; content
	// decides (see privateKeyVerdict).
	secretName = regexp.MustCompile(`(?i)((^|[._-])(id_rsa|id_ed25519|id_ecdsa)$|\.(pem|key|p12|pfx|keystore)$)`)
	// secretNameStrong matches filenames strong enough to flag even when the
	// content cannot be read.
	secretNameStrong = regexp.MustCompile(`(?i)((^|[._-])(id_rsa|id_ed25519|id_ecdsa)$|\.(p12|pfx|keystore)$)`)
	privateKeyBlock  = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)

	nodeModulesDir = regexp.MustCompile(`^node_modules$`)
	cacheDir       = regexp.MustCompile(`(?i)(^|[-_.])(cache|caches)$`)
	archiveName    = regexp.MustCompile(`(?i)\.(tgz|tar\.gz|zip)$`)
)

// adhocRules route ad-hoc Markdown by topic. The first matching rule wins;
// anything unmatched lands in adhocFallbackFolder.
var adhocRules = []struct {
	folder  string
	pattern *regexp.Regexp
}{
	{adhocFolderPR, regexp.MustCompile(`(?i)((^|[-_])pr[-_]?\d|pr-body$|^pr-|^reply|[-_]reply[-_]|^ship-pr|^codex-reply|-pr$|^merge-pr|^reconcile-pr)`)},
	{adhocFolderIncidents, regexp.MustCompile(`(?i)(postmortem|incident|^sentry-|root-cause|error-report|failure-evidence|triage)`)},
	{adhocFolderReviews, regexp.MustCompile(`(?i)(review|audit|verif|findings|retrospective|second-opinion|certif|readiness)`)},
	{adhocFolderPlansSpecs, regexp.MustCompile(`(?i)(plan|spec|roadmap|arch|design|runbook|handoff|context|proposal|brief|checklist|migration)`)},
}

// entry is one top-level member of the scratch root.
type entry struct {
	name  string
	isDir bool
	abs   string
	// registered is the path used to compare this entry against git's worktree
	// registrations. See newEntry for why it is not simply abs.
	registered string
}

// newEntry builds a top-level entry of root. canonicalRoot is root with its
// symlinks resolved, which is how git spells the paths it reports: on macOS a
// caller who says /tmp/scratch gets entries under /private/tmp/scratch from git,
// and comparing the two spellings directly would make a registered worktree look
// unregistered — moving it and silently breaking its registration.
func newEntry(root, canonicalRoot, name string, isDir bool) entry {
	return entry{
		name:       name,
		isDir:      isDir,
		abs:        filepath.Join(root, name),
		registered: filepath.Join(canonicalRoot, name),
	}
}

// placement records where one entry belongs and why.
type placement struct {
	entry
	dest   string
	reason string
	// worktree marks an entry whose relocation would edit git's worktree
	// registration.
	worktree bool
	// nested holds the registered worktrees below this entry, relative to it,
	// so repair can target each real worktree path.
	nested []string
}

// classifyContext holds the facts shared by every classification in one run.
type classifyContext struct {
	skillPrefixes map[string]struct{}
	worktrees     map[string]struct{}
	tracked       map[string]struct{}
}

func newSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func inSet(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

// extOf returns the lowercased text after the final dot, or "" when there is no
// dot at all.
func extOf(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return ""
	}
	return strings.ToLower(name[dot+1:])
}

// prefixOf returns the text before the first dot, which is the naming-convention
// prefix agents use for their report families.
func prefixOf(name string) string {
	if dot := strings.Index(name, "."); dot >= 0 {
		return name[:dot]
	}
	return name
}

// stemOf strips the extension and any trailing artifact-type or timestamp
// segments, leaving the topic portion of a filename.
func stemOf(name string) string {
	stem := extSuffix.ReplaceAllString(name, "")
	for {
		next := noiseSuffix.ReplaceAllString(stem, "")
		if next == stem {
			return stem
		}
		stem = next
	}
}

// keyVerdict is the tri-state answer to "does this file hold private key
// material", because "cannot tell" must not be reported as "no".
type keyVerdict int

const (
	keyUnreadable keyVerdict = iota
	keyAbsent
	keyPresent
)

// privateKeyVerdict reports whether a file actually contains private key
// material. A `.pem` is just as often a public certificate, so the filename
// alone must not decide.
func privateKeyVerdict(file string) keyVerdict {
	handle, err := os.Open(file) // #nosec G304 -- a top-level entry of the caller's own scratch root.
	if err != nil {
		return keyUnreadable
	}
	defer func() { _ = handle.Close() }()
	buffer := make([]byte, privateKeyProbeBytes)
	// ReadFull, not a bare Read: a single Read may return a short prefix of a
	// readable file, which could split the PEM header and clear a real key.
	read, err := io.ReadFull(handle, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return keyUnreadable
	}
	if privateKeyBlock.Match(buffer[:read]) {
		return keyPresent
	}
	return keyAbsent
}

// treeScan is the bounded sample of a directory tree that classification uses.
type treeScan struct {
	files int
	byExt map[string]int
	names []string
	// paths maps a candidate secret's filename to its full path so its content
	// can be checked. Unlike names it is not sampled, because a missed key is a
	// safety failure rather than a heuristic miss. Only the first path per
	// filename is kept, so two same-named candidates in different subdirectories
	// are judged by the first one seen.
	paths map[string]string
	// truncated records that the sample is partial, so callers never mistake a
	// bounded scan for a complete one.
	truncated  bool
	unreadable []string
}

// scanTree walks dir, stopping once limit files have been seen. The bound is
// checked between directories, so the count can overshoot by one directory's
// worth of entries. Unreadable directories are collected rather than ignored: a
// tree that cannot be inspected must not be classified as if it had been.
//
// os.ReadDir sorts each directory by name, so which files land in the retained
// sample — and therefore how a tree near a classification threshold is routed —
// is the same on every run and every filesystem.
func scanTree(dir string, limit int) treeScan {
	scan := treeScan{byExt: map[string]int{}, paths: map[string]string{}}
	stack := []string{dir}
	for len(stack) > 0 {
		if scan.files >= limit {
			scan.truncated = true
			break
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		children, err := os.ReadDir(current)
		if err != nil {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: %v", current, err))
			continue
		}
		for _, child := range children {
			full := filepath.Join(current, child.Name())
			switch {
			case child.IsDir():
				stack = append(stack, full)
			case child.Type().IsRegular():
				scan.files++
				scan.byExt[extOf(child.Name())]++
				if len(scan.names) < scanNameSample {
					scan.names = append(scan.names, child.Name())
				}
				if secretName.MatchString(child.Name()) {
					if _, seen := scan.paths[child.Name()]; !seen {
						scan.paths[child.Name()] = full
					}
				}
			}
		}
	}
	return scan
}

// looksVendored reports whether a tree looks like an installed dependency set
// or a build cache.
func looksVendored(name string, scan treeScan) bool {
	if nodeModulesDir.MatchString(name) {
		return true
	}
	if cacheDir.MatchString(name) {
		return true
	}
	if archiveName.MatchString(name) {
		return false
	}
	// A dependency install is overwhelmingly JS/TS.
	js := scan.byExt["js"] + scan.byExt["mjs"] + scan.byExt["ts"]
	if scan.files <= vendoredMinFiles {
		return false
	}
	return float64(js)/float64(scan.files) > vendoredJSRatio
}

// looksLikeRepoCopy detects a tree that merely restates the repository, e.g. a
// clone or a fixture copy. Compared by basename against the repo's tracked
// files: cheap, and good enough to separate "copy of our source" from "unique
// artifacts".
func looksLikeRepoCopy(scan treeScan, tracked map[string]struct{}) bool {
	if scan.files < repoCopyMinFiles || len(tracked) == 0 {
		return false
	}
	sample := scan.names
	if len(sample) > repoCopySample {
		sample = sample[:repoCopySample]
	}
	if len(sample) == 0 {
		return false
	}
	hits := 0
	for _, name := range sample {
		if inSet(tracked, name) {
			hits++
		}
	}
	return float64(hits)/float64(len(sample)) > repoCopyRatio
}

// uniqueAssetCount counts files that plausibly cannot be regenerated: authored
// assets that are neither tracked in the repo nor shipped by tooling.
func uniqueAssetCount(scan treeScan, tracked map[string]struct{}) int {
	count := 0
	for _, name := range scan.names {
		if !inSet(assetExt, extOf(name)) {
			continue
		}
		if inSet(tracked, name) || toolAsset.MatchString(name) {
			continue
		}
		count++
	}
	return count
}

// findSecrets returns the names within a tree that hold, or may hold, private
// key material.
//
// This walks scan.paths, not the scanNameSample-bounded scan.names: every
// candidate the walk saw is recorded in paths, so a key whose filename sorts
// past the sample is still inspected. Missing one would route a directory
// holding a private key to a destination the review list calls unambiguous.
func findSecrets(scan treeScan) []string {
	candidates := make([]string, 0, len(scan.paths))
	for name := range scan.paths {
		candidates = append(candidates, name)
	}
	sort.Strings(candidates)

	var hits []string
	for _, name := range candidates {
		if secretNameStrong.MatchString(name) {
			hits = append(hits, name)
			continue
		}
		// `.pem`/`.key` are ambiguous — confirm from content, and flag
		// unreadable files rather than assuming they are harmless.
		if privateKeyVerdict(scan.paths[name]) != keyAbsent {
			hits = append(hits, name)
		}
	}
	return hits
}

// classify decides where one top-level entry belongs.
func classify(item entry, ctx classifyContext) placement {
	if item.isDir {
		return classifyDir(item, ctx)
	}
	return classifyFile(item, ctx)
}

func classifyFile(item entry, ctx classifyContext) placement {
	if secretName.MatchString(item.name) {
		if secretNameStrong.MatchString(item.name) {
			return place(item, destReviewSecrets, "filename indicates private key material")
		}
		// Confirm from content: a `.pem` is as often a public certificate.
		verdict := privateKeyVerdict(item.abs)
		if verdict == keyUnreadable {
			return place(item, destReviewSecrets, "possible key material, unreadable — check by hand")
		}
		if verdict == keyPresent {
			return place(item, destReviewSecrets, "contains PRIVATE KEY block")
		}
		// Public certificate: keep it, but note why it was not flagged.
		return place(item, destArtifactsData, "certificate/public key material, no private key block")
	}
	if inSet(ctx.skillPrefixes, prefixOf(item.name)) {
		return place(item, reportsPrefix+prefixOf(item.name), reasonSkillConvention)
	}
	ext := extOf(item.name)
	switch {
	case ext == "md":
		return place(item, reportsAdhocPrefix+adhocFolder(stemOf(item.name)), "ad-hoc markdown")
	case inSet(logExt, ext):
		return place(item, destArtifactsLogs, "log extension")
	case inSet(imageExt, ext):
		return place(item, destArtifactsScreenshots, "image extension")
	case inSet(diffExt, ext):
		return place(item, destArtifactsDiffs, "diff extension")
	case inSet(assetExt, ext):
		return place(item, destReviewUniqueAssets, "authored asset, may be irreplaceable")
	case inSet(codeExt, ext):
		return place(item, destArtifactsScripts, "script extension")
	}
	return place(item, destArtifactsData, "data file")
}

// adhocFolder returns the topic folder for an ad-hoc Markdown stem.
func adhocFolder(stem string) string {
	for _, rule := range adhocRules {
		if rule.pattern.MatchString(stem) {
			return rule.folder
		}
	}
	return adhocFallbackFolder
}

func classifyDir(item entry, ctx classifyContext) placement {
	// Git state is checked before scanning: no amount of content changes the
	// fact that moving a registered worktree rewrites git's registration.
	if inSet(ctx.worktrees, item.registered) {
		return placement{
			entry:    item,
			dest:     destReviewCheckouts,
			reason:   "REGISTERED git worktree — moving edits git state",
			worktree: true,
		}
	}
	// A registered worktree may sit below this entry (e.g. `worktrees/<branch>`).
	// Moving the parent silently breaks that registration, so treat it the same.
	if nested := nestedWorktrees(item.registered, ctx.worktrees); len(nested) > 0 {
		return placement{
			entry: item,
			dest:  destReviewCheckouts,
			reason: fmt.Sprintf("contains %d registered git worktree(s): %s",
				len(nested), strings.Join(firstN(nested, examplesShown), ", ")),
			worktree: true,
			nested:   nested,
		}
	}

	scan := scanTree(item.abs, scanFileLimit)
	return discloseScanBounds(classifyScannedDir(item, ctx, scan), scan)
}

// discloseScanBounds records that a classification rests on a partial walk, so a
// bounded sample is never presented to the reader as a complete one.
func discloseScanBounds(result placement, scan treeScan) placement {
	if scan.truncated {
		result.reason += fmt.Sprintf(" (judged from the first %d files; the tree is larger)", scanFileLimit)
	}
	return result
}

func classifyScannedDir(item entry, ctx classifyContext, scan treeScan) placement {
	if len(scan.unreadable) > 0 {
		return place(item, destReviewUnknown, fmt.Sprintf("unreadable paths (%d) — classify by hand", len(scan.unreadable)))
	}
	if secrets := findSecrets(scan); len(secrets) > 0 {
		return place(item, destReviewSecrets, "contains possible key material: "+strings.Join(firstN(secrets, examplesShown), ", "))
	}
	if _, err := os.Stat(filepath.Join(item.abs, ".git")); err == nil {
		return place(item, destReviewCheckouts, "git clone — check for unmerged work before removing")
	}
	if looksVendored(item.name, scan) {
		return place(item, destReviewRegenerable, "dependency install or build cache — reinstallable")
	}
	if looksLikeRepoCopy(scan, ctx.tracked) {
		return place(item, destReviewRegenerable, "copy of tracked repo files — reproducible")
	}
	if unique := uniqueAssetCount(scan, ctx.tracked); unique > 0 {
		return place(item, destReviewUniqueAssets, fmt.Sprintf("%d authored asset(s) not tracked in the repo", unique))
	}
	if inSet(ctx.skillPrefixes, prefixOf(item.name)) {
		return place(item, reportsPrefix+prefixOf(item.name), reasonSkillConvention)
	}
	// Many files but few distinct extensions is the signature of a sample dump:
	// the analysis is usually a handful of files buried in thousands of samples.
	if distinct := len(scan.byExt); scan.files > bulkSamplesMinFiles && distinct <= bulkSamplesMaxTypes {
		return place(item, destReviewBulkSamples, fmt.Sprintf("%d+ files across %d types — extract analysis before removing", scan.files, distinct))
	}
	return place(item, destArtifactsEvidence, "mixed evidence directory")
}

// place builds a placement that carries no git-worktree implications.
func place(item entry, dest, reason string) placement {
	return placement{entry: item, dest: dest, reason: reason}
}

// nestedWorktrees returns the registered worktrees below dir, as paths relative
// to dir, sorted so output does not depend on map iteration order.
func nestedWorktrees(dir string, worktrees map[string]struct{}) []string {
	prefix := dir + string(os.PathSeparator)
	var nested []string
	for worktree := range worktrees {
		if strings.HasPrefix(worktree, prefix) {
			nested = append(nested, strings.TrimPrefix(worktree, prefix))
		}
	}
	sort.Strings(nested)
	return nested
}

func firstN(values []string, limit int) []string {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}
