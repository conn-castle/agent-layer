package install

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// UpgradePlanDiffPreviewOptions controls diff preview generation for upgrade plan rendering.
type UpgradePlanDiffPreviewOptions struct {
	System       System
	MaxDiffLines int
}

type planDiffMode string

const (
	planDiffModeUpdate   planDiffMode = "update"
	planDiffModeAddition planDiffMode = "addition"
	planDiffModeRemoval  planDiffMode = "removal"
)

// BuildUpgradePlanDiffPreviews builds line-level diff previews for upgrade-plan text rendering.
func BuildUpgradePlanDiffPreviews(root string, plan UpgradePlan, opts UpgradePlanDiffPreviewOptions) (map[string]DiffPreview, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf(messages.InstallRootRequired)
	}
	if opts.System == nil {
		return nil, fmt.Errorf(messages.InstallSystemRequired)
	}
	inst := &installer{
		root:         root,
		sys:          opts.System,
		pinVersion:   plan.PinVersionChange.Target,
		diffMaxLines: normalizeDiffMaxLines(opts.MaxDiffLines),
	}

	templatePathByRel, err := inst.allTemplatePathByRel()
	if err != nil {
		return nil, err
	}

	previews := make(map[string]DiffPreview)

	addPlanChanges := func(changes []UpgradeChange, mode planDiffMode) error {
		for _, change := range changes {
			preview, previewErr := inst.buildPlanChangeDiffPreview(change, mode, templatePathByRel)
			if previewErr != nil {
				return previewErr
			}
			previews[change.Path] = preview
		}
		return nil
	}

	if err := addPlanChanges(plan.TemplateAdditions, planDiffModeAddition); err != nil {
		return nil, err
	}
	if err := addPlanChanges(plan.TemplateUpdates, planDiffModeUpdate); err != nil {
		return nil, err
	}
	if err := addPlanChanges(plan.SectionAwareUpdates, planDiffModeUpdate); err != nil {
		return nil, err
	}
	if err := addPlanChanges(plan.TemplateRemovalsOrOrphans, planDiffModeRemoval); err != nil {
		return nil, err
	}

	return previews, nil
}

func (inst *installer) buildPlanChangeDiffPreview(change UpgradeChange, mode planDiffMode, templatePathByRel map[string]string) (DiffPreview, error) {
	entry := LabeledPath{
		Path:      change.Path,
		Ownership: change.Ownership,
	}
	switch mode {
	case planDiffModeUpdate:
		return inst.buildSingleDiffPreview(entry, templatePathByRel)
	case planDiffModeAddition:
		templatePath := templatePathByRel[change.Path]
		if strings.TrimSpace(templatePath) == "" {
			return DiffPreview{}, fmt.Errorf(messages.InstallMissingTemplatePathMappingFmt, change.Path)
		}
		templateBytes, err := templates.Read(templatePath)
		if err != nil {
			return DiffPreview{}, err
		}
		rendered, truncated := renderTruncatedUnifiedDiff(
			change.Path+" (current)",
			change.Path+" (template)",
			"",
			normalizeTemplateContent(string(templateBytes)),
			inst.diffMaxLines,
		)
		return DiffPreview{
			Path:        change.Path,
			Ownership:   change.Ownership,
			UnifiedDiff: rendered,
			Truncated:   truncated,
		}, nil
	case planDiffModeRemoval:
		localPath := filepath.Join(inst.root, filepath.FromSlash(change.Path))
		localBytes, err := inst.sys.ReadFile(localPath)
		if err != nil {
			return DiffPreview{}, err
		}
		rendered, truncated := renderTruncatedUnifiedDiff(
			change.Path+" (current)",
			change.Path+" (template)",
			normalizeTemplateContent(string(localBytes)),
			"",
			inst.diffMaxLines,
		)
		return DiffPreview{
			Path:        change.Path,
			Ownership:   change.Ownership,
			UnifiedDiff: rendered,
			Truncated:   truncated,
		}, nil
	default:
		return DiffPreview{}, fmt.Errorf(messages.InstallUnknownPlanDiffModeFmt, mode)
	}
}

func (inst *installer) allTemplatePathByRel() (map[string]string, error) {
	managed, err := inst.templates().managedTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	memory, err := inst.templates().memoryTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(managed)+len(memory))
	for key, value := range managed {
		out[key] = value
	}
	for key, value := range memory {
		out[key] = value
	}
	return out, nil
}
