package agentdispatch

import (
	"fmt"
	"io"
	"os/exec"
)

const (
	// AgentCodex is the Codex dispatch target and caller marker value.
	AgentCodex = "codex"
	// AgentClaude is the Claude dispatch target and caller marker value.
	AgentClaude = "claude"
	// AgentAntigravity is the Antigravity dispatch target and caller marker value.
	AgentAntigravity = "antigravity"
	// AgentRandom is the resolver value that selects an eligible target at random.
	AgentRandom = "random"
)

const (
	// ExitUsage is the stable dispatch usage/resolution failure exit code.
	ExitUsage = 64
	// ExitConfig is the stable dispatch configuration/state failure exit code.
	ExitConfig = 65
	// ExitUnavailable is the stable dispatch target-unavailable exit code.
	ExitUnavailable = 69
	// ExitTargetFailure is the stable dispatch target/adapter failure exit code.
	ExitTargetFailure = 70
	// ExitNested is the stable dispatch nested-call failure exit code.
	ExitNested = 75
	// ExitSigint is the stable dispatch SIGINT exit code.
	ExitSigint = 130
	// ExitSigterm is the stable dispatch SIGTERM exit code.
	ExitSigterm = 143
)

// ExitError carries a dispatch-owned exit category and the message already
// written or intended for stderr by the CLI wrapper.
type ExitError struct {
	Code    int
	Message string
	Err     error
}

// Error returns the user-facing dispatch error text.
func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("dispatch exit %d", e.Code)
}

// Unwrap returns the wrapped lower-level error.
func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func exitError(code int, message string) *ExitError {
	return &ExitError{Code: code, Message: message}
}

func wrapExitError(code int, message string, err error) *ExitError {
	return &ExitError{Code: code, Message: message, Err: err}
}

// RunOptions configures a single dispatch run.
type RunOptions struct {
	Root            string
	Agent           string
	Model           string
	ReasoningEffort string
	Skill           string
	PromptArgs      []string
	Stdin           io.Reader
	ReadStdin       bool
	Stdout          io.Writer
	Stderr          io.Writer
	Env             []string
	Quiet           bool
	LookPath        func(string) (string, error)
	NewCommand      CommandFactory
	ChooseRandom    RandomChooser
}

// OptionsRequest configures an Agent Dispatch options response.
type OptionsRequest struct {
	Root     string
	Env      []string
	Stdout   io.Writer
	JSON     bool
	LookPath func(string) (string, error)
}

// CommandFactory creates a command for a target adapter.
type CommandFactory func(name string, args ...string) *exec.Cmd

// RandomChooser selects one target from an already-built random pool.
type RandomChooser func([]string) (string, error)
