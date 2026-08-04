// Package skilllock owns `.agent-layer/skills.lock.json`, the machine-managed
// record of what Agent Layer actually imported from each configured Git source.
//
// The lockfile is the canonical merge base for every skill import operation:
// it records the resolved source ref, commit, and upstream tree hash for each
// imported skill so pull and push can reconcile local edits without inferring
// state. It is schema-versioned, stable-sorted, strictly decoded, and written
// atomically so a partially written file can never be mistaken for valid state.
package skilllock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/envref"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// Version is the current lockfile schema version. A file recording a different
// version is rejected rather than guessed at.
const Version = 1

// Tracking modes recorded for an imported skill. They are declared here because
// the lockfile is where they are persisted; internal/config exposes the same
// values as its configuration vocabulary.
const (
	// TrackingTracked follows the configured branch on `al skills pull`.
	TrackingTracked = "tracked"
	// TrackingPinned holds the locked commit until an explicit retarget.
	TrackingPinned = "pinned"
)

// commitIDLengths are the object-id widths Git uses. Anything else is not an
// object id and must never reach a Git command or a merge-base comparison.
var commitIDLengths = map[int]struct{}{40: {}, 64: {}}

// treeHashPrefix is the algorithm prefix every canonical skill tree hash
// carries. skilltree.Tree.Hash is the only producer.
const treeHashPrefix = "sha256:"

// Ref kinds recorded from remote resolution evidence. Offline code paths read
// these instead of guessing whether a configured ref names a branch.
const (
	// RefKindBranch marks a ref resolved to refs/heads/<name>.
	RefKindBranch = "branch"
	// RefKindTag marks a ref resolved to refs/tags/<name>.
	RefKindTag = "tag"
	// RefKindCommit marks a ref given directly as an object id.
	RefKindCommit = "commit"
)

// ErrMissing reports that no lockfile exists yet. Callers distinguish this from
// a malformed lockfile because an absent file is the normal state of a project
// with no imports, while a malformed one must fail loudly.
var ErrMissing = errors.New("skill lock file does not exist")

// ErrMalformed reports that a lockfile exists but cannot be trusted. Import
// operations preserve local content and fail the affected imports instead of
// inventing a merge base.
var ErrMalformed = errors.New("skill lock file is malformed")

// Entry is one imported skill's recorded upstream state.
type Entry struct {
	// Name is the validated skill name and the local directory name under
	// .agent-layer/imported-skills/.
	Name string `json:"name"`
	// Repository is the configured source repository.
	Repository string `json:"repository"`
	// Selector is the positive configuration selector that produced this entry.
	// Repository and selector together identify exactly one configured block.
	Selector string `json:"selector"`
	// SelectedPath is the repository-relative skill root path.
	SelectedPath string `json:"selected_path"`
	// ConfiguredRef is the block's configured ref, empty when the default branch
	// is requested. A change here is a retarget, not a removal plus addition.
	ConfiguredRef string `json:"configured_ref"`
	// ResolvedRef is the actual ref resolved from the remote: a branch name, a
	// tag name, or the commit id when the configured ref was an object id.
	ResolvedRef string `json:"resolved_ref"`
	// RefKind is remote-resolved evidence of what ResolvedRef names.
	RefKind string `json:"ref_kind"`
	// Tracking is the resolved tracking mode, RefKindBranch sources may track.
	Tracking string `json:"tracking"`
	// Commit is the resolved source commit that TreeHash was taken from.
	Commit string `json:"commit"`
	// TreeHash is the canonical hash of the upstream skill tree at Commit. It is
	// the immutable merge base; local edits never replace it.
	TreeHash string `json:"tree_hash"`
}

// File is the complete on-disk lock document.
type File struct {
	Version int     `json:"version"`
	Skills  []Entry `json:"skills"`
}

// New returns an empty lock document at the current schema version.
func New() *File {
	return &File{Version: Version, Skills: nil}
}

// Load reads and strictly validates the lockfile at path.
//
// A missing file returns ErrMissing so callers can distinguish "no imports yet"
// from corruption. Any structural problem returns an error wrapping
// ErrMalformed.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the resolved repository .agent-layer/skills.lock.json, not user input.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissing, path)
		}
		return nil, fmt.Errorf("failed to read skill lock %s: %w", path, err)
	}
	return Parse(data, path)
}

// Parse decodes lock data, rejecting unknown fields, unknown schema versions,
// and entries that are missing required identity or merge-base state. source is
// used for error context.
func Parse(data []byte, source string) (*File, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var file File
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMalformed, source, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("%w: %s: unexpected trailing content", ErrMalformed, source)
	}
	if file.Version != Version {
		return nil, fmt.Errorf("%w: %s: unsupported schema version %d (this Agent Layer supports %d)", ErrMalformed, source, file.Version, Version)
	}
	if err := validate(&file, source); err != nil {
		return nil, err
	}
	file.Sort()
	return &file, nil
}

// validate enforces every persisted invariant downstream code relies on.
//
// The lockfile is machine state a user can edit or a crash can corrupt, and its
// values are fed straight into filesystem paths, Git commands, and merge-base
// comparisons. Anything that would make one of those unsafe or meaningless is
// malformed state, so it fails loudly here instead of being trusted later.
func validate(file *File, source string) error {
	names := make(map[string]int, len(file.Skills))
	pathsByRepository := make(map[string][]string, len(file.Skills))
	for i, entry := range file.Skills {
		if err := validateEntry(entry); err != nil {
			return fmt.Errorf("%w: %s: skills[%d]: %w", ErrMalformed, source, i, err)
		}
		key := skilltree.NormalizeName(entry.Name)
		if first, duplicate := names[key]; duplicate {
			return fmt.Errorf("%w: %s: skills[%d]: skill name %q is already recorded by skills[%d]", ErrMalformed, source, i, entry.Name, first)
		}
		names[key] = i
		pathsByRepository[entry.Repository] = append(pathsByRepository[entry.Repository], entry.SelectedPath)
	}
	for repository, paths := range pathsByRepository {
		if err := rejectOverlappingPaths(paths); err != nil {
			return fmt.Errorf("%w: %s: repository %s: %w", ErrMalformed, source, repository, err)
		}
	}
	return nil
}

// rejectOverlappingPaths refuses duplicate or ancestor/descendant selected
// paths in one repository, because they describe overlapping editable owners
// that no import operation can reconcile.
//
// Comparing sorted neighbours is not enough: every byte below '/' sorts ahead
// of it, so a sibling such as "skills-old" lands between "skills" and
// "skills/alpha" and would hide that pair. Each path is instead checked against
// every path already accepted, by walking its own ancestor prefixes. Sorting
// guarantees an ancestor is accepted before any of its descendants, because an
// ancestor is a strict prefix and therefore sorts first.
func rejectOverlappingPaths(paths []string) error {
	sorted := append([]string{}, paths...)
	sort.Strings(sorted)
	accepted := make(map[string]struct{}, len(sorted))
	for _, current := range sorted {
		if conflict, overlaps := findOverlap(accepted, current); overlaps {
			return fmt.Errorf("selected paths %s and %s overlap", conflict, current)
		}
		accepted[current] = struct{}{}
	}
	return nil
}

// findOverlap returns an already-accepted path that is candidate itself or one
// of its ancestors.
func findOverlap(accepted map[string]struct{}, candidate string) (string, bool) {
	for current := candidate; current != "." && current != "/" && current != ""; current = path.Dir(current) {
		if _, exists := accepted[current]; exists {
			return current, true
		}
	}
	return "", false
}

// ValidateRepository rejects a repository reference that embeds a literal
// credential, while accepting one that references a secret by placeholder.
//
// A repository URL is written into config.toml, copied into this lockfile, and
// printed in status output and Git command errors. A literal secret would be
// published to all three, so it is refused. A `${AL_*}` placeholder is not a
// secret: the placeholder text is what stays canonical everywhere, and the
// value it names is resolved only at the Git access boundary. That mirrors how
// MCP server URLs and headers reference secrets.
//
// A literal credential can reach a URL three ways, and all three are refused: a
// password component, userinfo on any scheme that is not a known identity-only
// transport, and a value under a secret-like query key. Only the scp-like
// `user@host:path` form and an `ssh://user@host/path` or `git://user@host/path`
// username are ordinary identifiers rather than secrets, so only those stay
// accepted literally.
func ValidateRepository(repository string) error {
	// The AL_ namespace is the only one `.agent-layer/.env` exposes, so any
	// other placeholder can never resolve and is reported now rather than as a
	// missing value at the first Git access.
	for _, name := range envref.Names(repository) {
		if !envref.IsAgentLayerName(name) {
			return fmt.Errorf("placeholder ${%s} is outside the %s namespace that .agent-layer/.env provides", name, envref.AgentLayerPrefix)
		}
	}

	// A credential can ride in the query string as well as in userinfo. The key
	// vocabulary is the one the MCP policy warning already uses, so both places
	// agree on what counts as a secret parameter.
	if key, found := envref.LiteralSecretQueryKey(repository); found {
		return fmt.Errorf("repository URL embeds a literal secret in its %q query parameter; reference it as ${%sNAME} from .agent-layer/.env, or let a Git credential helper or SSH supply the credential",
			key, envref.AgentLayerPrefix)
	}

	scheme, rest, ok := strings.Cut(repository, "://")
	if !ok {
		return nil
	}
	authority, _, _ := strings.Cut(rest, "/")
	// Userinfo ends at the last "@" in the authority, so a literal "@" inside a
	// password is not mistaken for the host separator.
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return nil
	}
	userinfo := authority[:at]
	username, password, hasPassword := strings.Cut(userinfo, ":")

	// The rejected value is never echoed back: repeating it in the error would
	// reproduce the exposure this rule exists to prevent.
	switch {
	case hasPassword && !envref.IsEntirelyPlaceholders(password):
		return fmt.Errorf("repository URL embeds a literal password; reference it as ${%sNAME} from .agent-layer/.env, or let a Git credential helper or SSH supply it", envref.AgentLayerPrefix)
	case !hasPassword && !allowsLiteralUsername(scheme) && !envref.IsEntirelyPlaceholders(username):
		return fmt.Errorf("repository URL embeds literal credentials in its userinfo; reference them as ${%sNAME} from .agent-layer/.env, or let a Git credential helper or SSH supply them", envref.AgentLayerPrefix)
	}
	return nil
}

// identityOnlySchemes are the transports whose userinfo names an account rather
// than carrying a credential, so a literal username stays acceptable.
var identityOnlySchemes = map[string]struct{}{
	"ssh": {},
	"git": {},
}

// allowsLiteralUsername reports whether a bare literal username is an identity
// on this scheme rather than a token.
//
// Only the named transports qualify. Every other scheme — including one built
// from a placeholder, whose resolved value is unknowable here — is treated as
// possibly web, because `${AL_SCHEME}://token@host` would otherwise hand git
// `https://token@host` while passing validation.
func allowsLiteralUsername(scheme string) bool {
	_, ok := identityOnlySchemes[strings.ToLower(strings.TrimSpace(scheme))]
	return ok
}

func validateEntry(entry Entry) error {
	required := []struct {
		field string
		value string
	}{
		{"name", entry.Name},
		{"repository", entry.Repository},
		{"selector", entry.Selector},
		{"selected_path", entry.SelectedPath},
		{"resolved_ref", entry.ResolvedRef},
		{"ref_kind", entry.RefKind},
		{"tracking", entry.Tracking},
		{"commit", entry.Commit},
		{"tree_hash", entry.TreeHash},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.field)
		}
	}

	if err := validateSkillName(entry.Name); err != nil {
		return err
	}
	if entry.Repository != strings.TrimSpace(entry.Repository) || strings.HasSuffix(entry.Repository, "/") {
		return fmt.Errorf("repository %q is not normalized", entry.Repository)
	}
	if err := ValidateRepository(entry.Repository); err != nil {
		return fmt.Errorf("repository is invalid: %w", err)
	}
	if strings.HasPrefix(strings.TrimSpace(entry.Selector), "!") {
		return fmt.Errorf("selector %q is an exclusion; only a positive selector can own a lock entry", entry.Selector)
	}
	if err := skilltree.ValidateRelativePath(entry.Selector); err != nil {
		return fmt.Errorf("selector %q is unsafe: %w", entry.Selector, err)
	}
	if err := skilltree.ValidateRelativePath(entry.SelectedPath); err != nil {
		return fmt.Errorf("selected_path %q is unsafe: %w", entry.SelectedPath, err)
	}
	if strings.ContainsAny(entry.SelectedPath, "*?[") {
		return fmt.Errorf("selected_path %q is a pattern, not a resolved skill root", entry.SelectedPath)
	}
	if skilltree.NormalizeName(path.Base(entry.SelectedPath)) != skilltree.NormalizeName(entry.Name) {
		return fmt.Errorf("selected_path %q does not end in the skill name %q", entry.SelectedPath, entry.Name)
	}

	switch entry.RefKind {
	case RefKindBranch, RefKindTag, RefKindCommit:
	default:
		return fmt.Errorf("ref_kind %q is not one of branch, tag, commit", entry.RefKind)
	}
	switch entry.Tracking {
	case TrackingTracked, TrackingPinned:
	default:
		return fmt.Errorf("tracking %q is not one of tracked, pinned", entry.Tracking)
	}
	if entry.Tracking == TrackingTracked && entry.RefKind != RefKindBranch {
		// Only a branch can move, so a tracked tag or commit would exempt the
		// entry from the source-advance check while never advancing.
		return fmt.Errorf("tracking %q requires ref_kind %q, not %q", TrackingTracked, RefKindBranch, entry.RefKind)
	}
	if err := validateCommitID(entry.Commit); err != nil {
		return fmt.Errorf("commit %q is invalid: %w", entry.Commit, err)
	}
	if entry.RefKind == RefKindCommit && entry.ResolvedRef != entry.Commit {
		return fmt.Errorf("ref_kind %q requires resolved_ref to be the commit id, but it is %q", RefKindCommit, entry.ResolvedRef)
	}
	if err := validateTreeHash(entry.TreeHash); err != nil {
		return fmt.Errorf("tree_hash %q is invalid: %w", entry.TreeHash, err)
	}
	return nil
}

// validateSkillName rejects a name that could not be a local directory under
// the imported skill tier.
func validateSkillName(name string) error {
	if name != skilltree.NormalizeName(name) {
		return fmt.Errorf("name %q is not normalized", name)
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name %q is not a directory name", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("name %q must not start with a dot", name)
	}
	return nil
}

func validateCommitID(commit string) error {
	if _, ok := commitIDLengths[len(commit)]; !ok {
		return fmt.Errorf("a git object id is 40 or 64 hexadecimal characters")
	}
	return requireHex(commit)
}

func validateTreeHash(hash string) error {
	digest, ok := strings.CutPrefix(hash, treeHashPrefix)
	if !ok {
		return fmt.Errorf("a canonical skill tree hash starts with %q", treeHashPrefix)
	}
	if len(digest) != 64 {
		return fmt.Errorf("a sha256 digest is 64 hexadecimal characters")
	}
	return requireHex(digest)
}

func requireHex(value string) error {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return fmt.Errorf("%q is not lowercase hexadecimal", value)
		}
	}
	return nil
}

// Sort orders entries by skill name so the serialized document is stable.
func (f *File) Sort() {
	sort.Slice(f.Skills, func(i, j int) bool { return f.Skills[i].Name < f.Skills[j].Name })
}

// Entry returns the locked entry for a skill name.
func (f *File) Entry(name string) (Entry, bool) {
	for _, entry := range f.Skills {
		if entry.Name == name {
			return entry, true
		}
	}
	return Entry{}, false
}

// Names returns every locked skill name in sorted order.
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Skills))
	for _, entry := range f.Skills {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	return names
}

// Upsert inserts or replaces the entry for entry.Name and keeps the document
// sorted.
func (f *File) Upsert(entry Entry) {
	for i := range f.Skills {
		if f.Skills[i].Name == entry.Name {
			f.Skills[i] = entry
			return
		}
	}
	f.Skills = append(f.Skills, entry)
	f.Sort()
}

// Remove deletes the entry for name and reports whether one was present.
func (f *File) Remove(name string) bool {
	for i := range f.Skills {
		if f.Skills[i].Name == name {
			f.Skills = append(f.Skills[:i], f.Skills[i+1:]...)
			return true
		}
	}
	return false
}

// Clone returns a deep copy so an operation can build its next state without
// mutating the snapshot it was planned against.
func (f *File) Clone() *File {
	clone := &File{Version: f.Version}
	if len(f.Skills) > 0 {
		clone.Skills = make([]Entry, len(f.Skills))
		copy(clone.Skills, f.Skills)
	}
	return clone
}

// Marshal renders the deterministic serialized form written to disk.
//
// The same invariants Parse enforces are checked here, so a producer bug can
// never persist state that the next Load would reject as malformed.
func (f *File) Marshal() ([]byte, error) {
	f.Sort()
	document := *f
	document.Version = Version
	if document.Skills == nil {
		document.Skills = []Entry{}
	}
	if err := validate(&document, "skill lock"); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode skill lock: %w", err)
	}
	return append(data, '\n'), nil
}

// Save writes the lock document to path atomically.
func (f *File) Save(path string) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to write skill lock %s: %w", path, err)
	}
	return nil
}
