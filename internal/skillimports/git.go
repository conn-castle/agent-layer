// Package skillimports imports Agent Skills from Git repositories into the
// managed .agent-layer/imported-skills/ root, reconciles local edits against
// upstream, and contributes local changes back.
//
// Every remote operation runs through the installed `git` executable in a
// disposable temporary repository under .agent-layer/tmp, using the user's
// existing Git authentication. The consuming project's repository is never
// read as a Git repository, staged, or committed.
package skillimports

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/conn-castle/agent-layer/internal/gitenv"
)

// GitRunner executes one git invocation. It is an interface so tests can inject
// deterministic fetch, authentication, and push failures without a network.
type GitRunner interface {
	// Run executes git with args in dir and returns stdout. dir may be empty for
	// invocations that need no repository. The returned error carries redacted,
	// actionable stderr.
	Run(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// GitError reports a failed git invocation with redacted output.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
	// ExitCode is git's exit status, or -1 when the process did not run to
	// completion. Callers that treat a specific status as data rather than
	// failure (git merge-file reports its conflict count this way) read it here.
	ExitCode int
}

// Error renders the failed git command and its redacted stderr.
func (e *GitError) Error() string {
	command := "git " + strings.Join(e.Args, " ")
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return fmt.Sprintf("%s: %v", command, e.Err)
	}
	return fmt.Sprintf("%s: %v: %s", command, e.Err, stderr)
}

// Unwrap exposes the underlying exec error.
func (e *GitError) Unwrap() error { return e.Err }

// ExecGitRunner runs the installed git executable.
type ExecGitRunner struct{}

// Run executes git with args in dir. The environment is the process environment
// with git's repository-discovery variables removed, so a hook-inherited GIT_DIR
// can never redirect an invocation at the consuming repository. Credential
// helpers, transport settings, and insteadOf rules are deliberately preserved:
// they are the user's existing Git authentication.
func (ExecGitRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are built from validated configuration and internal constants, never from a shell string.
	cmd.Dir = dir
	cmd.Env = gitenv.WithoutDiscovery()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return stdout.Bytes(), &GitError{
			Args:     redactArgs(args),
			Stderr:   RedactSecrets(stderr.String()),
			Err:      err,
			ExitCode: exitCode,
		}
	}
	return stdout.Bytes(), nil
}

// credentialPattern matches userinfo embedded in a URL. Agent Layer rejects
// configured repositories that carry credentials, but a credential helper,
// insteadOf rule, or submodule URL can still put one into git's output.
var credentialPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^/@\s]*:[^/@\s]*@`)

// bearerPattern matches an Authorization header value echoed by a transport helper.
var bearerPattern = regexp.MustCompile(`(?i)(authorization\s*:\s*\w+\s+)\S+`)

// RedactSecrets removes credentials from text that is about to be shown to the
// user or embedded in an error. Host and path stay visible so the message is
// still actionable.
func RedactSecrets(text string) string {
	redacted := credentialPattern.ReplaceAllString(text, "${1}<redacted>@")
	return bearerPattern.ReplaceAllString(redacted, "${1}<redacted>")
}

// redactArgs applies RedactSecrets to each argument so a repository URL with an
// embedded credential never reaches an error message verbatim.
func redactArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, RedactSecrets(arg))
	}
	return out
}

// workspace is a disposable git repository used for one command's remote work.
// It lives under .agent-layer/tmp so it shares a filesystem with the managed
// skill root and is never mistaken for project content.
type workspace struct {
	dir    string
	runner GitRunner
}

// workspacesDirName is the parent for every disposable import repository.
const workspacesDirName = "skill-imports"

// newWorkspace creates and initializes a disposable git repository under
// root/.agent-layer/tmp/skill-imports. The caller must call close.
func newWorkspace(ctx context.Context, runner GitRunner, root string, label string) (*workspace, error) {
	parent := filepath.Join(root, ".agent-layer", "tmp", workspacesDirName)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, fmt.Errorf("create git workspace parent %s: %w", parent, err)
	}
	dir, err := os.MkdirTemp(parent, sanitizeLabel(label)+"-")
	if err != nil {
		return nil, fmt.Errorf("create git workspace under %s: %w", parent, err)
	}
	space := &workspace{dir: dir, runner: runner}
	if _, err := runner.Run(ctx, dir, "init", "--quiet"); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("initialize git workspace: %w", err)
	}
	return space, nil
}

// close removes the disposable repository. A cleanup failure is returned rather
// than ignored so a full disk or permission problem stays visible.
func (w *workspace) close() error {
	if w == nil || w.dir == "" {
		return nil
	}
	if err := os.RemoveAll(w.dir); err != nil {
		return fmt.Errorf("remove git workspace %s: %w", w.dir, err)
	}
	return nil
}

// run executes git inside the workspace.
func (w *workspace) run(ctx context.Context, args ...string) ([]byte, error) {
	return w.runner.Run(ctx, w.dir, args...)
}

// sanitizeLabel reduces a caller-supplied label to characters that are always
// safe in a directory name.
func sanitizeLabel(label string) string {
	var builder strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	trimmed := strings.Trim(builder.String(), "-")
	if trimmed == "" {
		return "workspace"
	}
	return trimmed
}
