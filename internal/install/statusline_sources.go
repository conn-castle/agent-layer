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

// statuslineSeedOriginTemplate labels seed bytes that came from the embedded
// template (as opposed to a migrated legacy source file).
const statuslineSeedOriginTemplate = "template"

// StatuslineSourceTemplate describes an editable provider status line source.
type StatuslineSourceTemplate struct {
	RelPath       string
	TemplatePath  string
	LegacyRelPath string
	Perm          os.FileMode
}

// StatuslineSourceTemplates returns the canonical editable status line sources.
func StatuslineSourceTemplates() []StatuslineSourceTemplate {
	return []StatuslineSourceTemplate{
		{
			RelPath:       claudeStatuslineSourceRelPath,
			TemplatePath:  "claude-statusline.sh",
			LegacyRelPath: ".agent-layer/statusline.sh",
			Perm:          0o755,
		},
		{
			RelPath:      codexStatuslineSourceRelPath,
			TemplatePath: "codex-statusline.toml",
			Perm:         0o644,
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
	for _, source := range StatuslineSourceTemplates() {
		if !statuslineSourceEnabled(cfg, source.RelPath) {
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

func (inst *installer) writeStatuslineSource(source StatuslineSourceTemplate) error {
	path := filepath.Join(inst.root, filepath.FromSlash(source.RelPath))
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
	matches, err := inst.templates().matchTemplate(inst.sys, path, source.TemplatePath, info)
	if err != nil {
		return err
	}
	if matches {
		return nil
	}
	overwrite := false
	router := inst.promptRouter()
	if router.hasStatuslineSource() {
		preview, previewErr := inst.buildStatuslineSourceDiffPreview(source)
		if previewErr != nil {
			return previewErr
		}
		resp, routeErr := router.route(promptRequest{kind: promptKindStatuslineSource, preview: preview})
		if routeErr != nil {
			return routeErr
		}
		overwrite = resp.approved
	}
	if !overwrite {
		return nil
	}
	return inst.writeStatuslineSourceTemplate(source, path)
}

func (inst *installer) seedMissingStatuslineSource(source StatuslineSourceTemplate, path string) error {
	data, _, err := inst.statuslineSourceSeedBytes(source)
	if err != nil {
		return err
	}
	return inst.writeStatuslineSourceBytes(path, data, source.Perm)
}

func (inst *installer) writeStatuslineSourceTemplate(source StatuslineSourceTemplate, path string) error {
	data, err := templates.Read(source.TemplatePath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, source.TemplatePath, err)
	}
	return inst.writeStatuslineSourceBytes(path, data, source.Perm)
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

func (inst *installer) buildStatuslineSourceDiffPreview(source StatuslineSourceTemplate) (DiffPreview, error) {
	path := filepath.Join(inst.root, filepath.FromSlash(source.RelPath))
	localBytes, err := inst.sys.ReadFile(path)
	if err != nil {
		return DiffPreview{}, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	templateBytes, err := templates.Read(source.TemplatePath)
	if err != nil {
		return DiffPreview{}, fmt.Errorf(messages.InstallFailedReadTemplateFmt, source.TemplatePath, err)
	}
	rendered, truncated, added, removed := renderTruncatedUnifiedDiff(
		source.RelPath+" (current)",
		source.RelPath+" (template)",
		normalizeTemplateContent(string(localBytes)),
		normalizeTemplateContent(string(templateBytes)),
		inst.diffMaxLines,
	)
	return DiffPreview{
		Path:         source.RelPath,
		Ownership:    OwnershipLocalCustomization,
		UnifiedDiff:  rendered,
		Truncated:    truncated,
		LinesAdded:   added,
		LinesRemoved: removed,
	}, nil
}

func (inst *installer) writeStatuslineSourcesTargetPaths() []string {
	sources := StatuslineSourceTemplates()
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		paths = append(paths, filepath.Join(inst.root, filepath.FromSlash(source.RelPath)))
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
	for _, source := range StatuslineSourceTemplates() {
		if !statuslineSourceEnabledAfterMigrations(cfg, source.RelPath, plan) {
			continue
		}
		path := filepath.Join(inst.root, filepath.FromSlash(source.RelPath))
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
		matches, err := inst.templates().matchTemplate(inst.sys, path, source.TemplatePath, info)
		if err != nil {
			return nil, nil, err
		}
		if !matches {
			updates = append(updates, statuslineSourceUpgradeChange(source))
		}
	}
	return additions, updates, nil
}

func statuslineSourceUpgradeChange(source StatuslineSourceTemplate) upgradeChangeWithTemplate {
	return upgradeChangeWithTemplate{
		path:         source.RelPath,
		templatePath: source.TemplatePath,
		ownership: ownershipClassification{
			Label: OwnershipLocalCustomization,
			State: OwnershipStateLocalCustomization,
		},
	}
}

func (inst *installer) statuslineSourceSeedBytes(source StatuslineSourceTemplate) ([]byte, string, error) {
	return statuslineSeedBytes(inst.root, inst.sys.ReadFile, source)
}

// StatuslineSourceSeedBytes returns the bytes to seed a missing status line
// source under root, preferring a present legacy source file over the embedded
// template so a reseed never clobbers the user's migrated legacy customizations.
// Callers outside the install package (e.g. the wizard) use this to share the
// canonical seed logic rather than reading TemplatePath directly.
func StatuslineSourceSeedBytes(root string, source StatuslineSourceTemplate) ([]byte, error) {
	data, _, err := statuslineSeedBytes(root, os.ReadFile, source)
	return data, err
}

// statuslineSeedBytes resolves seed bytes for a status line source, preferring
// the legacy source file (read via readFile) over the embedded template. It
// returns the bytes and a short origin label (legacy rel-path or "template").
func statuslineSeedBytes(root string, readFile func(string) ([]byte, error), source StatuslineSourceTemplate) ([]byte, string, error) {
	if source.LegacyRelPath != "" {
		legacyPath := filepath.Join(root, filepath.FromSlash(source.LegacyRelPath))
		data, err := readFile(legacyPath)
		if err == nil {
			return data, source.LegacyRelPath, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf(messages.InstallFailedReadFmt, legacyPath, err)
		}
	}
	data, err := templates.Read(source.TemplatePath)
	if err != nil {
		return nil, "", fmt.Errorf(messages.InstallFailedReadTemplateFmt, source.TemplatePath, err)
	}
	return data, statuslineSeedOriginTemplate, nil
}

func statuslineSourceByRelPath(relPath string) (StatuslineSourceTemplate, bool) {
	for _, source := range StatuslineSourceTemplates() {
		if source.RelPath == relPath {
			return source, true
		}
	}
	return StatuslineSourceTemplate{}, false
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
