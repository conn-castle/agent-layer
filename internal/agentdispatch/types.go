package agentdispatch

import (
	"context"
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
	// AgentRandom is rejected by start: every conversation names its exact agent.
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

// runOptions carries one prepared invocation's target and override inputs
// between Start/Continue and the shared preparation helpers.
type runOptions struct {
	Root            string
	Model           string
	ReasoningEffort string
	Skill           string
	Prompt          string
	LookPath        func(string) (string, error)
	// VersionLookup reads the installed provider version. Production uses the
	// provider's --version output; tests may inject exact fixture evidence.
	VersionLookup func(path string, agent string) (string, error)
}

// StartOptions configures the first asynchronous invocation of a conversation.
type StartOptions struct {
	Root            string
	WorkDir         string
	Agent           string
	Model           string
	ReasoningEffort string
	Skill           string
	Prompt          string
	PromptFile      string
	Stdout          io.Writer
	Stderr          io.Writer
	Env             []string
	LookPath        func(string) (string, error)
	VersionLookup   func(path string, agent string) (string, error)
	launchWorker    workerLauncher
}

// ContinueOptions configures one asynchronous continuation of a conversation.
type ContinueOptions struct {
	Root          string
	WorkDir       string
	Handle        string
	Prompt        string
	PromptFile    string
	Stdout        io.Writer
	Stderr        io.Writer
	Env           []string
	LookPath      func(string) (string, error)
	VersionLookup func(path string, agent string) (string, error)
	launchWorker  workerLauncher
}

// WaitRequest identifies one existing dispatch conversation, by handle, to
// await without changing provider work or execution state.
type WaitRequest struct {
	Context context.Context
	Root    string
	ID      string
	Stdout  io.Writer
}

// CancelRequest identifies one active invocation by handle or run UUID.
type CancelRequest struct {
	Root   string
	ID     string
	Stdout io.Writer
}

// OptionsRequest configures an Agent Dispatch options response.
type OptionsRequest struct {
	Root          string
	Env           []string
	Stdout        io.Writer
	LookPath      func(string) (string, error)
	VersionLookup func(path string, agent string) (string, error)
}

// CommandFactory creates a command for a target adapter.
type CommandFactory func(name string, args ...string) *exec.Cmd
