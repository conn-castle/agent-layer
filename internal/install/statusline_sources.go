package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// Repo-relative paths of the editable status line source files.
const (
	claudeStatuslineSourceRelPath = ".agent-layer/claude-statusline.sh"
	codexStatuslineSourceRelPath  = ".agent-layer/codex-statusline.toml"
)

type statuslineSourceTemplate struct {
	relPath       string
	templatePath  string
	legacyRelPath string
	perm          os.FileMode
}

func statuslineSourceTemplates() []statuslineSourceTemplate {
	return []statuslineSourceTemplate{
		{
			relPath:       claudeStatuslineSourceRelPath,
			templatePath:  "claude-statusline.sh",
			legacyRelPath: ".agent-layer/statusline.sh",
			perm:          0o755,
		},
		{
			relPath:      codexStatuslineSourceRelPath,
			templatePath: "codex-statusline.toml",
			perm:         0o644,
		},
	}
}

func (inst *installer) writeStatuslineSources() error {
	cfg, err := config.LoadConfigLenient(filepath.Join(inst.root, ".agent-layer", "config.toml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, source := range statuslineSourceTemplates() {
		if !statuslineSourceEnabled(cfg, source.relPath) {
			continue
		}
		if err := inst.writeStatuslineSource(source); err != nil {
			return err
		}
	}
	return nil
}

func statuslineSourceEnabled(cfg *config.Config, relPath string) bool {
	switch relPath {
	case claudeStatuslineSourceRelPath:
		return config.ClaudeStatuslineEnabled(cfg.Agents.Claude)
	case codexStatuslineSourceRelPath:
		return config.CodexStatuslineEnabled(cfg.Agents.Codex)
	default:
		return false
	}
}

func (inst *installer) writeStatuslineSource(source statuslineSourceTemplate) error {
	path := filepath.Join(inst.root, filepath.FromSlash(source.relPath))
	info, err := inst.sys.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		return inst.seedMissingStatuslineSource(source, path)
	}
	if info.IsDir() {
		return fmt.Errorf(messages.InstallFailedReadFmt, path, errors.New("is a directory"))
	}
	matches, err := inst.templates().matchTemplate(inst.sys, path, source.templatePath, info)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	overwrite := false
	if prompt, ok := inst.prompter.(statuslineSourcePrompter); ok {
		preview, previewErr := inst.buildStatuslineSourceDiffPreview(source)
		if previewErr != nil {
			return previewErr
		}
		overwrite, err = prompt.StatuslineSource(preview)
		if err != nil {
			return err
		}
	}
	if !overwrite {
		return nil
	}
	return inst.writeStatuslineSourceTemplate(source, path)
}

func (inst *installer) seedMissingStatuslineSource(source statuslineSourceTemplate, path string) error {
	if source.legacyRelPath != "" {
		legacyPath := filepath.Join(inst.root, filepath.FromSlash(source.legacyRelPath))
		data, err := inst.sys.ReadFile(legacyPath)
		if err == nil {
			return inst.writeStatuslineSourceBytes(path, data, source.perm)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallFailedReadFmt, legacyPath, err)
		}
	}
	return inst.writeStatuslineSourceTemplate(source, path)
}

func (inst *installer) writeStatuslineSourceTemplate(source statuslineSourceTemplate, path string) error {
	data, err := templates.Read(source.templatePath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, source.templatePath, err)
	}
	return inst.writeStatuslineSourceBytes(path, data, source.perm)
}

func (inst *installer) writeStatuslineSourceBytes(path string, data []byte, perm os.FileMode) error {
	if err := inst.sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	if err := inst.sys.WriteFileAtomic(path, data, perm); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func (inst *installer) buildStatuslineSourceDiffPreview(source statuslineSourceTemplate) (DiffPreview, error) {
	path := filepath.Join(inst.root, filepath.FromSlash(source.relPath))
	localBytes, err := inst.sys.ReadFile(path)
	if err != nil {
		return DiffPreview{}, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	templateBytes, err := templates.Read(source.templatePath)
	if err != nil {
		return DiffPreview{}, fmt.Errorf(messages.InstallFailedReadTemplateFmt, source.templatePath, err)
	}
	rendered, truncated, added, removed := renderTruncatedUnifiedDiff(
		source.relPath+" (current)",
		source.relPath+" (template)",
		normalizeTemplateContent(string(localBytes)),
		normalizeTemplateContent(string(templateBytes)),
		inst.diffMaxLines,
	)
	return DiffPreview{
		Path:         source.relPath,
		Ownership:    OwnershipLocalCustomization,
		UnifiedDiff:  rendered,
		Truncated:    truncated,
		LinesAdded:   added,
		LinesRemoved: removed,
	}, nil
}

func (inst *installer) writeStatuslineSourcesTargetPaths() []string {
	paths := make([]string, 0, len(statuslineSourceTemplates()))
	for _, source := range statuslineSourceTemplates() {
		paths = append(paths, filepath.Join(inst.root, filepath.FromSlash(source.relPath)))
	}
	return paths
}

func (inst *installer) planStatuslineSourceChanges(plan migrationPlan) ([]upgradeChangeWithTemplate, []upgradeChangeWithTemplate, error) {
	cfg, err := config.LoadConfigLenient(filepath.Join(inst.root, ".agent-layer", "config.toml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	additions := make([]upgradeChangeWithTemplate, 0)
	updates := make([]upgradeChangeWithTemplate, 0)
	for _, source := range statuslineSourceTemplates() {
		if !statuslineSourceEnabledAfterMigrations(cfg, source.relPath, plan) {
			continue
		}
		path := filepath.Join(inst.root, filepath.FromSlash(source.relPath))
		info, err := inst.sys.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				additions = append(additions, statuslineSourceUpgradeChange(source))
				continue
			}
			return nil, nil, fmt.Errorf(messages.InstallFailedStatFmt, path, err)
		}
		if info.IsDir() {
			continue
		}
		matches, err := inst.templates().matchTemplate(inst.sys, path, source.templatePath, info)
		if err != nil {
			return nil, nil, err
		}
		if !matches {
			updates = append(updates, statuslineSourceUpgradeChange(source))
		}
	}
	return additions, updates, nil
}

func statuslineSourceUpgradeChange(source statuslineSourceTemplate) upgradeChangeWithTemplate {
	return upgradeChangeWithTemplate{
		path:         source.relPath,
		templatePath: source.templatePath,
		ownership: ownershipClassification{
			Label: OwnershipLocalCustomization,
			State: OwnershipStateLocalCustomization,
		},
	}
}

func statuslineSourceEnabledAfterMigrations(cfg *config.Config, relPath string, plan migrationPlan) bool {
	var key string
	switch relPath {
	case claudeStatuslineSourceRelPath:
		key = "agents.claude.statusline"
		if cfg.Agents.Claude.Statusline != nil {
			return *cfg.Agents.Claude.Statusline
		}
	case codexStatuslineSourceRelPath:
		key = "agents.codex.statusline"
		if cfg.Agents.Codex.Statusline != nil {
			return *cfg.Agents.Codex.Statusline
		}
	default:
		return false
	}
	for _, migration := range plan.configMigrations {
		if migration.Key == key && migration.From == "(unset)" {
			return migration.To == "true"
		}
	}
	return false
}
