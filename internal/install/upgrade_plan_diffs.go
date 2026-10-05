package install

import (
	"errors"
	"fmt"
	"os"
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

	var origins map[string]string
	var ungatedTemplatePaths map[string]string
	ops := plannedOperationsFromReport(plan.MigrationReport)
	if hasRenameMigration(ops) {
		ungatedTemplatePaths, err = inst.templates().ungatedTemplatePathByRel()
		if err != nil {
			return nil, err
		}
		origins, err = inst.templateOriginsAfterMigrations(ops, ungatedTemplatePaths)
		if err != nil {
			return nil, err
		}
	}

	previews := make(map[string]DiffPreview)

	addPlanChanges := func(changes []UpgradeChange, mode planDiffMode) error {
		for _, change := range changes {
			var preview DiffPreview
			var previewErr error
			origin := origins[change.Path]
			if mode == planDiffModeUpdate && origin != "" && origin != filepath.Join(root, filepath.FromSlash(change.Path)) {
				if templatePathByRel[change.Path] == "" {
					templatePathByRel[change.Path] = ungatedTemplatePaths[change.Path]
				}
				localBytes, err := inst.sys.ReadFile(origin)
				if err != nil {
					return err
				}
				preview, previewErr = inst.buildSingleDiffPreviewFromBytes(change.Path, templatePathByRel, localBytes)
			} else {
				preview, previewErr = inst.buildPlanChangeDiffPreview(change, mode, templatePathByRel)
			}
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
	if err := addPlanChanges(plan.StatuslineSourceAdditions, planDiffModeAddition); err != nil {
		return nil, err
	}
	if err := addPlanChanges(plan.StatuslineSourceUpdates, planDiffModeUpdate); err != nil {
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
	switch mode {
	case planDiffModeUpdate:
		return inst.buildSingleDiffPreview(change.Path, templatePathByRel)
	case planDiffModeAddition:
		desiredBytes, label, err := inst.additionPreviewBytes(change.Path, templatePathByRel)
		if err != nil {
			return DiffPreview{}, err
		}
		return inst.renderDiffPreview(
			change.Path,
			change.Path+" (current)",
			change.Path+" ("+label+")",
			"",
			normalizeTemplateContent(string(desiredBytes)),
		), nil
	case planDiffModeRemoval:
		localPath := filepath.Join(inst.root, filepath.FromSlash(change.Path))
		// A directory, a symlink, or a path a migration has yet to create has
		// no file content to preview.
		info, err := inst.sys.Lstat(localPath)
		if errors.Is(err, os.ErrNotExist) {
			return DiffPreview{Path: change.Path}, nil
		}
		if err != nil {
			return DiffPreview{}, err
		}
		if !info.Mode().IsRegular() {
			return DiffPreview{Path: change.Path}, nil
		}
		localBytes, err := inst.sys.ReadFile(localPath)
		if err != nil {
			return DiffPreview{}, err
		}
		return inst.renderDiffPreview(
			change.Path,
			change.Path+" (current)",
			change.Path+" (template)",
			normalizeTemplateContent(string(localBytes)),
			"",
		), nil
	default:
		return DiffPreview{}, fmt.Errorf(messages.InstallUnknownPlanDiffModeFmt, mode)
	}
}

func (inst *installer) additionPreviewBytes(relPath string, templatePathByRel map[string]string) ([]byte, string, error) {
	if source, ok := statuslineSourceByRelPath(relPath); ok {
		data, label, err := inst.statuslineSourceSeedBytes(source)
		return data, label, err
	}
	templatePath := templatePathByRel[relPath]
	if strings.TrimSpace(templatePath) == "" {
		return nil, "", fmt.Errorf(messages.InstallMissingTemplatePathMappingFmt, relPath)
	}
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return nil, "", err
	}
	return templateBytes, statuslineSeedOriginTemplate, nil
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
	for _, source := range StatuslineSourceTemplates() {
		out[source.RelPath] = source.TemplatePath
	}
	return out, nil
}
