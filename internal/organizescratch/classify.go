// Package organizescratch sorts a scratch directory into a reviewable
// structure. It only moves top-level entries and never deletes, overwrites, or
// merges them. Deletion remains a separate human decision.
package organizescratch

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	destReviewSecrets      = "review/secrets"
	destReviewCheckouts    = "review/checkouts"
	destReviewRegenerable  = "review/regenerable"
	destReviewUniqueAssets = "review/unique-assets"
	destReviewBulkSamples  = "review/bulk-samples"
	destReviewOversized    = "review/oversized"
	destReviewSymlinks     = "review/symlinks"
	destReviewUnknown      = "review/unknown"

	destArtifactsLogs        = "artifacts/logs"
	destArtifactsScreenshots = "artifacts/screenshots"
	destArtifactsDiffs       = "artifacts/diffs"
	destArtifactsScripts     = "artifacts/scripts"
	destArtifactsData        = "artifacts/data"
	destArtifactsEvidence    = "artifacts/evidence"

	reviewPrefix       = "review/"
	reportsPrefix      = "reports/"
	reportsAdhocPrefix = "reports/adhoc/"

	adhocFolderPR         = "pr"
	adhocFolderIncidents  = "incidents"
	adhocFolderReviews    = "reviews"
	adhocFolderPlansSpecs = "plans-specs"
	adhocFallbackFolder   = "misc"

	reasonSkillConvention = "skill naming convention"
)

const (
	// Samples are used only by statistical heuristics. Safety facts are always
	// collected by a complete metadata walk.
	scanNameSample = 400
	repoCopySample = 300

	privateKeyProbeBytes = 1 << 20
	vendoredMinFiles     = 500
	vendoredJSRatio      = 0.5
	repoCopyMinFiles     = 50
	repoCopyRatio        = 0.6
	bulkSamplesMinFiles  = 300
	bulkSamplesMaxTypes  = 6
	examplesShown        = 3

	maxUnreviewedFiles   = 100
	maxUnreviewedBytes   = int64(250 * 1024 * 1024)
	unreadableLinkTarget = "<unreadable>"
)

var (
	logExt   = newSet("log", "out", "err", "stdout", "stderr")
	imageExt = newSet("png", "jpg", "jpeg", "gif", "webp", "bmp", "tiff")
	diffExt  = newSet("diff", "patch")
	codeExt  = newSet("mjs", "cjs", "js", "ts", "tsx", "sh", "py", "go", "rb", "sql")
	assetExt = newSet("svg", "psd", "ai", "sketch", "fig", "xcf", "eps")
)

var (
	extSuffix   = regexp.MustCompile(`(?i)\.[a-z0-9]+$`)
	noiseSuffix = regexp.MustCompile(`(?i)\.(prompt|report|plan|task|context|findings|output|evidence|summary|bak|\d{8}[-\dT]*[a-z0-9-]*)$`)

	toolAsset = regexp.MustCompile(`(?i)(^(playwright-logo|codicon|favicon)|^[0-9a-f]{32,}\.)`)
	// Candidate matching intentionally favors review. Content confirmation keeps
	// broad names such as session.json from becoming automatic secret findings.
	secretPath       = regexp.MustCompile(`(?i)(^|[/_.-])(secret|keys?|tokens?|credentials?|jwt|password|private|auth|session|cookies?|storage[-_.]?state)([/_.-]|$)|(^|/)\.env(?:$|[/_.-])`)
	secretName       = regexp.MustCompile(`(?i)((^|[._-])(id_rsa|id_ed25519|id_ecdsa)$|\.(pem|key|p12|pfx|keystore)$)`)
	secretNameStrong = regexp.MustCompile(`(?i)((^|[._-])(id_rsa|id_ed25519|id_ecdsa)$|\.(p12|pfx|keystore)$)`)
	privateKeyBlock  = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)
	base64Run        = regexp.MustCompile(`[A-Za-z0-9+/_-]{16,}={0,2}`)
	envAssignment    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\s*=\s*(.*)$`)

	nodeModulesDir = regexp.MustCompile(`(?i)^node_modules$`)
	cacheDir       = regexp.MustCompile(`(?i)(^|[-_.])(cache|caches)$`)
	archiveName    = regexp.MustCompile(`(?i)\.(tgz|tar\.gz|zip)$`)
)

var adhocRules = []struct {
	folder  string
	pattern *regexp.Regexp
}{
	{adhocFolderPR, regexp.MustCompile(`(?i)((^|[-_])pr[-_]?\d|pr-body$|^pr-|^reply|[-_]reply[-_]|^ship-pr|^codex-reply|-pr$|^merge-pr|^reconcile-pr)`)},
	{adhocFolderIncidents, regexp.MustCompile(`(?i)(postmortem|incident|^sentry-|root-cause|error-report|failure-evidence|triage)`)},
	{adhocFolderReviews, regexp.MustCompile(`(?i)(review|audit|verif|findings|retrospective|second-opinion|certif|readiness)`)},
	{adhocFolderPlansSpecs, regexp.MustCompile(`(?i)(plan|spec|roadmap|arch|design|runbook|handoff|context|proposal|brief|checklist|migration)`)},
}

type entry struct {
	name       string
	isDir      bool
	isSymlink  bool
	abs        string
	registered string
}

func newEntry(root, canonicalRoot, name string, isDir, isSymlink bool) entry {
	return entry{
		name:       name,
		isDir:      isDir,
		isSymlink:  isSymlink,
		abs:        filepath.Join(root, name),
		registered: filepath.Join(canonicalRoot, name),
	}
}

type scannedLink struct {
	rel    string
	target string
}

type placement struct {
	entry
	dest   string
	reason string
	// stationary entries are caller-kept or stable control paths inspected only
	// as symlink owners; they are never classified or moved.
	stationary bool

	worktree        bool
	worktreeTargets []string
	worktreeRepairs []worktreeRepair
	gitDirTargets   []string
	gitFileTargets  []string
	links           []scannedLink
	sampled         bool
}

type classifyContext struct {
	skillPrefixes map[string]struct{}
	worktrees     map[string]struct{}
	tracked       map[string]struct{}
}

type treeMeasure struct {
	files         int
	apparentBytes int64
}

type treeScan struct {
	files         int
	apparentBytes int64
	sampleFiles   int
	byExt         map[string]int
	names         []string

	secretCandidates map[string]string
	assets           []string
	gitMarkers       []string
	gitDirTargets    []string
	gitFileTargets   []string
	unreadable       []string
	symlinks         []scannedLink
	immediate        map[string]treeMeasure
	browserProfiles  []string
}

type secretFinding struct {
	path   string
	reason string
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

func extOf(name string) string {
	dot := strings.LastIndex(name, ".")
	if dot < 0 {
		return ""
	}
	return strings.ToLower(name[dot+1:])
}

func prefixOf(name string) string {
	if dot := strings.Index(name, "."); dot >= 0 {
		return name[:dot]
	}
	return name
}

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

func isExplicitDependencyOrCache(name string) bool {
	return nodeModulesDir.MatchString(name) || cacheDir.MatchString(name)
}

func pathUnderDependencyOrCache(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if isExplicitDependencyOrCache(part) {
			return true
		}
	}
	return false
}

func firstComponent(rel string) string {
	if first, _, ok := strings.Cut(filepath.ToSlash(rel), "/"); ok {
		return first
	}
	return rel
}

// scanTree walks every metadata entry without following symlinks. .git markers
// are recorded, but .git directories are not descended into and therefore do
// not inflate counts or samples with repository metadata.
func scanTree(dir string) treeScan {
	scan := treeScan{
		byExt:            map[string]int{},
		secretCandidates: map[string]string{},
		immediate:        map[string]treeMeasure{},
	}
	childrenByDir := map[string]map[string]struct{}{}

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: %v", path, relErr))
			return nil
		}
		if walkErr != nil {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: %v", rel, walkErr))
			return nil
		}
		if rel == "." {
			return nil
		}

		parent := filepath.Dir(rel)
		if childrenByDir[parent] == nil {
			childrenByDir[parent] = map[string]struct{}{}
		}
		childrenByDir[parent][d.Name()] = struct{}{}

		if d.Name() == ".git" {
			scan.gitMarkers = append(scan.gitMarkers, filepath.ToSlash(rel))
			if d.IsDir() {
				target := filepath.Dir(rel)
				if target == "." {
					target = ""
				}
				scan.gitDirTargets = append(scan.gitDirTargets, target)
				return filepath.SkipDir
			}
			if d.Type().IsRegular() {
				target := filepath.Dir(rel)
				if target == "." {
					target = ""
				}
				scan.gitFileTargets = append(scan.gitFileTargets, target)
			}
		}
		if d.IsDir() {
			return nil
		}

		scan.files++
		component := firstComponent(rel)
		measure := scan.immediate[component]
		measure.files++

		info, infoErr := d.Info()
		if infoErr != nil {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: %v", rel, infoErr))
			scan.immediate[component] = measure
			return nil
		}
		if info.Size() > 0 {
			scan.apparentBytes += info.Size()
			measure.apparentBytes += info.Size()
		}
		scan.immediate[component] = measure

		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: read link: %v", rel, readErr))
				target = unreadableLinkTarget
			}
			scan.symlinks = append(scan.symlinks, scannedLink{rel: rel, target: target})
			return nil
		}
		if !info.Mode().IsRegular() {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: unsupported filesystem node %s", rel, info.Mode().Type()))
			return nil
		}
		if info.Mode().Perm()&0o444 == 0 {
			scan.unreadable = append(scan.unreadable, fmt.Sprintf("%s: no read permission bits", rel))
		}

		if scan.sampleFiles < scanNameSample {
			scan.sampleFiles++
			scan.names = append(scan.names, d.Name())
			scan.byExt[extOf(d.Name())]++
		}
		if inSet(assetExt, extOf(d.Name())) && !pathUnderDependencyOrCache(rel) {
			scan.assets = append(scan.assets, filepath.ToSlash(rel))
		}
		if secretCandidatePath(rel) {
			scan.secretCandidates[filepath.ToSlash(rel)] = path
		}
		return nil
	})
	if err != nil {
		scan.unreadable = append(scan.unreadable, err.Error())
	}

	for rel, names := range childrenByDir {
		_, cookies := names["Cookies"]
		_, loginData := names["Login Data"]
		_, localState := names["Local State"]
		if cookies && (loginData || localState) {
			if rel == "." {
				rel = "<entry root>"
			}
			scan.browserProfiles = append(scan.browserProfiles, filepath.ToSlash(rel))
		}
	}
	sort.Strings(scan.assets)
	sort.Strings(scan.gitMarkers)
	sort.Strings(scan.gitFileTargets)
	sort.Strings(scan.gitDirTargets)
	sort.Strings(scan.browserProfiles)
	return scan
}

func secretCandidatePath(rel string) bool {
	base := filepath.Base(rel)
	if isEnvFile(base) || secretName.MatchString(base) || secretPath.MatchString(filepath.ToSlash(rel)) {
		return true
	}
	// Playwright's schema is content-identifiable. Probe every JSON file within
	// the fixed content bound so an equivalent state file is not missed merely
	// because it has an unfamiliar name.
	return extOf(base) == "json"
}

func isEnvFile(base string) bool {
	lower := strings.ToLower(base)
	return lower == ".env" || strings.HasPrefix(lower, ".env.") || strings.HasSuffix(lower, ".env")
}

func readProbe(path string) ([]byte, bool, error) {
	handle, err := os.Open(path) // #nosec G304 -- caller-selected scratch content.
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = handle.Close() }()
	data, err := io.ReadAll(io.LimitReader(handle, privateKeyProbeBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > privateKeyProbeBytes {
		return data[:privateKeyProbeBytes], true, nil
	}
	return data, false, nil
}

func envHasNonEmptyAssignment(data []byte) bool {
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		match := envAssignment.FindStringSubmatch(line)
		if len(match) != 2 {
			continue
		}
		value := strings.TrimSpace(match[1])
		if value != "" && value != `""` && value != "''" {
			return true
		}
	}
	return false
}

func jsonHasTopLevelCookies(data []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return false
	}
	cookies, ok := object["cookies"]
	trimmed := bytes.TrimSpace(cookies)
	return ok && len(trimmed) > 0 && trimmed[0] == '['
}

func containsBase64PrivateKey(data []byte) bool {
	compact := make([]byte, 0, len(data))
	for _, char := range data {
		switch char {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			compact = append(compact, char)
		}
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, candidate := range base64Run.FindAll(compact, -1) {
		// Starting at each possible quartet alignment also handles bounded
		// candidate text with a short base64-looking prefix.
		for offset := 0; offset < 4 && offset < len(candidate); offset++ {
			for _, encoding := range encodings {
				decoded, err := encoding.DecodeString(string(candidate[offset:]))
				if err == nil && privateKeyBlock.Match(decoded) {
					return true
				}
			}
		}
	}
	return false
}

func probeSecret(rel, path string) (finding *secretFinding, unreadable, disclosure string) {
	base := pathpkg.Base(rel)
	if secretNameStrong.MatchString(base) {
		return &secretFinding{path: filepath.ToSlash(rel), reason: "filename identifies private-key or credential material"}, "", ""
	}
	data, truncated, err := readProbe(path)
	if err != nil {
		return nil, fmt.Sprintf("%s: credential probe failed: %v", filepath.ToSlash(rel), err), ""
	}
	if truncated {
		disclosure = fmt.Sprintf("%s: credential probe successfully inspected the first %s; remaining content was not scanned", filepath.ToSlash(rel), formatBytes(privateKeyProbeBytes))
	}
	if privateKeyBlock.Match(data) {
		return &secretFinding{path: filepath.ToSlash(rel), reason: "contains a PEM PRIVATE KEY block"}, "", disclosure
	}
	if containsBase64PrivateKey(data) {
		return &secretFinding{path: filepath.ToSlash(rel), reason: "decodes to a PEM PRIVATE KEY block"}, "", disclosure
	}
	if isEnvFile(base) && envHasNonEmptyAssignment(data) {
		return &secretFinding{path: filepath.ToSlash(rel), reason: "contains non-empty environment assignments that may be credentials or placeholders"}, "", disclosure
	}
	if extOf(base) == "json" && jsonHasTopLevelCookies(data) {
		return &secretFinding{path: filepath.ToSlash(rel), reason: "contains a top-level browser cookies array"}, "", disclosure
	}
	return nil, "", disclosure
}

func findSecrets(scan treeScan) (findings []secretFinding, unreadable, disclosures []string) {
	for _, profile := range scan.browserProfiles {
		findings = append(findings, secretFinding{path: profile, reason: "browser profile contains Cookies with Login Data and/or Local State"})
	}
	paths := make([]string, 0, len(scan.secretCandidates))
	for rel := range scan.secretCandidates {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		finding, problem, disclosure := probeSecret(rel, scan.secretCandidates[rel])
		if finding != nil {
			findings = append(findings, *finding)
		}
		if problem != "" {
			unreadable = append(unreadable, problem)
		}
		if disclosure != "" {
			disclosures = append(disclosures, disclosure)
		}
	}
	return findings, unreadable, disclosures
}

func uniqueAssetNames(scan treeScan, tracked map[string]struct{}) []string {
	assets := make([]string, 0, len(scan.assets))
	for _, rel := range scan.assets {
		name := pathpkg.Base(rel)
		if inSet(tracked, name) || toolAsset.MatchString(name) {
			continue
		}
		assets = append(assets, rel)
	}
	return assets
}

func looksVendored(name string, scan treeScan) (bool, bool) {
	if isExplicitDependencyOrCache(name) {
		return true, false
	}
	if archiveName.MatchString(name) || scan.files <= vendoredMinFiles || scan.sampleFiles == 0 {
		return false, false
	}
	js := scan.byExt["js"] + scan.byExt["mjs"] + scan.byExt["ts"] + scan.byExt["tsx"]
	return float64(js)/float64(scan.sampleFiles) > vendoredJSRatio, true
}

func looksLikeRepoCopy(scan treeScan, tracked map[string]struct{}) (bool, bool) {
	if scan.files < repoCopyMinFiles || len(tracked) == 0 || len(scan.names) == 0 {
		return false, false
	}
	sample := scan.names
	if len(sample) > repoCopySample {
		sample = sample[:repoCopySample]
	}
	hits := 0
	for _, name := range sample {
		if inSet(tracked, name) {
			hits++
		}
	}
	return float64(hits)/float64(len(sample)) > repoCopyRatio, true
}

func classify(item entry, ctx classifyContext) placement {
	if item.isSymlink {
		target, err := os.Readlink(item.abs)
		reason := "top-level symlink — inspect target and move effects by hand"
		if err != nil {
			target = "<unreadable>"
			reason += fmt.Sprintf("; target unreadable: %v", err)
		}
		return placement{entry: item, dest: destReviewSymlinks, reason: reason, links: []scannedLink{{target: target}}}
	}
	if item.isDir {
		return classifyDir(item, ctx)
	}
	return classifyFile(item, ctx)
}

func classifyFile(item entry, ctx classifyContext) placement {
	info, err := os.Lstat(item.abs)
	if err != nil {
		return place(item, destReviewUnknown, fmt.Sprintf("could not inspect size/type: %v", err))
	}
	if !info.Mode().IsRegular() {
		return place(item, destReviewUnknown, fmt.Sprintf("unsupported top-level filesystem node: %s", info.Mode().Type()))
	}

	scan := treeScan{secretCandidates: map[string]string{}}
	if secretCandidatePath(item.name) {
		scan.secretCandidates[item.name] = item.abs
	}
	findings, unreadable, disclosures := findSecrets(scan)
	var result placement
	switch {
	case len(findings) > 0:
		result = place(item, destReviewSecrets, secretReason(findings))
	case len(unreadable) > 0:
		result = place(item, destReviewSecrets, "possible credential material could not be fully inspected: "+strings.Join(firstN(unreadable, examplesShown), "; "))
	case info.Mode().Perm()&0o444 == 0:
		result = place(item, destReviewUnknown, "top-level file has no read permission bits — inspect by hand")
	case inSet(ctx.skillPrefixes, prefixOf(item.name)):
		result = place(item, reportsPrefix+prefixOf(item.name), reasonSkillConvention)
	default:
		ext := extOf(item.name)
		switch {
		case ext == "md":
			result = place(item, reportsAdhocPrefix+adhocFolder(stemOf(item.name)), "ad-hoc markdown")
		case inSet(logExt, ext):
			result = place(item, destArtifactsLogs, "log extension")
		case inSet(imageExt, ext):
			result = place(item, destArtifactsScreenshots, "image extension")
		case inSet(diffExt, ext):
			result = place(item, destArtifactsDiffs, "diff extension")
		case inSet(assetExt, ext):
			result = place(item, destReviewUniqueAssets, "authored asset, may be irreplaceable")
		case inSet(codeExt, ext):
			result = place(item, destArtifactsScripts, "script extension")
		default:
			result = place(item, destArtifactsData, "data file")
		}
	}
	if info.Size() > maxUnreviewedBytes {
		result = applySizeOverride(result, fmt.Sprintf("file apparent size %s exceeds %s", formatBytes(info.Size()), formatBytes(maxUnreviewedBytes)))
	}
	if len(disclosures) > 0 {
		result.reason += "; bounded credential inspection: " + strings.Join(firstN(disclosures, examplesShown), "; ")
	}
	return result
}

func adhocFolder(stem string) string {
	for _, rule := range adhocRules {
		if rule.pattern.MatchString(stem) {
			return rule.folder
		}
	}
	return adhocFallbackFolder
}

func classifyDir(item entry, ctx classifyContext) placement {
	scan := scanTree(item.abs)
	targets := registeredTargets(item.registered, ctx.worktrees)
	findings, probeUnreadable, disclosures := findSecrets(scan)
	unreadable := append(append([]string{}, scan.unreadable...), probeUnreadable...)
	assets := uniqueAssetNames(scan, ctx.tracked)

	var result placement
	switch {
	case len(findings) > 0:
		result = place(item, destReviewSecrets, secretReason(findings))
	case len(targets) > 0 || len(scan.gitMarkers) > 0:
		parts := make([]string, 0, 2)
		if len(targets) > 0 {
			parts = append(parts, fmt.Sprintf("contains %d registered git worktree(s): %s", len(targets), strings.Join(firstN(targets, examplesShown), ", ")))
		}
		if len(scan.gitMarkers) > 0 {
			parts = append(parts, fmt.Sprintf("contains %d git checkout marker(s): %s", len(scan.gitMarkers), strings.Join(firstN(scan.gitMarkers, examplesShown), ", ")))
		}
		result = place(item, destReviewCheckouts, strings.Join(parts, "; "))
	case isExplicitDependencyOrCache(item.name):
		result = place(item, destReviewRegenerable, "explicit dependency install or build cache — reinstallable")
	case len(assets) > 0:
		result = place(item, destReviewUniqueAssets, fmt.Sprintf("%d authored asset(s) not tracked in the repo: %s", len(assets), strings.Join(firstN(assets, examplesShown), ", ")))
	case len(unreadable) > 0:
		result = place(item, destReviewUnknown, fmt.Sprintf("inspection incomplete at %d path(s): %s", len(unreadable), strings.Join(firstN(unreadable, examplesShown), "; ")))
	default:
		vendored, vendoredSampled := looksVendored(item.name, scan)
		repoCopy, repoCopySampled := looksLikeRepoCopy(scan, ctx.tracked)
		switch {
		case vendored:
			result = place(item, destReviewRegenerable, "dependency install or build cache — reinstallable")
			result.sampled = vendoredSampled
		case repoCopy:
			result = place(item, destReviewRegenerable, "copy of tracked repo files — reproducible")
			result.sampled = repoCopySampled
		case inSet(ctx.skillPrefixes, prefixOf(item.name)):
			result = place(item, reportsPrefix+prefixOf(item.name), reasonSkillConvention)
		case scan.files > bulkSamplesMinFiles && len(scan.byExt) <= bulkSamplesMaxTypes:
			result = place(item, destReviewBulkSamples, fmt.Sprintf("%d files across %d sampled types — extract analysis before removing", scan.files, len(scan.byExt)))
			result.sampled = scan.files > scan.sampleFiles
		default:
			result = place(item, destArtifactsEvidence, "mixed evidence directory")
		}
	}

	result.worktreeTargets = targets
	result.worktree = len(targets) > 0
	result.gitDirTargets = scan.gitDirTargets
	result.gitFileTargets = scan.gitFileTargets
	result.links = scan.symlinks
	if len(findings) > 0 && (len(targets) > 0 || len(scan.gitMarkers) > 0) {
		result.reason += fmt.Sprintf("; checkout evidence also requires review (%d registered worktree target(s), %d .git marker(s))", len(targets), len(scan.gitMarkers))
	}
	if result.sampled && scan.files > scan.sampleFiles {
		result.reason += fmt.Sprintf(" (statistical verdict used a bounded sample of %d of %d files)", scan.sampleFiles, scan.files)
	}

	var sizeEvidence []string
	if scan.files > maxUnreviewedFiles {
		sizeEvidence = append(sizeEvidence, fmt.Sprintf("directory has %d files (limit %d)", scan.files, maxUnreviewedFiles))
	}
	if scan.apparentBytes > maxUnreviewedBytes {
		sizeEvidence = append(sizeEvidence, fmt.Sprintf("directory apparent size is %s (limit %s)", formatBytes(scan.apparentBytes), formatBytes(maxUnreviewedBytes)))
	}
	var childEvidence []string
	children := make([]string, 0, len(scan.immediate))
	for name := range scan.immediate {
		children = append(children, name)
	}
	sort.Strings(children)
	for _, name := range children {
		measure := scan.immediate[name]
		var facts []string
		if measure.files > maxUnreviewedFiles {
			facts = append(facts, fmt.Sprintf("%d files", measure.files))
		}
		if measure.apparentBytes > maxUnreviewedBytes {
			facts = append(facts, formatBytes(measure.apparentBytes))
		}
		if len(facts) > 0 {
			childEvidence = append(childEvidence, fmt.Sprintf("%s (%s)", name, strings.Join(facts, ", ")))
		}
	}
	if len(childEvidence) > 0 {
		sizeEvidence = append(sizeEvidence, "oversized immediate child/children: "+strings.Join(childEvidence, ", "))
	}
	if len(sizeEvidence) > 0 {
		result = applySizeOverride(result, strings.Join(sizeEvidence, "; "))
	}
	if len(unreadable) > 0 && result.dest != destReviewSecrets && result.dest != destReviewUnknown {
		result.reason += fmt.Sprintf("; inspection incomplete at %d path(s): %s", len(unreadable), strings.Join(firstN(unreadable, examplesShown), "; "))
	}
	if len(disclosures) > 0 {
		result.reason += "; bounded credential inspection: " + strings.Join(firstN(disclosures, examplesShown), "; ")
	}
	return result
}

func secretReason(findings []secretFinding) string {
	parts := make([]string, 0, len(findings))
	for _, finding := range firstN(findings, examplesShown) {
		parts = append(parts, fmt.Sprintf("%s (%s)", finding.path, finding.reason))
	}
	return fmt.Sprintf("contains %d credential candidate finding(s): %s; inspect values and revoke/rotate any live credentials before removal", len(findings), strings.Join(parts, "; "))
}

func applySizeOverride(result placement, evidence string) placement {
	if !strings.HasPrefix(result.dest, reviewPrefix) {
		result.dest = destReviewOversized
	}
	result.reason += "; size review required: " + evidence
	return result
}

func place(item entry, dest, reason string) placement {
	return placement{entry: item, dest: dest, reason: reason}
}

func registeredTargets(dir string, worktrees map[string]struct{}) []string {
	prefix := dir + string(os.PathSeparator)
	var targets []string
	if inSet(worktrees, dir) {
		targets = append(targets, ".")
	}
	for worktree := range worktrees {
		if strings.HasPrefix(worktree, prefix) {
			rel := strings.TrimPrefix(worktree, prefix)
			if rel != "" {
				targets = append(targets, rel)
			}
		}
	}
	sort.Strings(targets)
	return targets
}

func firstN[T any](values []T, limit int) []T {
	if len(values) > limit {
		return values[:limit]
	}
	return values
}

func formatBytes(value int64) string {
	const mebibyte = int64(1024 * 1024)
	if value%mebibyte == 0 {
		return fmt.Sprintf("%d MiB", value/mebibyte)
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/float64(mebibyte))
}
