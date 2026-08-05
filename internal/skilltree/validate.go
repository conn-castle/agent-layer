package skilltree

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

// nonBlockingFindings are validator findings that describe a style
// recommendation rather than a projection or identity defect. Import
// acceptance treats every other finding as blocking so an unprojectable skill
// is never silently accepted.
var nonBlockingFindings = map[string]struct{}{
	skillvalidator.FindingCodeSizeRecommendation: {},
}

// SkillInfo is the identity a validated skill tree carries.
type SkillInfo struct {
	// Name is the frontmatter name, which must equal the selected directory
	// name and becomes the local imported directory name.
	Name string
	// Description is the required nonempty frontmatter description.
	Description string
}

// ValidateSkill enforces Agent Layer's strict skill rules on a tree that was
// selected at sourcePath.
//
// It requires a canonical SKILL.md at the tree root, valid required metadata, a
// safe skill name, a name matching the selected directory, and frontmatter
// Agent Layer can project faithfully. sourcePath is used for error context and
// to derive the expected name.
func ValidateSkill(tree Tree, sourcePath string) (SkillInfo, error) {
	manifest, ok := tree.File(SkillManifestName)
	if !ok {
		if alternate, found := findCaseVariantManifest(tree); found {
			return SkillInfo{}, fmt.Errorf("skill %s uses %s; imported skills require a canonical %s", sourcePath, alternate, SkillManifestName)
		}
		return SkillInfo{}, fmt.Errorf("skill %s has no %s", sourcePath, SkillManifestName)
	}
	return ValidateManifest(manifest.Data, sourcePath)
}

// ValidateManifest applies the same strict rules to one manifest's bytes.
//
// Callers that already hold the manifest — ordinary projection reads it from
// the editable source directory — use it so local content is held to the exact
// standard imports are, rather than to the tolerant rules generic
// configuration loading applies. sourcePath names the skill root.
func ValidateManifest(manifest []byte, sourcePath string) (SkillInfo, error) {
	expected := path.Base(sourcePath)
	parsed, err := skillvalidator.ParseSkillContent(path.Join(sourcePath, SkillManifestName), manifest)
	if err != nil {
		return SkillInfo{}, fmt.Errorf("invalid skill %s: %w", sourcePath, err)
	}

	var blocking []string
	for _, finding := range skillvalidator.ValidateParsedSkill(parsed) {
		if _, skip := nonBlockingFindings[finding.Code]; skip {
			continue
		}
		blocking = append(blocking, finding.Message)
	}
	if len(blocking) > 0 {
		sort.Strings(blocking)
		return SkillInfo{}, fmt.Errorf("invalid skill %s: %s", sourcePath, strings.Join(blocking, "; "))
	}

	name := normalizeName(*parsed.Name)
	if name != normalizeName(expected) {
		return SkillInfo{}, fmt.Errorf("skill %s declares name %q, expected %q to match its directory", sourcePath, name, expected)
	}
	return SkillInfo{Name: name, Description: strings.TrimSpace(*parsed.Description)}, nil
}

// findCaseVariantManifest reports a root-level manifest that differs from the
// canonical filename only by case, so the error can name the actual problem.
func findCaseVariantManifest(tree Tree) (string, bool) {
	for _, file := range tree.Files() {
		if strings.Contains(file.Path, "/") {
			continue
		}
		if strings.EqualFold(file.Path, SkillManifestName) {
			return file.Path, true
		}
	}
	return "", false
}

// HasManifest reports whether a tree carries a root-level canonical SKILL.md.
// Wildcard selectors use it to ignore ordinary directories.
func HasManifest(tree Tree) bool {
	_, ok := tree.File(SkillManifestName)
	return ok
}

// NormalizeName applies the same Unicode normalization used when comparing
// skill names across configuration, imports, and user-managed sources.
func NormalizeName(value string) string { return normalizeName(value) }

func normalizeName(value string) string {
	return strings.TrimSpace(norm.NFKC.String(value))
}
