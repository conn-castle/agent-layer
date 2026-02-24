package install

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	// DefaultDiffMaxLines is the default maximum number of diff lines shown per file.
	DefaultDiffMaxLines = 40
	// diffLineCapFlagName is the CLI flag name used to raise per-file diff line caps.
	diffLineCapFlagName = "--diff-lines"
)

// DiffPreview is a user-facing, per-file diff preview used by upgrade prompts and plans.
type DiffPreview struct {
	Path        string
	Ownership   OwnershipLabel
	UnifiedDiff string
	Truncated   bool
}

func normalizeDiffMaxLines(value int) int {
	if value <= 0 {
		return DefaultDiffMaxLines
	}
	return value
}

func (inst *installer) buildManagedDiffPreviews(entries []LabeledPath) ([]DiffPreview, map[string]DiffPreview, error) {
	templatePathByRel, err := inst.managedTemplatePathByRel()
	if err != nil {
		return nil, nil, err
	}
	previews, err := inst.buildDiffPreviews(entries, templatePathByRel)
	if err != nil {
		return nil, nil, err
	}
	return previews, indexDiffPreviews(previews), nil
}

func (inst *installer) buildMemoryDiffPreviews(entries []LabeledPath) ([]DiffPreview, map[string]DiffPreview, error) {
	templatePathByRel, err := inst.memoryTemplatePathByRel()
	if err != nil {
		return nil, nil, err
	}
	previews, err := inst.buildDiffPreviews(entries, templatePathByRel)
	if err != nil {
		return nil, nil, err
	}
	return previews, indexDiffPreviews(previews), nil
}

func indexDiffPreviews(previews []DiffPreview) map[string]DiffPreview {
	index := make(map[string]DiffPreview, len(previews))
	for _, preview := range previews {
		index[filepath.ToSlash(preview.Path)] = preview
	}
	return index
}

func (inst *installer) buildDiffPreviews(entries []LabeledPath, templatePathByRel map[string]string) ([]DiffPreview, error) {
	out := make([]DiffPreview, 0, len(entries))
	for _, entry := range entries {
		preview, err := inst.buildSingleDiffPreview(entry, templatePathByRel)
		if err != nil {
			return nil, err
		}
		out = append(out, preview)
	}
	return out, nil
}

func (inst *installer) buildSingleDiffPreview(entry LabeledPath, templatePathByRel map[string]string) (DiffPreview, error) {
	relPath := filepath.ToSlash(entry.Path)
	if relPath == "" {
		return DiffPreview{}, fmt.Errorf(messages.InstallDiffPreviewPathRequired)
	}
	if relPath == pinVersionRelPath {
		return inst.pinVersionDiffPreview(relPath, entry.Ownership)
	}

	templatePath := templatePathByRel[relPath]
	if strings.TrimSpace(templatePath) == "" {
		return DiffPreview{}, fmt.Errorf(messages.InstallMissingTemplatePathMappingFmt, relPath)
	}

	localPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	localBytes, err := inst.sys.ReadFile(localPath)
	if err != nil {
		return DiffPreview{}, err
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return DiffPreview{}, err
	}

	fromName := relPath
	toName := relPath
	fromContent := normalizeTemplateContent(string(localBytes))
	toContent := normalizeTemplateContent(string(templateBytes))

	if marker, ok := sectionAwareMarkerForPath(relPath); ok {
		localManaged, _, err := splitSectionAwareContent(relPath, marker, localBytes)
		if err != nil {
			return DiffPreview{}, err
		}
		templateManaged, _, err := splitSectionAwareContent(relPath, marker, templateBytes)
		if err != nil {
			return DiffPreview{}, err
		}
		fromName = relPath + " (current managed section)"
		toName = relPath + " (template managed section)"
		fromContent = normalizeTemplateContent(localManaged)
		toContent = normalizeTemplateContent(templateManaged)
	}

	rendered, truncated := renderTruncatedUnifiedDiff(fromName, toName, fromContent, toContent, inst.diffMaxLines)
	return DiffPreview{
		Path:        relPath,
		Ownership:   entry.Ownership,
		UnifiedDiff: rendered,
		Truncated:   truncated,
	}, nil
}

func (inst *installer) pinVersionDiffPreview(relPath string, ownership OwnershipLabel) (DiffPreview, error) {
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	current := ""
	if data, err := inst.sys.ReadFile(path); err == nil {
		current = strings.TrimSpace(string(data))
	}
	target := strings.TrimSpace(inst.pinVersion)

	from := ""
	if current != "" {
		from = current + "\n"
	}
	to := ""
	if target != "" {
		to = target + "\n"
	}

	rendered, truncated := renderTruncatedUnifiedDiff(
		pinVersionRelPath+" (current)",
		pinVersionRelPath+" (target)",
		from,
		to,
		inst.diffMaxLines,
	)
	return DiffPreview{
		Path:        relPath,
		Ownership:   ownership,
		UnifiedDiff: rendered,
		Truncated:   truncated,
	}, nil
}

func renderTruncatedUnifiedDiff(fromName string, toName string, fromContent string, toContent string, maxLines int) (string, bool) {
	limit := normalizeDiffMaxLines(maxLines)
	diff := normalizeUnifiedDiffPreview(udiff.Unified(fromName, toName, fromContent, toContent))
	lines := splitDiffLines(diff)
	if len(lines) <= limit {
		return ensureTrailingNewline(strings.Join(lines, "\n")), false
	}
	truncated := lines[:limit]
	truncated = append(
		truncated,
		fmt.Sprintf("... (truncated to %d lines; rerun with %s <n> to see more)", limit, diffLineCapFlagName),
	)
	return ensureTrailingNewline(strings.Join(truncated, "\n")), true
}

func splitDiffLines(content string) []string {
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return []string{}
	}
	return strings.Split(trimmed, "\n")
}

func ensureTrailingNewline(content string) string {
	if content == "" {
		return ""
	}
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

func normalizeUnifiedDiffPreview(diff string) string {
	lines := splitDiffLines(diff)
	if len(lines) == 0 {
		return ""
	}
	out := make([]string, 0, len(lines))
	for idx := 0; idx < len(lines); {
		line := lines[idx]
		if isUnifiedDiffChangeLine(line) {
			end := idx
			for end < len(lines) && isUnifiedDiffChangeLine(lines[end]) {
				end++
			}
			out = append(out, collapseEquivalentDiffRun(lines[idx:end])...)
			idx = end
			continue
		}
		out = append(out, line)
		idx++
	}
	return ensureTrailingNewline(strings.Join(out, "\n"))
}

func isUnifiedDiffChangeLine(line string) bool {
	if strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
		return false
	}
	return strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+")
}

func collapseEquivalentDiffRun(run []string) []string {
	if len(run) == 0 {
		return nil
	}
	removedByNormalized := make(map[string][]int)
	addedByNormalized := make(map[string][]int)
	for idx, line := range run {
		if strings.HasPrefix(line, "-") {
			key := normalizeComparableDiffPayload(line[1:])
			removedByNormalized[key] = append(removedByNormalized[key], idx)
		}
		if strings.HasPrefix(line, "+") {
			key := normalizeComparableDiffPayload(line[1:])
			addedByNormalized[key] = append(addedByNormalized[key], idx)
		}
	}

	skip := make([]bool, len(run))
	for key, removedIndexes := range removedByNormalized {
		addedIndexes := addedByNormalized[key]
		pairCount := len(removedIndexes)
		if len(addedIndexes) < pairCount {
			pairCount = len(addedIndexes)
		}
		for idx := 0; idx < pairCount; idx++ {
			skip[removedIndexes[idx]] = true
			skip[addedIndexes[idx]] = true
		}
	}

	out := make([]string, 0, len(run))
	for idx, line := range run {
		if skip[idx] {
			continue
		}
		out = append(out, line)
	}
	return out
}

func normalizeComparableDiffPayload(payload string) string {
	return strings.TrimRight(payload, " \t")
}

func sectionAwareMarkerForPath(relPath string) (string, bool) {
	switch ownershipPolicyForPath(relPath) {
	case ownershipPolicyMemoryEntries:
		return ownershipMarkerEntriesStart, true
	case ownershipPolicyMemoryRoadmap:
		return ownershipMarkerPhasesStart, true
	default:
		return "", false
	}
}

func splitSectionAwareContent(relPath string, marker string, content []byte) (managed string, user string, err error) {
	lines := strings.SplitAfter(string(content), "\n")
	index := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if trimmed != marker {
			continue
		}
		if index >= 0 {
			return "", "", fmt.Errorf(messages.InstallSectionAwareMarkerDuplicateFmt, marker, relPath)
		}
		index = i
	}
	if index < 0 {
		return "", "", fmt.Errorf(messages.InstallSectionAwareMarkerMissingFmt, marker, relPath)
	}
	managed = strings.Join(lines[:index+1], "")
	user = strings.Join(lines[index+1:], "")
	return managed, user, nil
}
