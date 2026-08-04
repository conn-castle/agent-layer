package config

import (
	"fmt"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

// Skill import tracking modes. An omitted tracking mode is inferred from the
// resolved ref kind at remote-resolution time (branches track, tags and commits
// pin), never guessed statically.
const (
	// SkillTrackingTracked advances the locked commit when `al skills pull` runs.
	SkillTrackingTracked = "tracked"
	// SkillTrackingPinned keeps the locked commit until configuration retargets it.
	SkillTrackingPinned = "pinned"
)

// Skill import write policies.
const (
	// SkillWriteNone is the default write policy: `al skills push` skips the block.
	SkillWriteNone = "none"
	// SkillWriteBranch pushes to an explicitly configured non-primary branch.
	SkillWriteBranch = "branch"
	// SkillWriteDirect pushes to the destination repository's default branch.
	SkillWriteDirect = "direct"
)

// DefaultSkillWrite is the write policy applied when a block omits `write`.
const DefaultSkillWrite = SkillWriteNone

// SkillSelectorExclusionPrefix marks a selector as an exclusion.
const SkillSelectorExclusionPrefix = "!"

// MaxSkillSelectorLength bounds a single configured selector so a pathological
// value cannot be pushed through glob matching or into a Git argument list.
const MaxSkillSelectorLength = 512

// gitDirName is a repository's own metadata directory. It is never part of a
// skill's file set and can never be selected.
const gitDirName = ".git"

// SkillsConfig holds the skill-import section of .agent-layer/config.toml.
type SkillsConfig struct {
	Imports []SkillImport `toml:"imports"`
}

// SkillImport is one `[[skills.imports]]` block: a single source repository plus
// the selectors that share its ref, tracking mode, and write policy.
type SkillImport struct {
	// Repository is the source Git repository. Its exact configured string is the
	// repository identity; Agent Layer never guesses that two spellings of a URL
	// name the same remote.
	Repository string `toml:"repository"`
	// Selectors are repository-relative skill paths. A `!` prefix marks an
	// exclusion. At least one positive selector is required.
	Selectors []string `toml:"selectors"`
	// Ref is a branch, tag, or commit. Empty means the repository's default branch.
	Ref string `toml:"ref"`
	// Tracking is "tracked" or "pinned". Empty defers to the resolved ref kind.
	Tracking string `toml:"tracking"`
	// Write is "none", "branch", or "direct". Empty means DefaultSkillWrite.
	Write string `toml:"write"`
	// PushRepository is the upstream write destination. Empty means Repository.
	PushRepository string `toml:"push_repository"`
	// PushBranch is required for Write == SkillWriteBranch and forbidden otherwise.
	PushBranch string `toml:"push_branch"`
}

// SkillImportPolicyKey is the full policy tuple that decides whether a new
// selector extends an existing block or needs a new one. Two blocks with an
// identical key are a configuration error, and `al skills add` extends the block
// whose key matches its resolved flags exactly.
type SkillImportPolicyKey struct {
	Repository     string
	Ref            string
	Tracking       string
	Write          string
	PushRepository string
	PushBranch     string
}

// PolicyKey returns the block's full policy tuple with configured strings
// normalized the same way validation normalizes them.
func (imp SkillImport) PolicyKey() SkillImportPolicyKey {
	return SkillImportPolicyKey{
		Repository:     NormalizeSkillRepository(imp.Repository),
		Ref:            strings.TrimSpace(imp.Ref),
		Tracking:       strings.TrimSpace(imp.Tracking),
		Write:          strings.TrimSpace(imp.Write),
		PushRepository: NormalizeSkillRepository(imp.PushRepository),
		PushBranch:     strings.TrimSpace(imp.PushBranch),
	}
}

// EffectiveWrite returns the block's write policy with the omitted case resolved
// to the named DefaultSkillWrite constant.
func (imp SkillImport) EffectiveWrite() string {
	write := strings.TrimSpace(imp.Write)
	if write == "" {
		return DefaultSkillWrite
	}
	return write
}

// EffectivePushRepository returns the destination repository for upstream
// writes: the configured push repository, or the source repository when omitted.
func (imp SkillImport) EffectivePushRepository() string {
	push := NormalizeSkillRepository(imp.PushRepository)
	if push == "" {
		return NormalizeSkillRepository(imp.Repository)
	}
	return push
}

// PositiveSelectors returns the block's normalized non-exclusion selectors.
func (imp SkillImport) PositiveSelectors() []string {
	var out []string
	for _, selector := range imp.Selectors {
		normalized, exclusion, err := ParseSkillSelector(selector)
		if err != nil || exclusion {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// ExclusionSelectors returns the block's normalized exclusion selectors without
// their `!` prefix.
func (imp SkillImport) ExclusionSelectors() []string {
	var out []string
	for _, selector := range imp.Selectors {
		normalized, exclusion, err := ParseSkillSelector(selector)
		if err != nil || !exclusion {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

// ignoredSkillResourceNames are the only names excluded from a skill's file
// set. Everything else — hidden files, dotfiles, nested resources — is part of
// the skill and is imported, hashed, merged, projected, and pushed. The set is
// defined here so every consumer (import, comparison, merge, projection, push)
// reads one list.
var ignoredSkillResourceNames = map[string]struct{}{
	gitDirName:  {},
	".DS_Store": {},
	"Thumbs.db": {},
}

// IsIgnoredSkillResourceName reports whether a path segment is excluded from a
// skill's canonical file set.
func IsIgnoredSkillResourceName(name string) bool {
	_, ok := ignoredSkillResourceNames[name]
	return ok
}

// NormalizeSkillRepository trims surrounding whitespace from a configured
// repository string. Nothing else is rewritten: the exact remaining string is
// the repository identity used for grouping, lock lookup, and overlap checks.
func NormalizeSkillRepository(repository string) string {
	return strings.TrimSpace(repository)
}

// NormalizeSkillImportName returns the comparison form of a skill name. It
// matches the normalization applied to local skill directory names so a Unicode
// homograph cannot smuggle in a second skill that collides on disk.
func NormalizeSkillImportName(name string) string {
	return normalizeSkillName(name)
}

// IsSafeSkillImportName reports whether name can be used as an imported-skill
// directory without normalization or path traversal.
func IsSafeSkillImportName(name string) bool {
	return NormalizeSkillImportName(name) == name && skillvalidator.IsValidSkillName(name)
}

// ParseSkillSelector normalizes one configured selector. It returns the
// slash-normalized selector body, whether the selector is an exclusion, and an
// error describing why an unusable selector was rejected.
func ParseSkillSelector(selector string) (normalized string, exclusion bool, err error) {
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return "", false, fmt.Errorf("selector must not be empty")
	}
	if len(raw) > MaxSkillSelectorLength {
		return "", false, fmt.Errorf("selector exceeds %d characters", MaxSkillSelectorLength)
	}
	if !utf8.ValidString(raw) {
		return "", false, fmt.Errorf("selector is not valid UTF-8")
	}
	exclusion = strings.HasPrefix(raw, SkillSelectorExclusionPrefix)
	body := strings.TrimSpace(strings.TrimPrefix(raw, SkillSelectorExclusionPrefix))
	if body == "" {
		return "", exclusion, fmt.Errorf("selector %q has no path after %q", selector, SkillSelectorExclusionPrefix)
	}
	if strings.ContainsRune(body, '\\') {
		return "", exclusion, fmt.Errorf("selector %q must use forward slashes", selector)
	}
	if strings.ContainsRune(body, 0) {
		return "", exclusion, fmt.Errorf("selector %q contains a NUL byte", selector)
	}
	if strings.HasPrefix(body, "/") {
		return "", exclusion, fmt.Errorf("selector %q must be repository-relative, not absolute", selector)
	}
	cleaned := path.Clean(body)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", exclusion, fmt.Errorf("selector %q must stay inside the repository", selector)
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" {
			return "", exclusion, fmt.Errorf("selector %q contains an empty path segment", selector)
		}
		if segment == gitDirName {
			return "", exclusion, fmt.Errorf("selector %q must not select Git internals", selector)
		}
	}
	return cleaned, exclusion, nil
}

// IsSkillSelectorWildcard reports whether a normalized selector body contains a
// glob metacharacter, which decides whether a miss is a hard error (exact) or an
// empty match (wildcard).
func IsSkillSelectorWildcard(selector string) bool {
	return strings.ContainsAny(selector, "*?[")
}

// validateSkills checks every static skill-import invariant that does not need a
// remote. Ref kinds and destination primary-branch checks are deliberately left
// to remote resolution, where the real answer exists.
func validateSkills(path string, skills SkillsConfig) error {
	type selectorOrigin struct {
		block int
	}
	seenSelectors := make(map[string]selectorOrigin, len(skills.Imports))
	seenPolicies := make(map[SkillImportPolicyKey]int, len(skills.Imports))

	for i, imp := range skills.Imports {
		repository := NormalizeSkillRepository(imp.Repository)
		if repository == "" {
			return fmt.Errorf("%s: skills.imports[%d].repository is required", path, i)
		}
		if err := validateSkillRepositoryString(repository); err != nil {
			return fmt.Errorf("%s: skills.imports[%d].repository %w", path, i, err)
		}
		if len(imp.Selectors) == 0 {
			return fmt.Errorf("%s: skills.imports[%d].selectors requires at least one selector", path, i)
		}

		positives := 0
		blockSelectors := make(map[string]struct{}, len(imp.Selectors))
		for j, selector := range imp.Selectors {
			normalized, exclusion, err := ParseSkillSelector(selector)
			if err != nil {
				return fmt.Errorf("%s: skills.imports[%d].selectors[%d]: %w", path, i, j, err)
			}
			if !exclusion {
				positives++
			}
			key := selectorIdentity(repository, normalized, exclusion)
			if _, ok := blockSelectors[key]; ok {
				return fmt.Errorf("%s: skills.imports[%d].selectors[%d] %q is listed twice in the same block", path, i, j, selector)
			}
			blockSelectors[key] = struct{}{}
			// Repository+selector pairs are unique across the whole file so
			// `al skills remove <repository> <selector>` always names one selector.
			if origin, ok := seenSelectors[key]; ok {
				return fmt.Errorf(
					"%s: skills.imports[%d].selectors[%d] %q already appears in skills.imports[%d] for repository %q; each repository and selector pair must be unique so 'al skills remove' identifies one selector",
					path, i, j, selector, origin.block, repository,
				)
			}
			seenSelectors[key] = selectorOrigin{block: i}
		}
		if positives == 0 {
			return fmt.Errorf(
				"%s: skills.imports[%d].selectors requires at least one positive selector; an exclusion never imports a skill by itself",
				path, i,
			)
		}

		if err := validateSkillTracking(path, i, imp.Tracking); err != nil {
			return err
		}
		if err := validateSkillWrite(path, i, imp); err != nil {
			return err
		}
		if pushRepository := NormalizeSkillRepository(imp.PushRepository); pushRepository != "" {
			if err := validateSkillRepositoryString(pushRepository); err != nil {
				return fmt.Errorf("%s: skills.imports[%d].push_repository %w", path, i, err)
			}
		}
		if ref := strings.TrimSpace(imp.Ref); ref != "" {
			if err := validateSkillRefString(ref); err != nil {
				return fmt.Errorf("%s: skills.imports[%d].ref %w", path, i, err)
			}
		}

		key := imp.PolicyKey()
		if first, ok := seenPolicies[key]; ok {
			return fmt.Errorf(
				"%s: skills.imports[%d] repeats the repository, ref, tracking, write, and push policy of skills.imports[%d]; merge their selectors into one block",
				path, i, first,
			)
		}
		seenPolicies[key] = i
	}
	return nil
}

// selectorIdentity keys a selector by repository, exclusion sense, and body so a
// positive and an exclusion selector for the same path stay distinguishable.
func selectorIdentity(repository string, normalized string, exclusion bool) string {
	sense := "+"
	if exclusion {
		sense = "-"
	}
	return repository + "\x00" + sense + normalized
}

func validateSkillTracking(configPath string, index int, tracking string) error {
	switch strings.TrimSpace(tracking) {
	case "", SkillTrackingTracked, SkillTrackingPinned:
		return nil
	default:
		return fmt.Errorf(
			"%s: skills.imports[%d].tracking must be %q or %q",
			configPath, index, SkillTrackingTracked, SkillTrackingPinned,
		)
	}
}

func validateSkillWrite(configPath string, index int, imp SkillImport) error {
	write := strings.TrimSpace(imp.Write)
	switch write {
	case "", SkillWriteNone, SkillWriteBranch, SkillWriteDirect:
	default:
		return fmt.Errorf(
			"%s: skills.imports[%d].write must be %q, %q, or %q",
			configPath, index, SkillWriteNone, SkillWriteBranch, SkillWriteDirect,
		)
	}

	pushBranch := strings.TrimSpace(imp.PushBranch)
	if imp.EffectiveWrite() == SkillWriteBranch {
		if pushBranch == "" {
			return fmt.Errorf(
				"%s: skills.imports[%d].push_branch is required when write = %q; Agent Layer never generates branch names",
				configPath, index, SkillWriteBranch,
			)
		}
		if err := validateSkillRefString(pushBranch); err != nil {
			return fmt.Errorf("%s: skills.imports[%d].push_branch %w", configPath, index, err)
		}
		return nil
	}
	if pushBranch != "" {
		return fmt.Errorf(
			"%s: skills.imports[%d].push_branch is only valid when write = %q",
			configPath, index, SkillWriteBranch,
		)
	}
	return nil
}

// validateSkillRepositoryString rejects repository strings Agent Layer refuses to
// hand to git: control characters, option-looking values, and embedded URL
// userinfo (which would put a credential in repo-tracked configuration).
func validateSkillRepositoryString(repository string) error {
	if strings.HasPrefix(repository, "-") {
		return fmt.Errorf("%q must not start with '-'", repository)
	}
	if strings.ContainsAny(repository, "\x00\n\r") {
		return fmt.Errorf("%q must not contain control characters", repository)
	}
	if err := rejectRepositoryUserinfo(repository); err != nil {
		return err
	}
	return nil
}

// rejectRepositoryUserinfo fails a repository string that carries userinfo.
// Credentials belong in the user's existing Git authentication, never in
// .agent-layer/config.toml.
func rejectRepositoryUserinfo(repository string) error {
	parsed, err := url.Parse(repository)
	if err == nil && parsed.User != nil {
		return fmt.Errorf("must not embed credentials; configure Git authentication instead")
	}
	// scp-style remotes (user@host:path) are ordinary SSH syntax and carry no
	// secret, but an embedded password (user:secret@host:path) is rejected.
	if at := strings.Index(repository, "@"); at > 0 && !strings.Contains(repository, "://") {
		if strings.Contains(repository[:at], ":") {
			return fmt.Errorf("must not embed credentials; configure Git authentication instead")
		}
	}
	return nil
}

// validateSkillRefString rejects ref strings git would refuse or that could be
// read as a command-line option.
func validateSkillRefString(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("%q must not start with '-'", ref)
	}
	if strings.ContainsAny(ref, "\x00\n\r \t~^:?*[\\") {
		return fmt.Errorf("%q contains characters git does not accept in a ref", ref)
	}
	if strings.Contains(ref, "..") || strings.HasSuffix(ref, ".lock") || strings.HasSuffix(ref, "/") {
		return fmt.Errorf("%q is not a valid git ref", ref)
	}
	return nil
}

// SkillImportSelectorRef locates one configured selector.
type SkillImportSelectorRef struct {
	BlockIndex    int
	SelectorIndex int
	Normalized    string
	Exclusion     bool
}

// FindSkillImportSelector locates the single block and selector matching a
// repository and raw selector string. It returns false when the pair is absent.
func FindSkillImportSelector(skills SkillsConfig, repository string, selector string) (SkillImportSelectorRef, bool) {
	wantRepository := NormalizeSkillRepository(repository)
	wantBody, wantExclusion, err := ParseSkillSelector(selector)
	if err != nil {
		return SkillImportSelectorRef{}, false
	}
	for i, imp := range skills.Imports {
		if NormalizeSkillRepository(imp.Repository) != wantRepository {
			continue
		}
		for j, candidate := range imp.Selectors {
			body, exclusion, err := ParseSkillSelector(candidate)
			if err != nil {
				continue
			}
			if body == wantBody && exclusion == wantExclusion {
				return SkillImportSelectorRef{
					BlockIndex:    i,
					SelectorIndex: j,
					Normalized:    body,
					Exclusion:     exclusion,
				}, true
			}
		}
	}
	return SkillImportSelectorRef{}, false
}
