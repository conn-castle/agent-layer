package gitrepo

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/conn-castle/agent-layer/internal/envref"
)

// Repository is one repository reference in both of the forms Agent Layer
// needs: the configured text, which stays canonical everywhere it is recorded
// or displayed, and the resolved value, which only a Git command ever sees.
//
// Its String method returns the display form, so a repository formatted into
// any message is safe by construction rather than by remembering to pick a
// field.
type Repository struct {
	display string
	git     string
}

// String returns the configured text, with any `${AL_*}` placeholder intact.
func (r Repository) String() string { return r.display }

// IsZero reports whether the repository was never resolved.
func (r Repository) IsZero() bool { return r.display == "" && r.git == "" }

// Secrets is the single boundary at which a configured repository reference
// becomes a value a Git command can use, and the single place that keeps the
// resolved value from coming back out.
//
// Placeholders resolve from the AL_-filtered `.agent-layer/.env` map. Every
// value substituted is remembered so it can be replaced with the placeholder
// that named it in any command arguments or Git diagnostics rendered to the
// user. Redaction is therefore driven by what was actually resolved, not by a
// guess about which strings look secret.
type Secrets struct {
	env map[string]string
	// mu guards resolved, which grows as blocks are opened.
	mu sync.Mutex
	// resolved maps each substituted value to the placeholder that produced it.
	resolved map[string]string
}

// NewSecrets returns a resolver over an AL_-filtered environment map.
func NewSecrets(env map[string]string) *Secrets {
	return &Secrets{env: env, resolved: map[string]string{}}
}

// Resolve turns a configured repository reference into a Repository.
//
// A reference with no placeholder resolves to itself. A referenced value that
// is missing or empty fails with an actionable message naming the variables and
// the file they belong in, rather than handing Git a half-substituted URL.
func (s *Secrets) Resolve(reference string) (Repository, error) {
	names := envref.Names(reference)
	if len(names) == 0 {
		return Repository{display: reference, git: reference}, nil
	}

	var missing []string
	seen := map[string]struct{}{}
	git := reference
	for _, name := range names {
		if _, done := seen[name]; done {
			continue
		}
		seen[name] = struct{}{}
		value, ok := s.env[name]
		if !ok || value == "" {
			missing = append(missing, name)
			continue
		}
		placeholder := "${" + name + "}"
		git = strings.ReplaceAll(git, placeholder, value)
		s.remember(value, placeholder)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Repository{}, fmt.Errorf("repository %s references %s, which .agent-layer/.env does not define; add %s there, or use a Git credential helper or SSH instead of an in-URL credential",
			reference, describeVariables(missing), pluralizeVariables(missing))
	}
	return Repository{display: reference, git: git}, nil
}

// remember records a substituted value so redact can replace it later.
func (s *Secrets) remember(value string, placeholder string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved[value] = placeholder
}

// Redact replaces every value this resolver substituted with the placeholder
// that named it, so a rendered Git argument or diagnostic shows
// `https://${AL_TOKEN}@host/repo.git` rather than the credential.
//
// Longer values are replaced first so a secret that contains another secret
// cannot be left partly exposed by an earlier substitution.
func (s *Secrets) Redact(text string) string {
	if s == nil || text == "" {
		return text
	}
	s.mu.Lock()
	values := make([]string, 0, len(s.resolved))
	for value := range s.resolved {
		values = append(values, value)
	}
	replacements := make(map[string]string, len(s.resolved))
	for value, placeholder := range s.resolved {
		replacements[value] = placeholder
	}
	s.mu.Unlock()

	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) != len(values[j]) {
			return len(values[i]) > len(values[j])
		}
		return values[i] < values[j]
	})
	for _, value := range values {
		text = strings.ReplaceAll(text, value, replacements[value])
	}
	return text
}

// redactAll applies Redact to each element of args.
func (s *Secrets) redactAll(args []string) []string {
	if s == nil || len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = s.Redact(arg)
	}
	return out
}

// describeVariables renders a missing-variable list for an error message.
func describeVariables(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, "${"+name+"}")
	}
	return strings.Join(quoted, ", ")
}

// pluralizeVariables names what the user has to add, matching the count.
func pluralizeVariables(names []string) string {
	if len(names) == 1 {
		return "that key"
	}
	return "those keys"
}
