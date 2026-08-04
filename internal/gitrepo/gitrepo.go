// Package gitrepo runs the installed `git` executable on behalf of skill
// imports.
//
// Every invocation uses an argument array (never a shell command string),
// disables interactive prompting, and works inside a caller-owned temporary
// directory so an import can never mutate the consuming project's repository.
// The user's existing Git authentication and configuration remain
// authoritative: Agent Layer neither reads nor stores credentials.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// commitIDPattern matches a full 40-character SHA-1 object id. Abbreviated ids
// are not accepted as configured refs because they are ambiguous.
var commitIDPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ErrGitUnavailable reports that no usable `git` executable is on PATH.
var ErrGitUnavailable = errors.New("git executable not found on PATH")

// Runner invokes git with a fixed non-interactive environment.
type Runner struct {
	path string
	env  []string
}

// NewRunner locates git and returns a runner bound to it.
func NewRunner() (*Runner, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}
	return &Runner{path: path, env: nonInteractiveEnv(os.Environ())}, nil
}

// nonInteractiveEnv disables every interactive credential and SSH prompt so a
// missing credential fails fast with an actionable error instead of blocking on
// a terminal that an automated run does not have.
func nonInteractiveEnv(base []string) []string {
	filtered := make([]string, 0, len(base)+4)
	for _, entry := range base {
		switch {
		case strings.HasPrefix(entry, "GIT_TERMINAL_PROMPT="),
			strings.HasPrefix(entry, "GIT_ASKPASS="),
			strings.HasPrefix(entry, "SSH_ASKPASS="),
			strings.HasPrefix(entry, "GIT_OPTIONAL_LOCKS="):
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GIT_OPTIONAL_LOCKS=0",
	)
}

// CommandError carries the captured stderr of a failed git invocation so the
// caller can report an actionable message.
type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

// Error renders the failing git command with its captured diagnostics.
func (e *CommandError) Error() string {
	message := strings.TrimSpace(e.Stderr)
	if message == "" {
		message = e.Err.Error()
	}
	return fmt.Sprintf("git %s failed: %s", strings.Join(e.Args, " "), message)
}

// Unwrap exposes the underlying execution error.
func (e *CommandError) Unwrap() error { return e.Err }

// run executes git in dir and returns its standard output.
func (r *Runner) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	stdout, _, err := r.runAllowExit(ctx, dir, nil, args...)
	return stdout, err
}

// runAllowExit executes git and returns stdout, the process exit code, and an
// error only for failures that are not an ordinary non-zero exit. Callers that
// treat a specific exit code as meaningful (for example `git merge-file`
// reporting conflict counts) use this form.
func (r *Runner) runAllowExit(ctx context.Context, dir string, allowedExitCodes []int, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, r.path, args...) // #nosec G204 -- args are built from validated configuration and resolved object ids, never a shell string.
	cmd.Dir = dir
	cmd.Env = r.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		for _, allowed := range allowedExitCodes {
			if code == allowed {
				return stdout.Bytes(), code, nil
			}
		}
		return stdout.Bytes(), code, &CommandError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return nil, -1, &CommandError{Args: args, Stderr: stderr.String(), Err: err}
}

// IsCommitID reports whether value is a full object id rather than a symbolic
// ref name.
func IsCommitID(value string) bool {
	return commitIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}
