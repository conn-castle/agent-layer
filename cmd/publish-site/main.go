// Command publish-site publishes the Agent Layer website into a local checkout of Repo B.
//
// This tool is run from Repo A (conn-castle/agent-layer) during the release
// workflow on tag pushes `v*` (starting at the first website-capable release).
//
// It copies content from Repo A `site/` into Repo B, generates a CLI reference
// page from the source, snapshots the docs version via Docusaurus versioning,
// and normalizes `versions.json` ordering.
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
	"strings"
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
	flag.Parse()

	if *tag == "" {
		return fmt.Errorf("--tag is required")
	}
	if *repoBDir == "" {
		return fmt.Errorf("--repo-b-dir is required")
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

	// Generate CLI reference into docs staging.
	fmt.Println("Generating CLI reference...")
	cliBody, err := generateCLIReferenceMDX(repoA, *tag)
	if err != nil {
		return fmt.Errorf("failed to generate CLI reference: %w", err)
	}

	cliDocPath := filepath.Join(dstDocs, "reference", "cli.mdx")
	if err := os.MkdirAll(filepath.Dir(cliDocPath), 0755); err != nil {
		return fmt.Errorf("failed to create reference dir: %w", err)
	}
	cliContent := "---\ntitle: CLI reference\n---\n\n" + cliBody + "\n"
	if err := os.WriteFile(cliDocPath, []byte(cliContent), 0644); err != nil {
		return fmt.Errorf("failed to write CLI reference: %w", err)
	}

	// Ensure reruns are safe for the same version.
	fmt.Printf("Ensuring idempotent version %s...\n", docsVersion)
	if err := ensureIdempotentVersion(repoB, docsVersion); err != nil {
		return fmt.Errorf("failed to ensure idempotent version: %w", err)
	}

	// Snapshot docs version.
	fmt.Printf("Running docusaurus docs:version %s...\n", docsVersion)
	// #nosec G204 -- docsVersion is validated and only used to run a trusted local command.
	cmd := execCommandContext(context.Background(), "npx", "docusaurus", "docs:version", docsVersion)
	cmd.Dir = repoB
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
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

// repoRoot returns Repo A root assuming binary/source is at cmd/publish-site/.
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
var runGoCmdFunc = runGoCmd

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

func generateCLIReferenceMDX(repoA, tag string) (string, error) {
	version := tag
	ldflags := fmt.Sprintf("-X main.Version=%s", version)

	// Get top-level help.
	topHelp, err := runGoCmdFunc(repoA, ldflags, "--help")
	if err != nil {
		return "", fmt.Errorf("failed to get top-level help: %w", err)
	}

	// Parse available commands from help output.
	cmds := parseAvailableCommands(topHelp)

	var parts []string
	parts = append(parts, fmt.Sprintf("## `al --help`\n\n```text\n%s\n```\n", topHelp))

	for _, cmd := range cmds {
		cmdHelp, err := runGoCmdFunc(repoA, ldflags, cmd, "--help")
		if err != nil {
			// Some commands might not have --help, skip them.
			continue
		}
		parts = append(parts, fmt.Sprintf("## `al %s --help`\n\n```text\n%s\n```\n", cmd, cmdHelp))
	}

	return strings.Join(parts, "\n"), nil
}

func runGoCmd(repoA, ldflags string, args ...string) (string, error) {
	cmdArgs := make([]string, 0, 4+len(args))
	cmdArgs = append(cmdArgs, "run", "-ldflags", ldflags, "./cmd/al")
	cmdArgs = append(cmdArgs, args...)

	// #nosec G204 -- cmdArgs is constructed from trusted inputs and runs the local Go toolchain.
	cmd := execCommandContext(context.Background(), "go", cmdArgs...)
	cmd.Dir = repoA
	if len(cmd.Env) == 0 {
		cmd.Env = append(os.Environ(), "AL_NO_NETWORK=1")
	} else {
		cmd.Env = append(cmd.Env, "AL_NO_NETWORK=1")
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w\noutput: %s", err, string(output))
	}
	return strings.TrimSpace(string(output)), nil
}

var cmdRegexp = regexp.MustCompile(`^\s{2,}([a-z0-9-]+)\s+.*$`)

func parseAvailableCommands(helpOutput string) []string {
	var cmds []string
	inCmds := false

	for _, line := range strings.Split(helpOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Available Commands:" {
			inCmds = true
			continue
		}
		if inCmds {
			if trimmed == "" || strings.HasPrefix(trimmed, "Flags:") {
				break
			}
			if matches := cmdRegexp.FindStringSubmatch(line); matches != nil {
				cmds = append(cmds, matches[1])
			}
		}
	}
	return cmds
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

	if _, err := fmt.Sscanf(coreParts[0], "%d", &v.major); err != nil {
		return v, err
	}
	if _, err := fmt.Sscanf(coreParts[1], "%d", &v.minor); err != nil {
		return v, err
	}
	if _, err := fmt.Sscanf(coreParts[2], "%d", &v.patch); err != nil {
		return v, err
	}

	return v, nil
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

		// Both have prereleases, compare lexicographically.
		return vi.prerelease > vj.prerelease
	})

	newData, err := json.MarshalIndent(unique, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(versionsPath, append(newData, '\n'), 0644)
}
