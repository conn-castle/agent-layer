// Command publish-site publishes the Agent Layer website into a local checkout of Repo B.
//
// This tool is run from Repo A (conn-castle/agent-layer) during the release
// workflow on tag pushes `v*` (starting at the first website-capable release).
//
// It copies content from Repo A `site/` into Repo B, snapshots the docs version
// via Docusaurus versioning, and normalizes `versions.json` ordering.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	tag := flag.String("tag", "", "Git tag to publish, e.g. v0.6.0 (required)")
	repoBDir := flag.String("repo-b-dir", "", "Path to local checkout of agent-layer-web (required)")
	docusaurusTimeout := flag.Duration("docusaurus-timeout", 5*time.Minute, "Timeout for docusaurus docs:version (e.g. 5m, 30s)")
	flag.Parse()

	if *tag == "" {
		return fmt.Errorf("--tag is required")
	}
	if *repoBDir == "" {
		return fmt.Errorf("--repo-b-dir is required")
	}
	if *docusaurusTimeout <= 0 {
		return fmt.Errorf("--docusaurus-timeout must be a positive duration")
	}

	if err := validateTagFormat(*tag); err != nil {
		return err
	}
	docsVersion := stripV(*tag)

	repoA, err := repoRoot()
	if err != nil {
		return fmt.Errorf("failed to find repo root: %w", err)
	}

	repoB, err := filepath.Abs(*repoBDir)
	if err != nil {
		return fmt.Errorf("failed to resolve repo-b-dir: %w", err)
	}

	if err := validateRepoBRoot(repoB); err != nil {
		return err
	}

	sitePages := filepath.Join(repoA, "site", "pages")
	siteDocs := filepath.Join(repoA, "site", "docs")

	if _, err := os.Stat(sitePages); os.IsNotExist(err) {
		return fmt.Errorf("missing Repo A site pages dir: %s", sitePages)
	}
	if _, err := os.Stat(siteDocs); os.IsNotExist(err) {
		return fmt.Errorf("missing Repo A site docs dir: %s", siteDocs)
	}
	changelogSrc := filepath.Join(repoA, "CHANGELOG.md")
	changelogInfo, err := os.Stat(changelogSrc)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("missing Repo A changelog: %s", changelogSrc)
		}
		return fmt.Errorf("failed to stat Repo A changelog: %w", err)
	}

	// Publish unversioned pages by replacing Repo B src/pages.
	dstPages := filepath.Join(repoB, "src", "pages")
	if err := os.MkdirAll(filepath.Join(repoB, "src"), 0755); err != nil {
		return fmt.Errorf("failed to create src dir: %w", err)
	}
	fmt.Printf("Copying %s -> %s\n", sitePages, dstPages)
	if err := copyTree(sitePages, dstPages); err != nil {
		return fmt.Errorf("failed to copy pages: %w", err)
	}

	// Copy docs staging.
	dstDocs := filepath.Join(repoB, "docs")
	fmt.Printf("Copying %s -> %s\n", siteDocs, dstDocs)
	if err := copyTree(siteDocs, dstDocs); err != nil {
		return fmt.Errorf("failed to copy docs: %w", err)
	}

	// Sync canonical changelog into Repo B root for website rendering.
	changelogData, err := os.ReadFile(changelogSrc)
	if err != nil {
		return fmt.Errorf("failed to read Repo A changelog: %w", err)
	}
	changelogDst := filepath.Join(repoB, "CHANGELOG.md")
	if err := os.WriteFile(changelogDst, changelogData, changelogInfo.Mode()); err != nil {
		return fmt.Errorf("failed to write Repo B changelog: %w", err)
	}

	// Ensure reruns are safe for the same version.
	fmt.Printf("Ensuring idempotent version %s...\n", docsVersion)
	if err := ensureIdempotentVersion(repoB, docsVersion); err != nil {
		return fmt.Errorf("failed to ensure idempotent version: %w", err)
	}

	// Snapshot docs version.
	fmt.Printf("Running docusaurus docs:version %s...\n", docsVersion)
	ctx, cancel := context.WithTimeout(context.Background(), *docusaurusTimeout)
	defer cancel()

	// #nosec G204 -- docsVersion is validated and only used to run a trusted local command.
	cmd := execCommandContext(ctx, "npx", "docusaurus", "docs:version", docsVersion)
	cmd.Dir = repoB
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("docusaurus docs:version exceeded timeout (%s): %w", docusaurusTimeout.String(), err)
		}
		return fmt.Errorf("docusaurus docs:version failed: %w", err)
	}

	// Normalize versions.json ordering.
	fmt.Println("Normalizing versions.json...")
	if err := normalizeVersionsJSON(repoB); err != nil {
		return fmt.Errorf("failed to normalize versions.json: %w", err)
	}

	fmt.Println("Done!")
	return nil
}

// repoRoot returns Repo A root by searching upwards for go.mod.
func repoRoot() (string, error) {
	// Try to find repo root by looking for go.mod
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find repo root (no go.mod found)")
}

var tagRegexp = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$`)

var execCommandContext = exec.CommandContext

func validateTagFormat(tag string) error {
	if !tagRegexp.MatchString(tag) {
		return fmt.Errorf("invalid tag format: %s (expected vX.Y.Z or vX.Y.Z-prerelease)", tag)
	}
	return nil
}

func stripV(tag string) string {
	return strings.TrimPrefix(tag, "v")
}

func validateRepoBRoot(repoB string) error {
	if _, err := os.Stat(repoB); os.IsNotExist(err) {
		return fmt.Errorf("--repo-b-dir does not exist: %s", repoB)
	}

	// Must be a git checkout.
	if _, err := os.Stat(filepath.Join(repoB, ".git")); os.IsNotExist(err) {
		return fmt.Errorf("--repo-b-dir is not a git checkout (missing .git): %s", repoB)
	}

	// Must look like a Docusaurus repo root.
	required := []string{"package.json", "docusaurus.config.js", "sidebars.js", "src"}
	for _, f := range required {
		if _, err := os.Stat(filepath.Join(repoB, f)); os.IsNotExist(err) {
			return fmt.Errorf("--repo-b-dir missing %s: %s", f, repoB)
		}
	}

	if _, err := os.Stat(filepath.Join(repoB, "src", "pages")); os.IsNotExist(err) {
		return fmt.Errorf("--repo-b-dir missing src/pages/: %s", repoB)
	}

	return nil
}

func copyTree(src, dst string) error {
	// Remove destination if exists.
	if err := os.RemoveAll(dst); err != nil {
		return err
	}

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func ensureIdempotentVersion(repoB, docsVersion string) error {
	// Remove existing versioned docs.
	versionedDocsDir := filepath.Join(repoB, "versioned_docs", fmt.Sprintf("version-%s", docsVersion))
	if err := os.RemoveAll(versionedDocsDir); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Remove existing versioned sidebar.
	sidebarPath := filepath.Join(repoB, "versioned_sidebars", fmt.Sprintf("version-%s-sidebars.json", docsVersion))
	if err := os.Remove(sidebarPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Remove version from versions.json if present.
	versionsPath := filepath.Join(repoB, "versions.json")
	if _, err := os.Stat(versionsPath); err == nil {
		data, err := os.ReadFile(versionsPath)
		if err != nil {
			return err
		}

		var versions []string
		if err := json.Unmarshal(data, &versions); err != nil {
			return err
		}

		var filtered []string
		for _, v := range versions {
			if v != docsVersion {
				filtered = append(filtered, v)
			}
		}

		newData, err := json.MarshalIndent(filtered, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(versionsPath, append(newData, '\n'), 0644); err != nil {
			return err
		}
	}

	return nil
}

// version represents a semver-like version for sorting.
type version struct {
	major      int
	minor      int
	patch      int
	prerelease string
	original   string
}

func parseVersion(s string) (version, error) {
	v := version{original: s}

	parts := strings.SplitN(s, "-", 2)
	core := parts[0]
	if len(parts) > 1 {
		v.prerelease = parts[1]
	}

	coreParts := strings.Split(core, ".")
	if len(coreParts) != 3 {
		return v, fmt.Errorf("expected X.Y.Z, got: %s", s)
	}

	var err error
	v.major, err = strconv.Atoi(coreParts[0])
	if err != nil {
		return v, fmt.Errorf("invalid major version %q: %w", coreParts[0], err)
	}
	v.minor, err = strconv.Atoi(coreParts[1])
	if err != nil {
		return v, fmt.Errorf("invalid minor version %q: %w", coreParts[1], err)
	}
	v.patch, err = strconv.Atoi(coreParts[2])
	if err != nil {
		return v, fmt.Errorf("invalid patch version %q: %w", coreParts[2], err)
	}

	return v, nil
}

// comparePrerelease compares two prerelease strings according to SemVer precedence rules.
// It assumes a and b are non-empty strings (stable releases are handled separately).
// It returns -1 if a < b, 0 if a == b, and 1 if a > b.
func comparePrerelease(a string, b string) int {
	aIDs := strings.Split(a, ".")
	bIDs := strings.Split(b, ".")

	for i := 0; i < len(aIDs) && i < len(bIDs); i++ {
		aRaw := aIDs[i]
		bRaw := bIDs[i]

		aNum, aIsNum := parseNumericIdentifier(aRaw)
		bNum, bIsNum := parseNumericIdentifier(bRaw)

		switch {
		case aIsNum && bIsNum:
			if aNum < bNum {
				return -1
			}
			if aNum > bNum {
				return 1
			}
		case aIsNum && !bIsNum:
			// Numeric identifiers have lower precedence than non-numeric.
			return -1
		case !aIsNum && bIsNum:
			return 1
		default:
			if aRaw < bRaw {
				return -1
			}
			if aRaw > bRaw {
				return 1
			}
		}
	}

	// If all compared identifiers are equal, the longer list has higher precedence.
	if len(aIDs) < len(bIDs) {
		return -1
	}
	if len(aIDs) > len(bIDs) {
		return 1
	}
	return 0
}

// parseNumericIdentifier reports whether s is a valid SemVer numeric identifier:
// it must be all digits with no leading zeros (except the string "0").
func parseNumericIdentifier(s string) (int, bool) {
	if s == "0" {
		return 0, true
	}
	if s == "" || s[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func normalizeVersionsJSON(repoB string) error {
	versionsPath := filepath.Join(repoB, "versions.json")
	if _, err := os.Stat(versionsPath); os.IsNotExist(err) {
		return fmt.Errorf("versions.json not found after docs:version")
	}

	data, err := os.ReadFile(versionsPath)
	if err != nil {
		return err
	}

	var versions []string
	if err := json.Unmarshal(data, &versions); err != nil {
		return err
	}

	// Deduplicate.
	seen := make(map[string]bool)
	var unique []string
	for _, v := range versions {
		if !seen[v] {
			seen[v] = true
			unique = append(unique, v)
		}
	}

	// Sort newest-first.
	sort.Slice(unique, func(i, j int) bool {
		vi, errI := parseVersion(unique[i])
		vj, errJ := parseVersion(unique[j])

		// If parsing fails, fall back to string comparison.
		if errI != nil || errJ != nil {
			return unique[i] > unique[j]
		}

		// Compare major, minor, patch.
		if vi.major != vj.major {
			return vi.major > vj.major
		}
		if vi.minor != vj.minor {
			return vi.minor > vj.minor
		}
		if vi.patch != vj.patch {
			return vi.patch > vj.patch
		}

		// Stable releases (no prerelease) come before prereleases.
		if vi.prerelease == "" && vj.prerelease != "" {
			return true
		}
		if vi.prerelease != "" && vj.prerelease == "" {
			return false
		}

		// Both have prereleases, compare by SemVer precedence.
		return comparePrerelease(vi.prerelease, vj.prerelease) > 0
	})

	newData, err := json.MarshalIndent(unique, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(versionsPath, append(newData, '\n'), 0644)
}
