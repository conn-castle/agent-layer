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
	WorkDir         string
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
	// VersionLookup reads the installed provider version. Production uses the
	// provider's --version output; tests may inject exact fixture evidence.
	VersionLookup func(path string, agent string) (string, error)
}

// ResumeOptions configures one explicit continuation of a durable session.
type ResumeOptions struct {
	Root          string
	WorkDir       string
	Name          string
	Skill         string
	PromptArgs    []string
	Stdin         io.Reader
	ReadStdin     bool
	Stdout        io.Writer
	Stderr        io.Writer
	Env           []string
	Quiet         bool
	LookPath      func(string) (string, error)
	NewCommand    CommandFactory
	VersionLookup func(path string, agent string) (string, error)
}

// InspectionRequest configures factual, read-only dispatch inspection.
type InspectionRequest struct {
	Root   string
	ID     string
	Stdout io.Writer
	JSON   bool
}

// ListRequest configures durable mapping listing.
type ListRequest struct {
	Root   string
	Stdout io.Writer
	JSON   bool
}

// HistoryRequest configures immutable turn-history output.
type HistoryRequest struct {
	Root   string
	Name   string
	Stdout io.Writer
	Stderr io.Writer
	JSON   bool
}

// CancelRequest identifies one active run, friendly conversation, or fanout.
type CancelRequest struct {
	Root string
	ID   string
}

// FanoutTarget is one self-contained provider target specification.
type FanoutTarget struct {
	Agent           string `json:"agent"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// FanoutOptions configures one synchronous shared-prompt fanout.
type FanoutOptions struct {
	Root          string
	WorkDir       string
	Targets       []FanoutTarget
	Skill         string
	PromptArgs    []string
	Stdin         io.Reader
	ReadStdin     bool
	Stdout        io.Writer
	Stderr        io.Writer
	Env           []string
	Quiet         bool
	LookPath      func(string) (string, error)
	NewCommand    CommandFactory
	VersionLookup func(path string, agent string) (string, error)
}

// OptionsRequest configures an Agent Dispatch options response.
type OptionsRequest struct {
	Root          string
	Env           []string
	Stdout        io.Writer
	JSON          bool
	LookPath      func(string) (string, error)
	VersionLookup func(path string, agent string) (string, error)
}

// CommandFactory creates a command for a target adapter.
type CommandFactory func(name string, args ...string) *exec.Cmd

// RandomChooser selects one target from an already-built random pool.
type RandomChooser func([]string) (string, error)
