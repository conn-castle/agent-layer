package install

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// PromptOverwriteAllUnifiedPreviewFunc asks whether to apply managed and memory updates in one pass.
type PromptOverwriteAllUnifiedPreviewFunc func(managed []DiffPreview, memory []DiffPreview) (bool, bool, error)

// PromptOverwritePreviewFunc asks whether to overwrite a single diff preview path.
type PromptOverwritePreviewFunc func(preview DiffPreview) (bool, error)

// PromptStatuslineSourcePreviewFunc asks whether to replace a user-owned
// statusline source with the embedded template version.
type PromptStatuslineSourcePreviewFunc func(preview DiffPreview) (bool, error)

// PromptConfigSetDefaultFunc asks the user to confirm or customize a value for a
// missing required config key. It receives the key path, a default value from the
// migration manifest, a rationale string, and an optional field definition from the
// config catalog (nil when the key is not in the catalog). It returns the value to
// set (which may differ from the manifest value). When nil, the manifest value is
// used without prompting.
type PromptConfigSetDefaultFunc func(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error)

// SkillsMigrationConflict describes a flat-format skill that conflicts with an
// existing directory-format skill (different content at both locations).
type SkillsMigrationConflict struct {
	SkillName string
	FlatPath  string
	DirPath   string
	Reason    string
}

// PromptConfirmSkillsMigrationFunc asks the user to confirm the skills-format
// migration. It receives the list of flat skills to migrate and any detected
// conflicts. Returns true to proceed, false to abort.
type PromptConfirmSkillsMigrationFunc func(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error)

// PromptFuncs holds install prompt callbacks. A nil *PromptFuncs means headless.
// Overwrite mode requires unified and per-file overwrite callbacks and both
// delete-unknown callbacks. Optional nil callbacks keep statusline files, leave
// tmp untouched, select no keep-list paths, use manifest config defaults, and
// proceed with skills migration.
type PromptFuncs struct {
	OverwriteAllUnifiedPreviewFunc PromptOverwriteAllUnifiedPreviewFunc
	OverwritePreviewFunc           PromptOverwritePreviewFunc
	StatuslineSourcePreviewFunc    PromptStatuslineSourcePreviewFunc
	DeleteUnknownAllFunc           PromptDeleteUnknownAllFunc
	DeleteUnknownFunc              PromptDeleteUnknownFunc
	DeleteUnknownTmpAllFunc        PromptDeleteUnknownTmpAllFunc
	SelectUnknownsToKeepFunc       PromptSelectUnknownsToKeepFunc
	ConfigSetDefaultFunc           PromptConfigSetDefaultFunc
	ConfirmSkillsMigrationFunc     PromptConfirmSkillsMigrationFunc
}

// overwrite asks whether to overwrite a single path.
func (p *PromptFuncs) overwrite(preview DiffPreview) (bool, error) {
	if p == nil || p.OverwritePreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwritePreviewFunc(preview)
}

// statuslineSource keeps the source when no callback is set.
func (p *PromptFuncs) statuslineSource(preview DiffPreview) (bool, error) {
	if p == nil || p.StatuslineSourcePreviewFunc == nil {
		return false, nil
	}
	return p.StatuslineSourcePreviewFunc(preview)
}

// deleteUnknownAll asks whether to delete all unknown paths.
func (p *PromptFuncs) deleteUnknownAll(paths []string) (bool, error) {
	if p == nil || p.DeleteUnknownAllFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownAllFunc(paths)
}

// deleteUnknown asks whether to delete a single unknown path.
func (p *PromptFuncs) deleteUnknown(path string) (bool, error) {
	if p == nil || p.DeleteUnknownFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownFunc(path)
}

// deleteUnknownTmpAll leaves tmp untouched when no callback is set.
func (p *PromptFuncs) deleteUnknownTmpAll(paths []string) (bool, error) {
	if p == nil || p.DeleteUnknownTmpAllFunc == nil {
		return false, nil
	}
	return p.DeleteUnknownTmpAllFunc(paths)
}

// selectUnknownsToKeep selects nothing when no callback is set.
func (p *PromptFuncs) selectUnknownsToKeep(paths []string) ([]string, error) {
	if p == nil || p.SelectUnknownsToKeepFunc == nil {
		return nil, nil
	}
	return p.SelectUnknownsToKeepFunc(paths)
}

// configSetDefault uses the manifest value when no callback is set.
func (p *PromptFuncs) configSetDefault(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error) {
	if p == nil || p.ConfigSetDefaultFunc == nil {
		return manifestValue, nil
	}
	return p.ConfigSetDefaultFunc(key, manifestValue, rationale, field)
}

// confirmSkillsMigration proceeds when no callback is set.
func (p *PromptFuncs) confirmSkillsMigration(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error) {
	if p == nil || p.ConfirmSkillsMigrationFunc == nil {
		return true, nil
	}
	return p.ConfirmSkillsMigrationFunc(flatSkills, conflicts)
}

// hasStatuslineSource gates the file-reading diff preview build.
func (p *PromptFuncs) hasStatuslineSource() bool {
	return p != nil && p.StatuslineSourcePreviewFunc != nil
}

// validate checks the required callbacks before overwrite work begins.
func (p *PromptFuncs) validate() error {
	if p == nil || p.OverwriteAllUnifiedPreviewFunc == nil || p.OverwritePreviewFunc == nil {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if p.DeleteUnknownAllFunc == nil || p.DeleteUnknownFunc == nil {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return nil
}
