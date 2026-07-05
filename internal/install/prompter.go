package install

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// Prompter provides user prompts for overwrite and delete decisions.
type Prompter interface {
	OverwriteAll(previews []DiffPreview) (bool, error)
	OverwriteAllMemory(previews []DiffPreview) (bool, error)
	Overwrite(preview DiffPreview) (bool, error)
	DeleteUnknownAll(paths []string) (bool, error)
	DeleteUnknown(path string) (bool, error)
}

// PromptOverwriteAllPreviewFunc asks whether to overwrite all paths in a diff preview batch.
type PromptOverwriteAllPreviewFunc func(previews []DiffPreview) (bool, error)

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

// PromptFuncs adapts optional prompt callbacks into a Prompter.
type PromptFuncs struct {
	OverwriteAllPreviewFunc        PromptOverwriteAllPreviewFunc
	OverwriteAllMemoryPreviewFunc  PromptOverwriteAllPreviewFunc
	OverwriteAllUnifiedPreviewFunc PromptOverwriteAllUnifiedPreviewFunc
	OverwritePreviewFunc           PromptOverwritePreviewFunc
	StatuslineSourcePreviewFunc    PromptStatuslineSourcePreviewFunc
	DeleteUnknownAllFunc           PromptDeleteUnknownAllFunc
	DeleteUnknownFunc              PromptDeleteUnknownFunc
	DeleteUnknownTmpAllFunc        PromptDeleteUnknownTmpAllFunc
	ConfigSetDefaultFunc           PromptConfigSetDefaultFunc
	ConfirmSkillsMigrationFunc     PromptConfirmSkillsMigrationFunc
}

// OverwriteAll prompts the user to confirm overwriting all given paths.
// Returns an error if no overwrite callback is configured.
func (p PromptFuncs) OverwriteAll(previews []DiffPreview) (bool, error) {
	if p.OverwriteAllPreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllPreviewFunc(previews)
}

// OverwriteAllMemory prompts the user to confirm overwriting all memory file paths.
// Returns an error if no OverwriteAll callback is configured.
func (p PromptFuncs) OverwriteAllMemory(previews []DiffPreview) (bool, error) {
	if p.OverwriteAllMemoryPreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllMemoryPreviewFunc(previews)
}

// OverwriteAllUnified prompts for managed and memory overwrite-all decisions in one pass.
// Returns an error if no unified callback is configured.
func (p PromptFuncs) OverwriteAllUnified(managed []DiffPreview, memory []DiffPreview) (bool, bool, error) {
	if p.OverwriteAllUnifiedPreviewFunc == nil {
		return false, false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwriteAllUnifiedPreviewFunc(managed, memory)
}

// Overwrite prompts the user to confirm overwriting a single path.
// Returns an error if no Overwrite callback is configured.
func (p PromptFuncs) Overwrite(preview DiffPreview) (bool, error) {
	if p.OverwritePreviewFunc == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	return p.OverwritePreviewFunc(preview)
}

// StatuslineSource prompts for a user-owned statusline source replacement.
// Returns false when no callback is set so headless/non-integrated prompters
// never overwrite these files silently.
func (p PromptFuncs) StatuslineSource(preview DiffPreview) (bool, error) {
	if p.StatuslineSourcePreviewFunc == nil {
		return false, nil
	}
	return p.StatuslineSourcePreviewFunc(preview)
}

// DeleteUnknownAll prompts the user to confirm deleting all unknown paths.
// Returns an error if no DeleteUnknownAllFunc is configured.
func (p PromptFuncs) DeleteUnknownAll(paths []string) (bool, error) {
	if p.DeleteUnknownAllFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownAllFunc(paths)
}

// DeleteUnknown prompts the user to confirm deleting a single unknown path.
// Returns an error if no DeleteUnknownFunc is configured.
func (p PromptFuncs) DeleteUnknown(path string) (bool, error) {
	if p.DeleteUnknownFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownFunc(path)
}

// DeleteUnknownTmpAll prompts the user to confirm deleting all unknown paths
// under .agent-layer/tmp/ as a group. Returns an error when no
// DeleteUnknownTmpAllFunc is configured, mirroring the no-silent-fallback
// behavior of the other prompt callbacks. The grouped-vs-untouched fallback for
// callers that construct a PromptFuncs without wiring DeleteUnknownTmpAllFunc —
// and for legacy Prompter implementations that don't implement
// tmpUnknownsPrompter at all — is owned by the promptRouter, which probes
// promptValidator (see newPromptRouter) and leaves tmp paths untouched rather
// than invoking this method.
func (p PromptFuncs) DeleteUnknownTmpAll(paths []string) (bool, error) {
	if p.DeleteUnknownTmpAllFunc == nil {
		return false, fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return p.DeleteUnknownTmpAllFunc(paths)
}

// configSetDefaultPrompter is an optional interface that a Prompter can
// implement to interactively confirm or customize config_set_default
// migration values. When the Prompter does not implement this interface (or
// PromptFuncs has no callback wired), the migration uses the manifest value
// directly.
type configSetDefaultPrompter interface {
	ConfigSetDefault(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error)
}

// ConfigSetDefault prompts the user to confirm or customize a default value
// for a missing config key. Returns the manifest value when no callback is set.
func (p PromptFuncs) ConfigSetDefault(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error) {
	if p.ConfigSetDefaultFunc == nil {
		return manifestValue, nil
	}
	return p.ConfigSetDefaultFunc(key, manifestValue, rationale, field)
}

// SkillsMigrationConflict describes a flat-format skill that conflicts with an
// existing directory-format skill (different content at both locations).
type SkillsMigrationConflict struct {
	SkillName string
	FlatPath  string
	DirPath   string
	Reason    string
}

// skillsMigrationPrompter is an optional interface that a Prompter can
// implement to confirm skills-format migration with the user. When the
// Prompter does not implement this interface (or the callback is nil),
// migration proceeds automatically (headless default).
type skillsMigrationPrompter interface {
	ConfirmSkillsMigration(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error)
}

type statuslineSourcePrompter interface {
	StatuslineSource(preview DiffPreview) (bool, error)
}

// PromptConfirmSkillsMigrationFunc asks the user to confirm the skills-format
// migration. It receives the list of flat skills to migrate and any detected
// conflicts. Returns true to proceed, false to abort.
type PromptConfirmSkillsMigrationFunc func(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error)

// ConfirmSkillsMigration prompts the user to confirm skills-format migration.
// Returns true (proceed) when no callback is set (headless default).
func (p PromptFuncs) ConfirmSkillsMigration(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error) {
	if p.ConfirmSkillsMigrationFunc == nil {
		return true, nil
	}
	return p.ConfirmSkillsMigrationFunc(flatSkills, conflicts)
}

type promptValidator interface {
	hasOverwriteAll() bool
	hasOverwriteAllMemory() bool
	hasOverwriteAllUnified() bool
	hasOverwrite() bool
	hasDeleteUnknownAll() bool
	hasDeleteUnknown() bool
	hasDeleteUnknownTmpAll() bool
}

// tmpUnknownsPrompter is an optional capability a Prompter can implement to
// collapse the per-file delete prompts for files under .agent-layer/tmp/ into
// a single grouped yes/no question. Detection is two-step: a Prompter must
// satisfy this interface AND, when it also implements promptValidator, report
// hasDeleteUnknownTmpAll() == true. The second step exists because PromptFuncs
// always satisfies this interface (the method is defined on the struct), so
// callers that build a PromptFuncs without wiring DeleteUnknownTmpAllFunc
// would otherwise hit the "prompt required" error instead of leaving tmp paths
// untouched. newPromptRouter performs both probes.
type tmpUnknownsPrompter interface {
	DeleteUnknownTmpAll(paths []string) (bool, error)
}

func (p PromptFuncs) hasOverwriteAll() bool {
	return p.OverwriteAllPreviewFunc != nil
}

func (p PromptFuncs) hasOverwriteAllMemory() bool {
	return p.OverwriteAllMemoryPreviewFunc != nil
}

func (p PromptFuncs) hasOverwriteAllUnified() bool {
	return p.OverwriteAllUnifiedPreviewFunc != nil
}

func (p PromptFuncs) hasOverwrite() bool {
	return p.OverwritePreviewFunc != nil
}

func (p PromptFuncs) hasDeleteUnknownAll() bool {
	return p.DeleteUnknownAllFunc != nil
}

func (p PromptFuncs) hasDeleteUnknown() bool {
	return p.DeleteUnknownFunc != nil
}

func (p PromptFuncs) hasDeleteUnknownTmpAll() bool {
	return p.DeleteUnknownTmpAllFunc != nil
}

func (p PromptFuncs) hasStatuslineSource() bool {
	return p.StatuslineSourcePreviewFunc != nil
}

// unifiedOverwritePrompter is an optional interface a Prompter can implement to
// resolve the managed and memory overwrite-all decisions in a single pass. The
// router only selects it when the prompter also implements promptValidator and
// reports the unified callback as wired (see newPromptRouter); a prompter that
// implements this interface without promptValidator keeps the separate managed
// and memory overwrite-all prompts.
type unifiedOverwritePrompter interface {
	OverwriteAllUnified(managed []DiffPreview, memory []DiffPreview) (bool, bool, error)
}

type statuslineSourceValidator interface {
	hasStatuslineSource() bool
}

// promptKind identifies which prompt category a promptRequest represents.
type promptKind int

const (
	promptKindOverwriteAll promptKind = iota
	promptKindOverwriteAllMemory
	promptKindOverwriteAllUnified
	promptKindOverwrite
	promptKindStatuslineSource
	promptKindDeleteUnknownAll
	promptKindDeleteUnknown
	promptKindDeleteUnknownTmpAll
	promptKindConfigSetDefault
	promptKindConfirmSkillsMigration
)

// promptRequest carries the data a single prompt category needs. Only the
// fields relevant to kind are populated by the caller.
type promptRequest struct {
	kind promptKind

	previews       []DiffPreview // overwrite-all managed batch (also unified managed batch)
	memoryPreviews []DiffPreview // unified memory batch
	preview        DiffPreview   // single overwrite / statusline source

	paths []string // delete-unknown-all / delete-unknown-tmp-all
	path  string   // single delete-unknown

	configKey     string
	manifestValue any
	rationale     string
	field         *config.FieldDef

	flatSkills []string
	conflicts  []SkillsMigrationConflict
}

// promptResponse carries a prompt outcome. Which fields are meaningful depends
// on the request kind: approved is the primary yes/no decision, approvedMemory
// is the second unified overwrite-all decision, and value is the resolved
// config default.
type promptResponse struct {
	approved       bool
	approvedMemory bool
	value          any
}

// promptRouter is the single place install/upgrade prompt decisions flow
// through. It wraps a Prompter, resolves the optional prompt capabilities once
// under the fixed capability policy, and applies the fallback for each optional
// prompt category so callers no longer repeat optional-interface probing.
type promptRouter struct {
	prompter      Prompter
	unified       unifiedOverwritePrompter
	tmpUnknowns   tmpUnknownsPrompter
	statusline    statuslineSourcePrompter
	configDefault configSetDefaultPrompter
	skills        skillsMigrationPrompter
}

// newPromptRouter resolves prompter's optional prompt capabilities under the
// current capability policy. It accepts a nil prompter so validation can run
// before an installer exists.
func newPromptRouter(prompter Prompter) *promptRouter {
	r := &promptRouter{prompter: prompter}
	if prompter == nil {
		return r
	}
	// Unified overwrite requires the unified interface AND a promptValidator
	// that reports the unified callback as wired. A prompter that implements
	// the interface but not promptValidator does NOT get unified overwrite,
	// preserving the separate managed and memory overwrite-all prompts.
	if unified, ok := prompter.(unifiedOverwritePrompter); ok && unified != nil {
		if validator, vok := prompter.(promptValidator); vok && validator.hasOverwriteAllUnified() {
			r.unified = unified
		}
	}
	// Tmp grouped deletion requires the tmp interface and, when the prompter
	// also implements promptValidator, the tmp callback reported as wired. A
	// prompter without promptValidator keeps the capability (legacy Prompter
	// implementations). PromptFuncs always satisfies the interface, so the
	// validator probe distinguishes a wired callback from a zero value; when it
	// is unwired, tmp paths are left untouched rather than routed elsewhere.
	if grouped, ok := prompter.(tmpUnknownsPrompter); ok {
		wired := true
		if validator, vok := prompter.(promptValidator); vok && !validator.hasDeleteUnknownTmpAll() {
			wired = false
		}
		if wired {
			r.tmpUnknowns = grouped
		}
	}
	if statusline, ok := prompter.(statuslineSourcePrompter); ok {
		wired := true
		if validator, vok := prompter.(statuslineSourceValidator); vok && !validator.hasStatuslineSource() {
			wired = false
		}
		if wired {
			r.statusline = statusline
		}
	}
	if configDefault, ok := prompter.(configSetDefaultPrompter); ok {
		r.configDefault = configDefault
	}
	if skills, ok := prompter.(skillsMigrationPrompter); ok {
		r.skills = skills
	}
	return r
}

// hasUnifiedOverwrite reports whether the wrapped prompter resolves the managed
// and memory overwrite-all decisions in one unified pass.
func (r *promptRouter) hasUnifiedOverwrite() bool { return r.unified != nil }

// hasStatuslineSource reports whether the wrapped prompter can prompt for a
// user-owned statusline source replacement. Callers gate the (file-reading)
// diff-preview build on this when the prompter lacks the optional interface.
func (r *promptRouter) hasStatuslineSource() bool { return r.statusline != nil }

// validateRequiredOverwrite enforces that a Prompter used in overwrite mode
// wires the required core overwrite and delete callbacks before any overwrite
// work begins. It preserves the historical early-error messages.
func (r *promptRouter) validateRequiredOverwrite() error {
	if r.prompter == nil {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	validator, ok := r.prompter.(promptValidator)
	if !ok {
		return nil
	}
	if !validator.hasOverwriteAll() {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if !validator.hasOverwriteAllMemory() {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if !validator.hasOverwrite() {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if !validator.hasDeleteUnknownAll() {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	if !validator.hasDeleteUnknown() {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	return nil
}

// route dispatches a prompt request to the wrapped prompter, applying the fixed
// fallback policy for the optional prompt categories. It is the single entry
// point for every install/upgrade prompt decision.
func (r *promptRouter) route(req promptRequest) (promptResponse, error) {
	switch req.kind {
	case promptKindOverwriteAll:
		approved, err := r.prompter.OverwriteAll(req.previews)
		return promptResponse{approved: approved}, err
	case promptKindOverwriteAllMemory:
		approved, err := r.prompter.OverwriteAllMemory(req.previews)
		return promptResponse{approved: approved}, err
	case promptKindOverwriteAllUnified:
		managed, memory, err := r.unified.OverwriteAllUnified(req.previews, req.memoryPreviews)
		return promptResponse{approved: managed, approvedMemory: memory}, err
	case promptKindOverwrite:
		approved, err := r.prompter.Overwrite(req.preview)
		return promptResponse{approved: approved}, err
	case promptKindStatuslineSource:
		// Missing statusline prompt keeps an existing customized source.
		if r.statusline == nil {
			return promptResponse{}, nil
		}
		approved, err := r.statusline.StatuslineSource(req.preview)
		return promptResponse{approved: approved}, err
	case promptKindDeleteUnknownAll:
		approved, err := r.prompter.DeleteUnknownAll(req.paths)
		return promptResponse{approved: approved}, err
	case promptKindDeleteUnknown:
		approved, err := r.prompter.DeleteUnknown(req.path)
		return promptResponse{approved: approved}, err
	case promptKindDeleteUnknownTmpAll:
		// Missing grouped tmp prompt leaves tmp paths untouched; tmp deletion
		// only ever happens through this dedicated destructive confirmation.
		if r.tmpUnknowns == nil {
			return promptResponse{}, nil
		}
		approved, err := r.tmpUnknowns.DeleteUnknownTmpAll(req.paths)
		return promptResponse{approved: approved}, err
	case promptKindConfigSetDefault:
		// Missing config-default prompt uses the migration manifest value.
		if r.configDefault == nil {
			return promptResponse{value: req.manifestValue}, nil
		}
		value, err := r.configDefault.ConfigSetDefault(req.configKey, req.manifestValue, req.rationale, req.field)
		return promptResponse{value: value}, err
	case promptKindConfirmSkillsMigration:
		// Missing skills-migration prompt proceeds (headless default).
		if r.skills == nil {
			return promptResponse{approved: true}, nil
		}
		approved, err := r.skills.ConfirmSkillsMigration(req.flatSkills, req.conflicts)
		return promptResponse{approved: approved}, err
	default:
		return promptResponse{}, fmt.Errorf("install: unknown prompt kind %d", req.kind)
	}
}
