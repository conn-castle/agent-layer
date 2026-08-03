// Package gitenv builds environments for invoking git as a subprocess.
//
// Git exports its repository-discovery variables to every hook it runs, so any
// process a hook starts inherits them — including `go test ./...` run by a
// pre-commit hook. Those variables take precedence over a `-C <dir>` argument,
// so an inheriting subprocess operates on the hook's repository rather than the
// directory it was pointed at. For a test that runs `git init` in a temporary
// fixture, that means re-initializing the developer's own checkout.
package gitenv

import (
	"os"
	"slices"
	"strings"
)

// discoveryVariables are the environment variables that override how git decides
// which repository it is operating on. Credentials, transport settings, and
// configuration overrides are deliberately absent: they do not redirect git to a
// different repository, and dropping them would change behavior the caller asked
// for.
var discoveryVariables = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_CEILING_DIRECTORIES",
	"GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_PREFIX",
}

// WithoutDiscovery returns the current process environment with every
// repository-discovery variable removed, so git resolves the repository from the
// directory it is given. Assign it to exec.Cmd.Env whenever a git subprocess is
// pointed at a specific directory.
func WithoutDiscovery() []string {
	return withoutDiscovery(os.Environ())
}

// withoutDiscovery filters an explicit environment, so the behavior is testable
// without mutating the process.
func withoutDiscovery(environ []string) []string {
	cleaned := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, found := strings.Cut(entry, "=")
		if found && slices.Contains(discoveryVariables, name) {
			continue
		}
		cleaned = append(cleaned, entry)
	}
	return cleaned
}
