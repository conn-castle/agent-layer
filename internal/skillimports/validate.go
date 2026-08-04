package skillimports

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillfrontmatter"
	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

const (
	manifestScannerInitialBufferSize = 64 * 1024
	manifestScannerMaxTokenSize      = 8 * 1024 * 1024
)

// SkillIdentity is the validated identity of an imported skill.
type SkillIdentity struct {
	// Name is the frontmatter name, which is also the local directory name.
	Name string
	// Description is the frontmatter description.
	Description string
}

// ValidateImportedTree applies Agent Layer's strict import rules to a
// materialized skill tree. sourcePath is the selected repository-relative path,
// whose final segment the skill name must match.
//
// Unlike the advisory source validator, every rule here is fatal: an import that
// cannot be projected faithfully is refused rather than accepted with fields
// silently dropped.
func ValidateImportedTree(tree *Tree, sourcePath string) (SkillIdentity, error) {
	manifest, ok := tree.Lookup(SkillManifestName)
	if !ok {
		return SkillIdentity{}, fmt.Errorf("%s has no %s", sourcePath, SkillManifestName)
	}

	doc, err := parseManifestFrontMatter(manifest.Data)
	if err != nil {
		return SkillIdentity{}, fmt.Errorf("%s/%s: %w", sourcePath, SkillManifestName, err)
	}

	var unknown []string
	for _, key := range doc.Keys {
		if !skillvalidator.IsAllowedFrontMatterField(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return SkillIdentity{}, fmt.Errorf(
			"%s/%s has frontmatter field(s) %s that Agent Layer cannot project; supported fields are %s",
			sourcePath, SkillManifestName,
			strings.Join(quoteAll(unknown), ", "),
			strings.Join(quoteAll(skillvalidator.AllowedFrontMatterFields()), ", "),
		)
	}

	name, err := requiredFrontMatterValue(doc.Name, "name")
	if err != nil {
		return SkillIdentity{}, fmt.Errorf("%s/%s: %w", sourcePath, SkillManifestName, err)
	}
	description, err := requiredFrontMatterValue(doc.Description, "description")
	if err != nil {
		return SkillIdentity{}, fmt.Errorf("%s/%s: %w", sourcePath, SkillManifestName, err)
	}

	normalizedName := skillvalidator.NormalizeName(name)
	if !skillvalidator.IsValidSkillName(normalizedName) {
		return SkillIdentity{}, fmt.Errorf(
			"%s/%s: name %q must contain only lowercase letters, digits, and hyphens, and must not start or end with a hyphen",
			sourcePath, SkillManifestName, name,
		)
	}
	if strings.Contains(normalizedName, "--") {
		return SkillIdentity{}, fmt.Errorf(
			"%s/%s: name %q must not contain consecutive hyphens", sourcePath, SkillManifestName, name,
		)
	}
	if count := utf8.RuneCountInString(normalizedName); count > skillvalidator.MaxSkillNameLength {
		return SkillIdentity{}, fmt.Errorf(
			"%s/%s: name %q is %d characters; the maximum is %d",
			sourcePath, SkillManifestName, name, count, skillvalidator.MaxSkillNameLength,
		)
	}

	directory := path.Base(sourcePath)
	if normalizedName != skillvalidator.NormalizeName(directory) {
		return SkillIdentity{}, fmt.Errorf(
			"%s/%s: name %q must match the selected directory %q",
			sourcePath, SkillManifestName, name, directory,
		)
	}

	for _, file := range tree.Files() {
		if err := rejectUnsafeMemberPath(file.Path); err != nil {
			return SkillIdentity{}, fmt.Errorf("%s: %w", sourcePath, err)
		}
	}

	return SkillIdentity{Name: normalizedName, Description: description}, nil
}

// rejectUnsafeMemberPath refuses a tree member whose name would be unusable or
// dangerous once materialized under the managed skill root.
func rejectUnsafeMemberPath(p string) error {
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("path %q is not a usable file path", p)
		}
	}
	if strings.ContainsRune(p, 0) {
		return fmt.Errorf("path %q contains a NUL byte", p)
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("path %q is not valid UTF-8", p)
	}
	return nil
}

// parseManifestFrontMatter splits a SKILL.md into its YAML frontmatter and
// parses it with the shared structural parser.
func parseManifestFrontMatter(data []byte) (skillfrontmatter.Document, error) {
	content := string(bytes.TrimPrefix(data, utf8BOM))
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, manifestScannerInitialBufferSize), manifestScannerMaxTokenSize)
	if !scanner.Scan() {
		return skillfrontmatter.Document{}, fmt.Errorf("file is empty")
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return skillfrontmatter.Document{}, fmt.Errorf("missing YAML frontmatter")
	}
	var lines []string
	terminated := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			terminated = true
			break
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return skillfrontmatter.Document{}, fmt.Errorf("read content: %w", err)
	}
	if !terminated {
		return skillfrontmatter.Document{}, fmt.Errorf("unterminated YAML frontmatter")
	}
	doc, err := skillfrontmatter.Parse(strings.Join(lines, "\n"))
	if err != nil {
		return skillfrontmatter.Document{}, fmt.Errorf("invalid frontmatter: %w", err)
	}
	return doc, nil
}

// requiredFrontMatterValue extracts a required nonempty scalar. A present-null
// field is treated the same as an empty one: both fail.
func requiredFrontMatterValue(field skillfrontmatter.Field, name string) (string, error) {
	if field.State != skillfrontmatter.FieldValue {
		return "", fmt.Errorf("missing required frontmatter field %q", name)
	}
	value := strings.TrimSpace(field.Value)
	if value == "" {
		return "", fmt.Errorf("frontmatter field %q must be non-empty", name)
	}
	if name == "name" && (field.Multiline || strings.Contains(value, "\n")) {
		return "", fmt.Errorf("frontmatter field %q must be a single line", name)
	}
	return value, nil
}

// rejectDuplicateSkillNames fails when two resolved imports would own the same
// local directory. Distinct source paths that normalize to one skill name are a
// deterministic configuration error, independent of sync history and independent
// of which block or repository each came from.
func rejectDuplicateSkillNames(entries []config.SkillImportLockEntry) error {
	type owner struct {
		repository string
		sourcePath string
		name       string
	}
	seen := make(map[string]owner, len(entries))
	sorted := append([]config.SkillImportLockEntry(nil), entries...)
	config.SortSkillImportLockEntries(sorted)
	for _, entry := range sorted {
		normalized := config.NormalizeSkillImportName(entry.SkillName)
		current := owner{repository: entry.Repository, sourcePath: entry.SourcePath, name: entry.SkillName}
		if previous, ok := seen[normalized]; ok {
			return fmt.Errorf(
				"skill name %q from %s:%s collides with %q from %s:%s; one local directory cannot have two owners",
				current.name, RedactSecrets(current.repository), current.sourcePath,
				previous.name, RedactSecrets(previous.repository), previous.sourcePath,
			)
		}
		seen[normalized] = current
	}
	return nil
}

// rejectOverlappingSourcePaths fails when two resolved imports from the exact
// same source repository select an ancestor and a descendant path.
func rejectOverlappingSourcePaths(entries []config.SkillImportLockEntry) error {
	byRepository := map[string][]string{}
	for _, entry := range entries {
		byRepository[entry.Repository] = append(byRepository[entry.Repository], entry.SourcePath)
	}
	repositories := make([]string, 0, len(byRepository))
	for repository := range byRepository {
		repositories = append(repositories, repository)
	}
	sort.Strings(repositories)
	for _, repository := range repositories {
		paths := byRepository[repository]
		sort.Strings(paths)
		if err := rejectOverlappingPaths(paths); err != nil {
			return fmt.Errorf("%s: %w", RedactSecrets(repository), err)
		}
	}
	return nil
}

func quoteAll(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprintf("%q", value))
	}
	return out
}
