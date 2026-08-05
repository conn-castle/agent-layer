// Package gitrepo runs the installed `git` executable on behalf of skill
// imports.
//
// Every invocation uses an argument array (never a shell command string),
// disables interactive prompting, and works inside a caller-owned temporary
// directory so an import can never mutate the consuming project's repository.
// The user's existing Git authentication and identity remain authoritative:
// Agent Layer neither reads nor stores credentials. Protocol, hook, template,
// and publication-signing settings are narrowed at the execution boundary.
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

// commitIDPattern matches a full object id in either width Git uses: 40
// characters for SHA-1 and 64 for SHA-256. Abbreviated ids are not accepted as
// configured refs because they are ambiguous. The widths match the ones
// skilllock accepts, so a configured id and a locked id are never classified
// differently.
var commitIDPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ErrGitUnavailable reports that no usable `git` executable is on PATH.
var ErrGitUnavailable = errors.New("git executable not found on PATH")

// Runner invokes git with a fixed non-interactive environment.
type Runner struct {
	path string
	env  []string
	// secrets resolves `${AL_*}` placeholders in repository references and
	// redacts the resolved values back out of anything reported to the user.
	// Repository URLs are ordinary command arguments, so every diagnostic this
	// runner produces passes through it.
	secrets *Secrets
}

// NewRunner locates git and returns a runner whose repository references
// resolve from env, an AL_-filtered `.agent-layer/.env` map.
func NewRunner(env map[string]string) (*Runner, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGitUnavailable, err)
	}
	return &Runner{path: path, env: nonInteractiveEnv(os.Environ()), secrets: NewSecrets(env)}, nil
}

// Secrets returns the runner's resolution and redaction boundary. Every
// repository reference a caller hands to this runner must be resolved through
// it, so the resolved value is known to the redactor.
func (r *Runner) Secrets() *Secrets { return r.secrets }

// repositorySelectionVars are the environment variables Git uses to choose a
// repository, work tree, index, or object store instead of discovering them
// from the working directory. Inheriting any of them would let an ambient value
// redirect an isolated import at the consuming project's repository, which is
// exactly the isolation this package promises, so they are dropped rather than
// overridden. Credential and identity configuration stays authoritative;
// GIT_CONFIG_NOSYSTEM and the credential environment are untouched here.
var repositorySelectionVars = []string{
	"GIT_DIR=",
	"GIT_WORK_TREE=",
	"GIT_INDEX_FILE=",
	"GIT_COMMON_DIR=",
	"GIT_OBJECT_DIRECTORY=",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=",
	"GIT_NAMESPACE=",
}

// gitPolicyVars are replaced rather than inherited. GIT_ALLOW_PROTOCOL sets
// Git's default protocol policy to never and enables only the listed
// transports; GIT_PROTOCOL_FROM_USER prevents nested Git operations from
// re-enabling a user-only transport through configuration such as insteadOf.
var gitPolicyVars = []string{
	"GIT_ALLOW_PROTOCOL=",
	"GIT_PROTOCOL_FROM_USER=",
}

// nonInteractiveEnv disables every interactive credential and SSH prompt so a
// missing credential fails fast with an actionable error instead of blocking on
// a terminal that an automated run does not have. It removes every inherited
// repository-selection variable and replaces inherited protocol policy so each
// command operates only on its caller-owned directory and approved transports.
func nonInteractiveEnv(base []string) []string {
	filtered := make([]string, 0, len(base)+6)
	for _, entry := range base {
		switch {
		case strings.HasPrefix(entry, "GIT_TERMINAL_PROMPT="),
			strings.HasPrefix(entry, "GIT_ASKPASS="),
			strings.HasPrefix(entry, "SSH_ASKPASS="),
			strings.HasPrefix(entry, "GIT_OPTIONAL_LOCKS="):
			continue
		}
		if hasAnyPrefix(entry, repositorySelectionVars) {
			continue
		}
		if hasAnyPrefix(entry, gitPolicyVars) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_ALLOW_PROTOCOL=https:ssh:git:file",
		"GIT_PROTOCOL_FROM_USER=0",
	)
}

// hasAnyPrefix reports whether value starts with any of the given prefixes.
func hasAnyPrefix(value string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// literalPathspec prevents repository paths containing Git metacharacters from
// selecting sibling content. Callers still pass `--`; this magic prefix makes
// literal interpretation explicit rather than depending on ambient config.
func literalPathspec(repoPath string) string { return ":(literal)" + repoPath }

// commitExists distinguishes an absent commit from a failure to inspect the
// object database. Batch-check reports a missing object in machine-readable
// stdout while reserving a non-zero exit for repository and object failures.
func commitExists(ctx context.Context, runner *Runner, dir string, commit string) (bool, error) {
	query := commit + "^{commit}\n"
	output, err := runner.runInput(ctx, dir, []byte(query), "cat-file", "--batch-check=%(objecttype)")
	if err != nil {
		return false, err
	}
	result := strings.TrimSpace(string(output))
	switch {
	case result == "commit":
		return true, nil
	case result == strings.TrimSpace(query)+" missing":
		return false, nil
	default:
		return false, fmt.Errorf("git cat-file returned an unexpected commit inspection result %q", result)
	}
}

// CommandError carries the captured stderr of a failed git invocation so the
// caller can report an actionable message.
//
// Its Args and Stderr are already redacted: a repository URL is an ordinary
// command argument, and git echoes it back in most of its own diagnostics, so
// both are passed through the runner's Secrets before being stored.
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
	stdout, _, err := r.runAllowExitInput(ctx, dir, nil, nil, args...)
	return stdout, err
}

// runInput executes git with input connected to standard input. Object
// publication uses it to hash exact blob bytes without materializing a remote
// tree or passing content through checkout filters.
func (r *Runner) runInput(ctx context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	stdout, _, err := r.runAllowExitInput(ctx, dir, input, nil, args...)
	return stdout, err
}

// runAllowExit executes git and returns stdout, the process exit code, and an
// error only for failures that are not an ordinary non-zero exit. Callers that
// treat a specific exit code as meaningful (for example `git merge-file`
// reporting conflict counts) use this form.
func (r *Runner) runAllowExit(ctx context.Context, dir string, allowedExitCodes []int, args ...string) ([]byte, int, error) {
	return r.runAllowExitInput(ctx, dir, nil, allowedExitCodes, args...)
}

// runAllowExitInput is the single process boundary for Git invocation. It
// applies fixed hook/template protections, optional standard input, redaction,
// and caller-declared non-zero result handling.
func (r *Runner) runAllowExitInput(ctx context.Context, dir string, input []byte, allowedExitCodes []int, args ...string) ([]byte, int, error) {
	// These protections apply to every invocation, including `git init`, so a
	// global template cannot seed hooks into a disposable repository and no Git
	// command can discover or run repository hooks.
	effectiveArgs := make([]string, 0, len(args)+4)
	effectiveArgs = append(effectiveArgs, "-c", "core.hooksPath="+os.DevNull, "-c", "init.templateDir=")
	effectiveArgs = append(effectiveArgs, args...)
	cmd := exec.CommandContext(ctx, r.path, effectiveArgs...) // #nosec G204 -- args are built from validated configuration and resolved object ids, never a shell string.
	cmd.Dir = dir
	cmd.Env = r.env
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
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
		return stdout.Bytes(), code, r.commandError(args, stderr.String(), err)
	}
	return nil, -1, r.commandError(args, stderr.String(), err)
}

// commandError builds a failure whose rendered form carries no resolved secret.
func (r *Runner) commandError(args []string, stderr string, err error) *CommandError {
	return &CommandError{
		Args:   r.secrets.redactAll(args),
		Stderr: r.secrets.Redact(stderr),
		Err:    err,
	}
}

// IsCommitID reports whether value is a full object id rather than a symbolic
// ref name.
func IsCommitID(value string) bool {
	return commitIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}
