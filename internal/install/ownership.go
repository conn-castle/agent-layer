package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	// OwnershipUpstreamTemplateDelta indicates a file differs because upstream template content changed.
	OwnershipUpstreamTemplateDelta OwnershipLabel = "upstream template delta"
	// OwnershipLocalCustomization indicates a file differs because local edits/customization are present.
	OwnershipLocalCustomization OwnershipLabel = "local customization"
)

// OwnershipLabel classifies why a managed file differs from the embedded template.
type OwnershipLabel string

// LabeledPath is a path paired with an ownership label.
type LabeledPath struct {
	Path      string
	Ownership OwnershipLabel
}

// Display returns a stable user-facing ownership label string.
func (o OwnershipLabel) Display() string {
	trimmed := strings.TrimSpace(string(o))
	if trimmed == "" {
		return string(OwnershipLocalCustomization)
	}
	return trimmed
}

// classifyOwnership classifies a template diff as upstream delta or local customization.
// This is best-effort: when classification is ambiguous, it returns local customization.
func (inst *installer) classifyOwnership(relPath string, templatePath string) (OwnershipLabel, error) {
	if !strings.HasPrefix(templatePath, "docs/agent-layer/") {
		return OwnershipLocalCustomization, nil
	}

	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	currentTemplateBytes, err := templates.Read(templatePath)
	if err != nil {
		return "", err
	}
	if normalizeTemplateContent(string(localBytes)) == normalizeTemplateContent(string(currentTemplateBytes)) {
		return OwnershipUpstreamTemplateDelta, nil
	}

	suffix := strings.TrimPrefix(templatePath, "docs/agent-layer/")
	baselinePath := filepath.Join(inst.root, ".agent-layer", "templates", "docs", filepath.FromSlash(suffix))
	baselineBytes, err := inst.sys.ReadFile(baselinePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return OwnershipLocalCustomization, nil
		}
		return "", err
	}

	localNormalized := normalizeTemplateContent(string(localBytes))
	baselineNormalized := normalizeTemplateContent(string(baselineBytes))
	templateNormalized := normalizeTemplateContent(string(currentTemplateBytes))
	if localNormalized == baselineNormalized && baselineNormalized != templateNormalized {
		return OwnershipUpstreamTemplateDelta, nil
	}
	return OwnershipLocalCustomization, nil
}

// classifyOrphanOwnership classifies template orphans; ambiguous cases are local customization.
func (inst *installer) classifyOrphanOwnership(relPath string) (OwnershipLabel, error) {
	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	localNormalized := normalizeTemplateContent(string(localBytes))

	switch {
	case strings.HasPrefix(relPath, "docs/agent-layer/"):
		suffix := strings.TrimPrefix(relPath, "docs/agent-layer/")
		baselinePath := filepath.Join(inst.root, ".agent-layer", "templates", "docs", filepath.FromSlash(suffix))
		baselineBytes, err := inst.sys.ReadFile(baselinePath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return OwnershipLocalCustomization, nil
			}
			return "", err
		}
		if localNormalized == normalizeTemplateContent(string(baselineBytes)) {
			return OwnershipUpstreamTemplateDelta, nil
		}
	case strings.HasPrefix(relPath, ".agent-layer/templates/docs/"):
		return OwnershipUpstreamTemplateDelta, nil
	}

	return OwnershipLocalCustomization, nil
}

func formatLabeledPaths(entries []LabeledPath) []string {
	if len(entries) == 0 {
		return nil
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, entry.Path+" ["+entry.Ownership.Display()+"]")
	}
	return lines
}
