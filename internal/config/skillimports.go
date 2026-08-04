package config

import (
	"fmt"
	"path"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skilllock"
)

// Skill import tracking modes. An omitted value is resolved from the source
// ref kind during the first networked add or pull: branch refs become
// SkillTrackingTracked and tag/commit refs become SkillTrackingPinned.
//
// The values come from internal/skilllock, which persists them, so
// configuration and recorded state can never drift apart.
const (
	// SkillTrackingTracked follows the configured branch on `al skills pull`.
	SkillTrackingTracked = skilllock.TrackingTracked
	// SkillTrackingPinned holds the locked commit until an explicit retarget.
	SkillTrackingPinned = skilllock.TrackingPinned
)

// Skill import write policies.
const (
	// SkillWritePolicyNone disables upstream writes. It is the default.
	SkillWritePolicyNone = "none"
	// SkillWritePolicyBranch pushes to an explicitly configured non-primary branch.
	SkillWritePolicyBranch = "branch"
	// SkillWritePolicyDirect pushes to the destination repository's default branch.
	SkillWritePolicyDirect = "direct"
)

// SkillExclusionPrefix marks a selector that removes candidates from its own
// block's desired set.
const SkillExclusionPrefix = "!"

// primaryBranchNames are branch names Agent Layer refuses to accept as an
// explicit `branch` write destination. `direct` is the documented way to write
// to a destination default branch.
var primaryBranchNames = map[string]struct{}{
	"main":   {},
	"master": {},
	"HEAD":   {},
}

// SkillsConfig groups skill-source configuration.
type SkillsConfig struct {
	Imports []SkillImport `toml:"imports"`
}

// SkillImport declares one Git-backed skill import block. Every selector in a
// block shares the block's repository, source ref, tracking mode, write policy,
// and push destination; per-selector overrides are unsupported.
type SkillImport struct {
	// Repository is the source Git repository reachable through the user's
	// existing Git authentication.
	Repository string `toml:"repository"`
	// Selectors are exact paths, path wildcards, or `!`-prefixed exclusions.
	Selectors []string `toml:"selectors"`
	// Ref is a branch, tag, or commit. An empty value resolves to the
	// repository's default branch on every pull.
	Ref string `toml:"ref"`
	// Tracking is SkillTrackingTracked or SkillTrackingPinned. An empty value is
	// resolved from the source ref kind during the first networked operation.
	Tracking string `toml:"tracking"`
	// WritePolicy is SkillWritePolicyNone, SkillWritePolicyBranch, or
	// SkillWritePolicyDirect. An empty value means SkillWritePolicyNone.
	WritePolicy string `toml:"write_policy"`
	// PushRepository is the destination repository. An empty value means the
	// source repository, which permits fork-based contribution when set.
	PushRepository string `toml:"push_repository"`
	// PushBranch is the required explicit destination branch for
	// SkillWritePolicyBranch.
	PushBranch string `toml:"push_branch"`
}

// EffectiveWritePolicy returns the block's write policy with the documented
// `none` default applied.
func (imp SkillImport) EffectiveWritePolicy() string {
	policy := strings.TrimSpace(imp.WritePolicy)
	if policy == "" {
		return SkillWritePolicyNone
	}
	return policy
}

// EffectivePushRepository returns the destination repository for upstream
// writes, falling back to the source repository when no fork is configured.
func (imp SkillImport) EffectivePushRepository() string {
	pushRepository := strings.TrimSpace(imp.PushRepository)
	if pushRepository == "" {
		return strings.TrimSpace(imp.Repository)
	}
	return pushRepository
}

// WriteEnabled reports whether the block permits upstream writes.
func (imp SkillImport) WriteEnabled() bool {
	return imp.EffectiveWritePolicy() != SkillWritePolicyNone
}

// PositiveSelectors returns the block's non-exclusion selectors in
// configuration order.
func (imp SkillImport) PositiveSelectors() []string {
	positives := make([]string, 0, len(imp.Selectors))
	for _, selector := range imp.Selectors {
		if !IsSkillExclusionSelector(selector) {
			positives = append(positives, strings.TrimSpace(selector))
		}
	}
	return positives
}

// ExclusionSelectors returns the block's exclusion selectors with the `!`
// prefix removed, in configuration order.
func (imp SkillImport) ExclusionSelectors() []string {
	exclusions := make([]string, 0, len(imp.Selectors))
	for _, selector := range imp.Selectors {
		if IsSkillExclusionSelector(selector) {
			exclusions = append(exclusions, SkillExclusionPath(selector))
		}
	}
	return exclusions
}

// IsSkillExclusionSelector reports whether a configured selector removes
// candidates instead of adding them.
func IsSkillExclusionSelector(selector string) bool {
	return strings.HasPrefix(strings.TrimSpace(selector), SkillExclusionPrefix)
}

// SkillExclusionPath returns the selector path with any exclusion prefix
// removed.
func SkillExclusionPath(selector string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(selector), SkillExclusionPrefix))
}

// SkillImportBlockIdentity is the tuple that makes two import blocks
// interchangeable. Configuration keeps one block per unique identity so
// selector additions with the same policy extend an existing block.
type SkillImportBlockIdentity struct {
	Repository     string
	Ref            string
	Tracking       string
	WritePolicy    string
	PushRepository string
	PushBranch     string
}

// Identity returns the block's policy identity with defaults applied.
func (imp SkillImport) Identity() SkillImportBlockIdentity {
	return SkillImportBlockIdentity{
		Repository:     NormalizeSkillRepository(imp.Repository),
		Ref:            strings.TrimSpace(imp.Ref),
		Tracking:       strings.TrimSpace(imp.Tracking),
		WritePolicy:    imp.EffectiveWritePolicy(),
		PushRepository: NormalizeSkillRepository(imp.EffectivePushRepository()),
		PushBranch:     strings.TrimSpace(imp.PushBranch),
	}
}

// NormalizeSkillRepository trims a configured repository reference so the same
// remote written with incidental whitespace or a trailing slash resolves to one
// configuration identity. It does not rewrite scheme, host, or path.
func NormalizeSkillRepository(repository string) string {
	normalized := strings.TrimSpace(repository)
	for strings.HasSuffix(normalized, "/") {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	return normalized
}

// NormalizeSkillSelector trims a selector and normalizes its path separators so
// selector identity does not depend on incidental formatting. The `!` exclusion
// prefix is preserved.
func NormalizeSkillSelector(selector string) string {
	trimmed := strings.TrimSpace(selector)
	prefix := ""
	if strings.HasPrefix(trimmed, SkillExclusionPrefix) {
		prefix = SkillExclusionPrefix
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, SkillExclusionPrefix))
	}
	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	for strings.HasSuffix(trimmed, "/") {
		trimmed = strings.TrimSuffix(trimmed, "/")
	}
	if trimmed == "" {
		return prefix
	}
	return prefix + path.Clean(trimmed)
}

// validateSkills enforces every static skill-import rule that does not require
// contacting a remote. Remote-dependent identity rules (name collisions,
// ancestor/descendant overlap of resolved paths) are enforced during selector
// resolution, where the actual source tree is known.
func validateSkills(configPath string, skills SkillsConfig) error {
	seenPairs := make(map[string]int, len(skills.Imports))
	seenIdentities := make(map[SkillImportBlockIdentity]int, len(skills.Imports))
	for i, imp := range skills.Imports {
		if err := validateSkillImportBlock(configPath, i, imp); err != nil {
			return err
		}
		identity := imp.Identity()
		if first, ok := seenIdentities[identity]; ok {
			return fmt.Errorf(messages.ConfigSkillImportDuplicateBlockFmt, configPath, i, first)
		}
		seenIdentities[identity] = i

		for _, selector := range imp.Selectors {
			key := identity.Repository + "\x00" + NormalizeSkillSelector(selector)
			if first, ok := seenPairs[key]; ok {
				return fmt.Errorf(messages.ConfigSkillImportDuplicateSelectorFmt, configPath, i, strings.TrimSpace(selector), first)
			}
			seenPairs[key] = i
		}
	}
	return nil
}

func validateSkillImportBlock(configPath string, index int, imp SkillImport) error {
	if NormalizeSkillRepository(imp.Repository) == "" {
		return fmt.Errorf(messages.ConfigSkillImportRepositoryRequiredFmt, configPath, index)
	}
	if len(imp.Selectors) == 0 {
		return fmt.Errorf(messages.ConfigSkillImportSelectorsRequiredFmt, configPath, index)
	}
	positives := 0
	for _, selector := range imp.Selectors {
		if err := validateSkillSelector(configPath, index, selector); err != nil {
			return err
		}
		if !IsSkillExclusionSelector(selector) {
			positives++
		}
	}
	if positives == 0 {
		return fmt.Errorf(messages.ConfigSkillImportPositiveSelectorRequiredFmt, configPath, index)
	}

	switch strings.TrimSpace(imp.Tracking) {
	case "", SkillTrackingTracked, SkillTrackingPinned:
	default:
		return fmt.Errorf(messages.ConfigSkillImportTrackingInvalidFmt, configPath, index, imp.Tracking)
	}

	policy := imp.EffectiveWritePolicy()
	switch policy {
	case SkillWritePolicyNone, SkillWritePolicyBranch, SkillWritePolicyDirect:
	default:
		return fmt.Errorf(messages.ConfigSkillImportWritePolicyInvalidFmt, configPath, index, imp.WritePolicy)
	}

	pushBranch := strings.TrimSpace(imp.PushBranch)
	if policy == SkillWritePolicyBranch {
		if pushBranch == "" {
			return fmt.Errorf(messages.ConfigSkillImportPushBranchRequiredFmt, configPath, index)
		}
		if _, primary := primaryBranchNames[pushBranch]; primary {
			return fmt.Errorf(messages.ConfigSkillImportPushBranchPrimaryFmt, configPath, index, pushBranch)
		}
	} else if pushBranch != "" {
		return fmt.Errorf(messages.ConfigSkillImportPushBranchUnsupportedFmt, configPath, index, policy)
	}

	if policy == SkillWritePolicyNone && strings.TrimSpace(imp.PushRepository) != "" {
		return fmt.Errorf(messages.ConfigSkillImportPushRepositoryUnsupportedFmt, configPath, index)
	}
	return nil
}

func validateSkillSelector(configPath string, index int, selector string) error {
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return fmt.Errorf(messages.ConfigSkillImportSelectorEmptyFmt, configPath, index)
	}
	value := SkillExclusionPath(raw)
	if value == "" {
		return fmt.Errorf(messages.ConfigSkillImportSelectorEmptyFmt, configPath, index)
	}
	if err := ValidateSkillSelectorPath(value); err != nil {
		return fmt.Errorf(messages.ConfigSkillImportSelectorInvalidFmt, configPath, index, raw, err)
	}
	return nil
}

// ValidateSkillSelectorPath enforces the repository-relative selector path
// syntax shared by configuration validation and `al skills add`/`remove`.
func ValidateSkillSelectorPath(value string) error {
	if strings.Contains(value, "\\") {
		return fmt.Errorf(messages.ConfigSkillSelectorBackslash)
	}
	if strings.HasPrefix(value, "/") {
		return fmt.Errorf(messages.ConfigSkillSelectorAbsolute)
	}
	if value != path.Clean(value) {
		return fmt.Errorf(messages.ConfigSkillSelectorNotNormalized)
	}
	for _, segment := range strings.Split(value, "/") {
		switch segment {
		case "", ".", "..":
			return fmt.Errorf(messages.ConfigSkillSelectorNotNormalized)
		}
	}
	return nil
}
