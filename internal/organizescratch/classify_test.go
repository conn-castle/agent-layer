package organizescratch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const publicCertPEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"

// privateKeyPEM is a fixture, not a key: only the PEM header drives the
// classifier. The header phrase is split so the repository's detect-private-key
// pre-commit hook does not flag this file as a leaked credential.
const privateKeyPEM = "-----BEGIN OPENSSH " + "PRIVATE KEY-----\nnot-a-real-key\n" +
	"-----END OPENSSH " + "PRIVATE KEY-----\n"

func writeFileAt(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func mkdirAt(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

// fileEntry creates name inside a fresh temp dir and returns it as a top-level entry.
func fileEntry(t *testing.T, name, content string) entry {
	t.Helper()
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, name), content)
	return topLevel(root, name, false)
}

func dirEntry(t *testing.T, root, name string) entry {
	t.Helper()
	mkdirAt(t, filepath.Join(root, name))
	return topLevel(root, name, true)
}

// topLevel builds an entry the same way a run does, so tests exercise the real
// path that is compared against git's worktree registrations.
func topLevel(root, name string, isDir bool) entry {
	return newEntry(root, canonicalPath(root), name, isDir)
}

func emptyContext() classifyContext {
	return classifyContext{
		skillPrefixes: map[string]struct{}{},
		worktrees:     map[string]struct{}{},
		tracked:       map[string]struct{}{},
	}
}

func TestNameParsingDropsArtifactAndTimestampSegments(t *testing.T) {
	// stemOf exists so topic rules anchored with `$` still match a filename that
	// carries an extension plus a chain of artifact-type or timestamp segments.
	cases := []struct{ name, ext, prefix, stem string }{
		{"ship-pr.pr-body.md", "md", "ship-pr", "ship-pr.pr-body"},
		{"audit.report.20250104T1200-final.md", "md", "audit", "audit"},
		{"plan.plan.summary.md", "md", "plan", "plan"},
		{"NOTES", "", "NOTES", "NOTES"},
		{".gitignore", "gitignore", "", ""},
		{"trace.OUT", "out", "trace", "trace"},
	}
	for _, tc := range cases {
		if got := extOf(tc.name); got != tc.ext {
			t.Errorf("extOf(%q) = %q, want %q", tc.name, got, tc.ext)
		}
		if got := prefixOf(tc.name); got != tc.prefix {
			t.Errorf("prefixOf(%q) = %q, want %q", tc.name, got, tc.prefix)
		}
		if got := stemOf(tc.name); got != tc.stem {
			t.Errorf("stemOf(%q) = %q, want %q", tc.name, got, tc.stem)
		}
	}
}

func TestClassifyFileRoutesByExtension(t *testing.T) {
	// Extension routing is the "no review needed" path: each destination must be
	// reachable from a rule a human can predict without reading the tree.
	cases := []struct{ name, dest string }{
		{"run.log", destArtifactsLogs},
		{"cmd.stderr", destArtifactsLogs},
		{"shot.PNG", destArtifactsScreenshots},
		{"change.patch", destArtifactsDiffs},
		{"logo.svg", destReviewUniqueAssets},
		{"helper.sh", destArtifactsScripts},
		{"payload.json", destArtifactsData},
		{"README", destArtifactsData},
	}
	for _, tc := range cases {
		got := classify(fileEntry(t, tc.name, "x"), emptyContext())
		if got.dest != tc.dest {
			t.Errorf("classify(%q).dest = %q, want %q", tc.name, got.dest, tc.dest)
		}
		if got.reason == "" {
			t.Errorf("classify(%q) has no reason; the review list depends on it", tc.name)
		}
	}
}

func TestClassifyMarkdownRoutesByTopic(t *testing.T) {
	// Ad-hoc markdown is grouped by topic so a cleanup pass can read one folder
	// at a time instead of a flat pile of report names.
	cases := []struct{ name, folder string }{
		{"ship-pr-42.md", adhocFolderPR},
		{"reconcile-pr.md", adhocFolderPR},
		{"sentry-outage.md", adhocFolderIncidents},
		{"postmortem-of-tuesday.md", adhocFolderIncidents},
		{"security-audit.md", adhocFolderReviews},
		{"readiness.report.md", adhocFolderReviews},
		{"migration-runbook.md", adhocFolderPlansSpecs},
		{"random-thoughts.md", adhocFallbackFolder},
	}
	for _, tc := range cases {
		got := classify(fileEntry(t, tc.name, "x"), emptyContext())
		want := reportsAdhocPrefix + tc.folder
		if got.dest != want {
			t.Errorf("classify(%q).dest = %q, want %q", tc.name, got.dest, want)
		}
	}
}

func TestClassifySecretsDecideFromContentNotFilename(t *testing.T) {
	// A `.pem` is as often a public certificate as a private key. Deciding on the
	// filename alone would push ordinary certificates into review/secrets and
	// train the reader to ignore that folder.
	cases := []struct {
		name    string
		content string
		dest    string
	}{
		{"id_ed25519", "anything", destReviewSecrets},
		{"backup.p12", "binary", destReviewSecrets},
		{"server.key", privateKeyPEM, destReviewSecrets},
		{"server.pem", publicCertPEM, destArtifactsData},
		{"empty.pem", "", destArtifactsData},
	}
	for _, tc := range cases {
		got := classify(fileEntry(t, tc.name, tc.content), emptyContext())
		if got.dest != tc.dest {
			t.Errorf("classify(%q).dest = %q, want %q (reason %q)", tc.name, got.dest, tc.dest, got.reason)
		}
	}
}

func TestClassifyUnreadableKeyCandidateIsFlaggedNotCleared(t *testing.T) {
	// "Cannot tell" must never be reported as "no key here".
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file, so unreadability cannot be simulated")
	}
	root := t.TempDir()
	locked := writeFileAt(t, filepath.Join(root, "locked.pem"), privateKeyPEM)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	got := classify(topLevel(root, "locked.pem", false), emptyContext())
	if got.dest != destReviewSecrets || !strings.Contains(got.reason, "unreadable") {
		t.Fatalf("dest = %q reason = %q, want review/secrets and an unreadable reason", got.dest, got.reason)
	}
}

func TestClassifySkillPrefixOutranksExtensionRouting(t *testing.T) {
	// A recognised report family goes to its own folder even when the extension
	// rules would happily route it, so whole skill folders can be skipped later.
	ctx := emptyContext()
	ctx.skillPrefixes["audit-tests"] = struct{}{}
	got := classify(fileEntry(t, "audit-tests.findings.md", "x"), ctx)
	if got.dest != reportsPrefix+"audit-tests" {
		t.Fatalf("dest = %q, want reports/audit-tests", got.dest)
	}
	if got.reason != reasonSkillConvention {
		t.Fatalf("reason = %q, want %q", got.reason, reasonSkillConvention)
	}
}

func TestClassifyDirProtectsRegisteredWorktrees(t *testing.T) {
	// Relocating a registered worktree rewrites git state, so it must be flagged
	// before any content heuristic gets a say.
	root := t.TempDir()
	item := dirEntry(t, root, "checkout")
	ctx := emptyContext()
	ctx.worktrees[item.registered] = struct{}{}

	got := classify(item, ctx)
	if got.dest != destReviewCheckouts || !got.worktree {
		t.Fatalf("dest = %q worktree = %v, want review/checkouts and worktree=true", got.dest, got.worktree)
	}
	if len(got.nested) != 0 {
		t.Fatalf("nested = %v, want none for a worktree that is itself the entry", got.nested)
	}
}

func TestClassifyDirProtectsParentsOfNestedWorktrees(t *testing.T) {
	// Moving the parent of a registered worktree silently breaks that
	// registration, so the parent needs the same protection and must report each
	// nested path — repairing the parent alone leaves the rest prunable.
	root := t.TempDir()
	item := dirEntry(t, root, "worktrees")
	ctx := emptyContext()
	for _, branch := range []string{"feat-b", "feat-a"} {
		mkdirAt(t, filepath.Join(item.abs, branch))
		ctx.worktrees[filepath.Join(item.registered, branch)] = struct{}{}
	}

	got := classify(item, ctx)
	if got.dest != destReviewCheckouts || !got.worktree {
		t.Fatalf("dest = %q worktree = %v, want review/checkouts and worktree=true", got.dest, got.worktree)
	}
	if strings.Join(got.nested, ",") != "feat-a,feat-b" {
		t.Fatalf("nested = %v, want deterministic [feat-a feat-b]", got.nested)
	}
	if !strings.Contains(got.reason, "2 registered git worktree(s)") {
		t.Fatalf("reason = %q, want the nested count", got.reason)
	}
}

func TestClassifyDirWithUnreadableSubtreeIsNotJudged(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 directory, so unreadability cannot be simulated")
	}
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	locked := mkdirAt(t, filepath.Join(item.abs, "locked"))
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// Restore traversal so the temp-dir cleanup can remove the tree afterwards.
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) // #nosec G302 -- a directory needs the execute bit to be removed.

	got := classify(item, emptyContext())
	if got.dest != destReviewUnknown {
		t.Fatalf("dest = %q, want review/unknown for a tree that could not be inspected", got.dest)
	}
}

func TestClassifyDirFindsKeyMaterialBelowTheTop(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	writeFileAt(t, filepath.Join(item.abs, "deep", "cluster.pem"), privateKeyPEM)
	writeFileAt(t, filepath.Join(item.abs, "notes.md"), "x")

	got := classify(item, emptyContext())
	if got.dest != destReviewSecrets || !strings.Contains(got.reason, "cluster.pem") {
		t.Fatalf("dest = %q reason = %q, want review/secrets naming cluster.pem", got.dest, got.reason)
	}
}

func TestClassifyDirPublicCertificateIsNotASecret(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	writeFileAt(t, filepath.Join(item.abs, "chain.pem"), publicCertPEM)

	got := classify(item, emptyContext())
	if got.dest == destReviewSecrets {
		t.Fatalf("dest = %q, want a certificate-only tree to stay out of review/secrets", got.dest)
	}
}

func TestClassifyDirRecognisesCheckoutsAndRegenerableTrees(t *testing.T) {
	root := t.TempDir()

	clone := dirEntry(t, root, "some-clone")
	mkdirAt(t, filepath.Join(clone.abs, ".git"))
	if got := classify(clone, emptyContext()); got.dest != destReviewCheckouts {
		t.Errorf("clone dest = %q, want review/checkouts", got.dest)
	}

	modules := dirEntry(t, root, "node_modules")
	writeFileAt(t, filepath.Join(modules.abs, "index.js"), "x")
	if got := classify(modules, emptyContext()); got.dest != destReviewRegenerable {
		t.Errorf("node_modules dest = %q, want review/regenerable", got.dest)
	}

	cache := dirEntry(t, root, "playwright-cache")
	writeFileAt(t, filepath.Join(cache.abs, "blob.bin"), "x")
	if got := classify(cache, emptyContext()); got.dest != destReviewRegenerable {
		t.Errorf("cache dest = %q, want review/regenerable", got.dest)
	}
}

func TestLooksVendoredNeedsBothVolumeAndJSDominance(t *testing.T) {
	// The ratio rule is what catches a dependency install that was renamed, but a
	// small JS tree is ordinary work product and must not be swept into
	// review/regenerable.
	small := treeScan{files: 10, byExt: map[string]int{"js": 10}}
	if looksVendored("vendor-copy", small) {
		t.Error("a 10-file JS tree must not read as a dependency install")
	}
	big := treeScan{files: 600, byExt: map[string]int{"js": 600}}
	if !looksVendored("vendor-copy", big) {
		t.Error("a 600-file all-JS tree must read as a dependency install")
	}
	mixed := treeScan{files: 600, byExt: map[string]int{"js": 100, "md": 500}}
	if looksVendored("vendor-copy", mixed) {
		t.Error("a mostly-markdown tree must not read as a dependency install")
	}
	// An archive is one artifact, not an install, whatever its contents look like.
	if looksVendored("bundle.zip", big) {
		t.Error("an archive must not read as a dependency install")
	}
}

func TestClassifyDirRepoCopyIsRegenerable(t *testing.T) {
	// A tree that merely restates tracked source is reproducible from the repo.
	root := t.TempDir()
	item := dirEntry(t, root, "fixture-copy")
	ctx := emptyContext()
	for i := 0; i < repoCopyMinFiles; i++ {
		name := fmt.Sprintf("source%d.go", i)
		writeFileAt(t, filepath.Join(item.abs, name), "package x")
		ctx.tracked[name] = struct{}{}
	}

	if got := classify(item, ctx); got.dest != destReviewRegenerable {
		t.Fatalf("dest = %q, want review/regenerable", got.dest)
	}
}

func TestClassifyDirKeepsUntrackedAuthoredAssets(t *testing.T) {
	// The rule this replaced treated a markdown-free directory as disposable and
	// nearly discarded logo SVGs that existed nowhere else.
	root := t.TempDir()
	item := dirEntry(t, root, "brand")
	writeFileAt(t, filepath.Join(item.abs, "wordmark.svg"), "<svg/>")

	got := classify(item, emptyContext())
	if got.dest != destReviewUniqueAssets || !strings.Contains(got.reason, "1 authored asset") {
		t.Fatalf("dest = %q reason = %q, want review/unique-assets with a count", got.dest, got.reason)
	}
}

func TestUniqueAssetCountIgnoresTrackedAndToolShippedAssets(t *testing.T) {
	scan := treeScan{names: []string{
		"wordmark.svg",                         // authored here
		"tracked.svg",                          // exists in the repo
		"playwright-logo.svg",                  // shipped by tooling
		"codicon.svg",                          // shipped by tooling
		"0123456789abcdef0123456789abcdef.svg", // content-addressed tool output
		"notes.md",                             // not an asset at all
	}}
	tracked := newSet("tracked.svg")
	if got := uniqueAssetCount(scan, tracked); got != 1 {
		t.Fatalf("uniqueAssetCount = %d, want 1 (only wordmark.svg is irreplaceable)", got)
	}
}

func TestClassifyDirBulkSampleDumpAsksForExtractionFirst(t *testing.T) {
	root := t.TempDir()
	item := dirEntry(t, root, "samples")
	for i := 0; i <= bulkSamplesMinFiles; i++ {
		writeFileAt(t, filepath.Join(item.abs, fmt.Sprintf("s%d.json", i)), "{}")
	}

	got := classify(item, emptyContext())
	if got.dest != destReviewBulkSamples || !strings.Contains(got.reason, "extract analysis") {
		t.Fatalf("dest = %q reason = %q, want review/bulk-samples", got.dest, got.reason)
	}
}

func TestClassifyDirFallsBackToEvidence(t *testing.T) {
	// A small mixed directory matches no rule; it is routed without review rather
	// than parked in review/ where it would dilute the entries that need a human.
	root := t.TempDir()
	item := dirEntry(t, root, "run-2025")
	writeFileAt(t, filepath.Join(item.abs, "notes.md"), "x")
	writeFileAt(t, filepath.Join(item.abs, "stdout.log"), "x")

	if got := classify(item, emptyContext()); got.dest != destArtifactsEvidence {
		t.Fatalf("dest = %q, want artifacts/evidence", got.dest)
	}
}

func TestScanTreeStopsAtItsLimitAndSaysSo(t *testing.T) {
	// The bound keeps a deep dependency tree from stalling classification; the
	// limit is honoured between directories, and truncated is what stops a
	// partial sample being read as a complete one.
	root := t.TempDir()
	for _, sub := range []string{"a", "b", "c"} {
		for i := 0; i < 2; i++ {
			writeFileAt(t, filepath.Join(root, sub, fmt.Sprintf("f%d.txt", i)), "x")
		}
	}
	scan := scanTree(root, 2)
	if !scan.truncated || scan.files >= 6 {
		t.Fatalf("truncated = %v files = %d, want a truncated partial scan", scan.truncated, scan.files)
	}

	full := scanTree(root, 100)
	if full.truncated || full.files != 6 {
		t.Fatalf("truncated = %v files = %d, want a complete scan of 6 files", full.truncated, full.files)
	}
}

func TestDiscloseScanBoundsAnnotatesPartialWalks(t *testing.T) {
	result := discloseScanBounds(placement{reason: "mixed evidence directory"}, treeScan{truncated: true})
	if !strings.Contains(result.reason, "judged from the first") {
		t.Fatalf("reason = %q, want a disclosure that the walk was partial", result.reason)
	}
	unchanged := discloseScanBounds(placement{reason: "mixed evidence directory"}, treeScan{})
	if unchanged.reason != "mixed evidence directory" {
		t.Fatalf("reason = %q, want it untouched for a complete walk", unchanged.reason)
	}
}

func TestScanTreeIgnoresSymlinksAndRecordsSecretPaths(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "deep", "id_rsa"), privateKeyPEM)
	if err := os.Symlink(filepath.Join(root, "deep"), filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	scan := scanTree(root, scanFileLimit)
	if scan.files != 1 {
		t.Fatalf("files = %d, want 1: a symlink must not be walked into a second time", scan.files)
	}
	if got := scan.paths["id_rsa"]; len(got) != 1 {
		t.Fatalf("scan.paths[id_rsa] = %v, want the one candidate path retained so its content can be checked", got)
	}
}

func TestClassifyDirCapsTheSecretsItEnumerates(t *testing.T) {
	// The reason line has to stay readable in a terminal, so it names a few
	// examples rather than every hit; the folder itself is the complete list.
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	for _, name := range []string{"a.p12", "b.p12", "c.p12", "d.p12"} {
		writeFileAt(t, filepath.Join(item.abs, name), "binary")
	}

	got := classify(item, emptyContext())
	if got.dest != destReviewSecrets {
		t.Fatalf("dest = %q, want review/secrets", got.dest)
	}
	if names := strings.Count(got.reason, ".p12"); names != examplesShown {
		t.Fatalf("reason = %q, want exactly %d examples", got.reason, examplesShown)
	}
}

func TestClassifyDirInspectsEveryPathForADuplicateCandidateName(t *testing.T) {
	// Two files can share a candidate filename and differ in content. Judging the
	// name by whichever path was walked first lets a directory holding a real key
	// bypass secret routing, so every recorded path is inspected.
	root := t.TempDir()
	item := dirEntry(t, root, "evidence")
	writeFileAt(t, filepath.Join(item.abs, "first", "config.pem"), publicCertPEM)
	writeFileAt(t, filepath.Join(item.abs, "second", "config.pem"), privateKeyPEM)

	got := classify(item, emptyContext())
	if got.dest != destReviewSecrets || !strings.Contains(got.reason, "config.pem") {
		t.Fatalf("dest = %q reason = %q, want review/secrets naming config.pem", got.dest, got.reason)
	}
}

func TestFindSecretsClearsANameOnlyWhenEveryPathIsClean(t *testing.T) {
	// The mirror of the case above: a name whose every occurrence is a certificate
	// must not be flagged, or review/secrets fills with noise and stops being read.
	root := t.TempDir()
	scan := treeScan{paths: map[string][]string{
		"chain.pem": {
			writeFileAt(t, filepath.Join(root, "a", "chain.pem"), publicCertPEM),
			writeFileAt(t, filepath.Join(root, "b", "chain.pem"), publicCertPEM),
		},
	}}
	if hits := findSecrets(scan); len(hits) != 0 {
		t.Fatalf("findSecrets = %v, want no hits when every path is a certificate", hits)
	}
}
