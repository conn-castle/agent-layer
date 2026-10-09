package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// MCP tool names. They are the canonical agent-facing Agent Dispatch surface
// and are deliberately terse: every enabled caller carries all seven schemas in
// its always-present context.
const (
	ToolOptions  = "dispatch_options"
	ToolStart    = "dispatch_start"
	ToolWait     = "dispatch_wait"
	ToolContinue = "dispatch_continue"
	ToolCancel   = "dispatch_cancel"
	ToolInspect  = "dispatch_inspect"
	ToolOutput   = "dispatch_output"
)

// mcpServerName identifies this server to MCP clients.
const mcpServerName = "agent-layer"

// mcpProgressInterval is how often a long wait relays a progress notification
// to clients that supplied a progress token.
const mcpProgressInterval = 30 * time.Second

// MCPServerOptions configures one Agent Dispatch MCP stdio server.
type MCPServerOptions struct {
	// Root is the Agent Layer configuration root.
	Root string
	// WorkDir is the caller's working directory, which differs from Root when a
	// linked worktree uses an ancestor checkout's configuration.
	WorkDir string
	// Version is reported to clients during initialization.
	Version string
	// Env is the process environment used for dispatch depth and benchmark
	// policy discovery. Nil reads os.Environ.
	Env []string
	// Set only by the process owner, which joins cleanup before server exit.
	discoveryCleanup *agentoptions.DiscoveryCleanup
}

// dispatchToolServer holds the resolved per-process configuration shared by
// every tool handler.
type dispatchToolServer struct {
	root             string
	workDir          string
	env              []string
	waitTimeout      time.Duration
	toolTimeout      time.Duration
	progressInterval time.Duration
	policy           benchmarkPolicy
	discoveryOnce    sync.Once
	discoveryGate    chan struct{}
	discoveryCleanup *agentoptions.DiscoveryCleanup
}

// OptionsInput is the (empty) input of dispatch_options.
type OptionsInput struct{}

// StartInput mirrors the CLI's `al dispatch start` selection flags. Exactly one
// prompt source is required, matching the CLI contract.
type StartInput struct {
	Agent           string `json:"agent"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Role            string `json:"role,omitempty"`
	Skill           string `json:"skill,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	PromptFile      string `json:"prompt_file,omitempty"`
}

// ContinueInput mirrors the CLI's `al dispatch continue` inputs.
type ContinueInput struct {
	Handle     string `json:"handle"`
	Prompt     string `json:"prompt,omitempty"`
	PromptFile string `json:"prompt_file,omitempty"`
}

// HandleInput identifies one conversation by its opaque handle.
type HandleInput struct {
	Handle string `json:"handle"`
}

// SelectorInput identifies one invocation by exactly one of handle or invocation_id.
type SelectorInput struct {
	Handle       string `json:"handle,omitempty"`
	InvocationID string `json:"invocation_id,omitempty"`
}

// WaitInput waits for one invocation. Condition applies only to wait.
type WaitInput struct {
	Handle       string `json:"handle,omitempty"`
	InvocationID string `json:"invocation_id,omitempty"`
	Condition    string `json:"condition,omitempty"`
}

// OutputInput retrieves bounded artifact content for one invocation.
type OutputInput struct {
	Handle       string `json:"handle,omitempty"`
	InvocationID string `json:"invocation_id,omitempty"`
	Artifact     string `json:"artifact"`
}

// RunMCPServer serves the Agent Dispatch tools over stdio until the client
// disconnects or ctx is cancelled. Nothing but the SDK ever writes to stdout:
// every dispatch operation renders into a private buffer.
func RunMCPServer(ctx context.Context, opts MCPServerOptions) error {
	stop := suppressMCPSIGPIPE()
	defer stop()
	return runMCPServer(ctx, opts, os.Stdin, os.Stdout, os.Stderr)
}

func newDispatchMCPServer(opts MCPServerOptions) (*mcp.Server, error) {
	tools, err := newDispatchToolServer(opts)
	if err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: opts.Version}, nil)
	if err := tools.register(server); err != nil {
		return nil, err
	}
	return server, nil
}

// newDispatchToolServer resolves the configuration every tool handler shares.
// It fails before any tool is served when the project config, benchmark policy,
// or root cannot be resolved.
func newDispatchToolServer(opts MCPServerOptions) (*dispatchToolServer, error) {
	root := strings.TrimSpace(opts.Root)
	if root == "" {
		return nil, exitError(ExitConfig, "repository root is required")
	}
	env := opts.Env
	if env == nil {
		env = os.Environ()
	}
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, exitError(ExitConfig, err.Error())
	}
	policy, err := loadBenchmarkPolicy(env)
	if err != nil {
		return nil, err
	}
	tools := &dispatchToolServer{
		root:             root,
		workDir:          strings.TrimSpace(opts.WorkDir),
		env:              env,
		waitTimeout:      config.DispatchMCPWaitTimeout(project.Config),
		toolTimeout:      config.DispatchMCPToolTimeout(project.Config),
		progressInterval: mcpProgressInterval,
		policy:           policy,
		discoveryCleanup: opts.discoveryCleanup,
	}
	if tools.workDir == "" {
		tools.workDir = root
	}
	return tools, nil
}

func (s *dispatchToolServer) register(server *mcp.Server) error {
	catalog, err := loadMCPToolDescriptions()
	if err != nil {
		return err
	}
	wait := catalog.Tools[ToolWait]
	wait.Description = renderMCPToolDescription(wait.Description, int(s.waitTimeout/time.Minute))
	if err := addDispatchTool(server, s.toolTimeout, ToolOptions, catalog.Tools[ToolOptions], readOnlyToolAnnotations(), s.handleOptions); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolStart, catalog.Tools[ToolStart], destructiveToolAnnotations(), s.handleStart); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolWait, wait, readOnlyToolAnnotations(), s.handleWait); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolContinue, catalog.Tools[ToolContinue], destructiveToolAnnotations(), s.handleContinue); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolCancel, catalog.Tools[ToolCancel], destructiveToolAnnotations(), s.handleCancel); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolInspect, catalog.Tools[ToolInspect], readOnlyToolAnnotations(), s.handleInspect); err != nil {
		return err
	}
	if err := addDispatchTool(server, s.toolTimeout, ToolOutput, catalog.Tools[ToolOutput], readOnlyToolAnnotations(), s.handleOutput); err != nil {
		return err
	}
	return nil
}

func addDispatchTool[In, Out any](server *mcp.Server, timeout time.Duration, name string, tool mcpToolDescription, annotations *mcp.ToolAnnotations, handler mcp.ToolHandlerFor[In, Out]) error {
	schema, err := mcpInputSchema[In](name, tool)
	if err != nil {
		return err
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Description: tool.Description,
		InputSchema: schema,
		Annotations: annotations,
	}, withoutPublishedOutputSchema(guard(timeout, handler)))
	return nil
}

func readOnlyToolAnnotations() *mcp.ToolAnnotations {
	openWorld := true
	return &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &openWorld}
}

func destructiveToolAnnotations() *mcp.ToolAnnotations {
	destructive, openWorld := true, true
	return &mcp.ToolAnnotations{DestructiveHint: &destructive, OpenWorldHint: &openWorld}
}

// withoutPublishedOutputSchema preserves typed handlers and structured results
// while using the SDK's `any` output escape hatch to omit the optional schema
// from every caller's permanently loaded tool definitions.
func withoutPublishedOutputSchema[In, Out any](handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, any, error) {
		result, output, err := handler(ctx, request, input)
		if err != nil {
			return result, nil, err
		}
		return result, output, nil
	}
}

// guard applies the configured hard timeout to one handler. Every client gets
// this bound, including clients with no per-server tool timeout of their own,
// so a wedged handler always releases the caller.
func guard[In, Out any](timeout time.Duration, handler mcp.ToolHandlerFor[In, Out]) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, request *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		type response struct {
			result *mcp.CallToolResult
			output Out
			err    error
		}
		completed := make(chan response, 1)
		go func() {
			if err := ctx.Err(); err != nil {
				completed <- response{err: err}
				return
			}
			result, output, err := handler(ctx, request, input)
			completed <- response{result: result, output: output, err: err}
		}()
		select {
		case response := <-completed:
			return response.result, response.output, response.err
		case <-ctx.Done():
			var zero Out
			return nil, zero, ctx.Err()
		}
	}
}

func (s *dispatchToolServer) handleOptions(ctx context.Context, _ *mcp.CallToolRequest, _ OptionsInput) (*mcp.CallToolResult, *OptionsResponse, error) {
	release, err := s.acquireDiscovery(ctx)
	if err != nil {
		return nil, nil, toolError(err)
	}
	defer release()
	options, err := BuildOptions(OptionsRequest{Context: ctx, Root: s.root, Env: s.env, Cleanup: s.discoveryCleanup})
	if err != nil {
		return nil, nil, toolError(err)
	}
	s.policy.constrainOptions(options)
	return nil, options, nil
}

func (s *dispatchToolServer) handleStart(ctx context.Context, _ *mcp.CallToolRequest, input StartInput) (*mcp.CallToolResult, *Result, error) {
	agent := strings.TrimSpace(input.Agent)
	model := strings.TrimSpace(input.Model)
	effort := strings.TrimSpace(input.ReasoningEffort)
	if err := s.policy.constrainStart(&agent, &model, &effort); err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	err := Start(StartOptions{
		Context: ctx, Root: s.root, WorkDir: s.workDir, Agent: agent, Model: model,
		ReasoningEffort: effort, Role: strings.TrimSpace(input.Role), Skill: strings.TrimSpace(input.Skill),
		Prompt: input.Prompt, PromptFile: strings.TrimSpace(input.PromptFile),
		Stdout: &out, Stderr: io.Discard, Env: s.env,
		beforeProvider: s.acquireProvider,
	})
	return decodeResult(&out, err)
}

func (s *dispatchToolServer) handleWait(ctx context.Context, request *mcp.CallToolRequest, input WaitInput) (*mcp.CallToolResult, *Result, error) {
	stop := relayProgress(ctx, request, s.progressInterval)
	defer stop()
	var out bytes.Buffer
	err := Wait(WaitRequest{
		Context: ctx, Root: s.root, Handle: strings.TrimSpace(input.Handle),
		InvocationID: strings.TrimSpace(input.InvocationID), Condition: strings.TrimSpace(input.Condition),
		Stdout: &out, Timeout: s.waitTimeout, PollInterval: mcpWaitPollInterval,
	})
	return decodeResult(&out, err)
}

func (s *dispatchToolServer) handleContinue(ctx context.Context, _ *mcp.CallToolRequest, input ContinueInput) (*mcp.CallToolResult, *Result, error) {
	var out bytes.Buffer
	err := Continue(ContinueOptions{
		Context: ctx, Root: s.root, WorkDir: s.workDir, Handle: strings.TrimSpace(input.Handle),
		Prompt: input.Prompt, PromptFile: strings.TrimSpace(input.PromptFile),
		Stdout: &out, Stderr: io.Discard, Env: s.env,
		beforeProvider: s.acquireProvider,
	})
	return decodeResult(&out, err)
}

func (s *dispatchToolServer) acquireProvider(ctx context.Context, agent string) (func(), error) {
	if agent != AgentClaude {
		return func() {}, ctx.Err()
	}
	return s.acquireDiscovery(ctx)
}

// Hold the gate through discovery or Claude launch, so another probe cannot
// register between waiting for credential persistence and starting Claude.
func (s *dispatchToolServer) acquireDiscovery(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.discoveryOnce.Do(func() { s.discoveryGate = make(chan struct{}, 1) })
	select {
	case s.discoveryGate <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for Claude model discovery shutdown: %w", ctx.Err())
	}
	release := func() { <-s.discoveryGate }
	if s.discoveryCleanup != nil {
		if err := s.discoveryCleanup.Wait(ctx); err != nil {
			release()
			return nil, fmt.Errorf("wait for Claude model discovery shutdown: %w", err)
		}
	} else if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (s *dispatchToolServer) handleCancel(_ context.Context, _ *mcp.CallToolRequest, input SelectorInput) (*mcp.CallToolResult, *Result, error) {
	var out bytes.Buffer
	err := Cancel(CancelRequest{
		Root: s.root, Handle: strings.TrimSpace(input.Handle),
		InvocationID: strings.TrimSpace(input.InvocationID), Stdout: &out,
	})
	return decodeCancelResult(&out, err)
}

func decodeCancelResult(out *bytes.Buffer, opErr error) (*mcp.CallToolResult, *Result, error) {
	// Unlike wait, a cancellation error is an operation failure even when
	// its durable outcome is already cancelled or failed.
	if opErr != nil {
		return nil, nil, toolError(opErr)
	}
	return decodeResult(out, nil)
}

func (s *dispatchToolServer) handleInspect(_ context.Context, _ *mcp.CallToolRequest, input SelectorInput) (*mcp.CallToolResult, *InspectResult, error) {
	var out bytes.Buffer
	err := Inspect(InspectRequest{
		Root: s.root, Handle: strings.TrimSpace(input.Handle),
		InvocationID: strings.TrimSpace(input.InvocationID), Stdout: &out,
	})
	result, err := decodeJSONOutput[InspectResult](&out, err)
	return nil, result, err
}

func (s *dispatchToolServer) handleOutput(_ context.Context, _ *mcp.CallToolRequest, input OutputInput) (*mcp.CallToolResult, *OutputResult, error) {
	var out bytes.Buffer
	err := Output(OutputRequest{
		Root: s.root, Handle: strings.TrimSpace(input.Handle),
		InvocationID: strings.TrimSpace(input.InvocationID), Artifact: strings.TrimSpace(input.Artifact),
		Stdout: &out,
	})
	result, err := decodeJSONOutput[OutputResult](&out, err)
	return nil, result, err
}

func decodeJSONOutput[T any](out *bytes.Buffer, opErr error) (*T, error) {
	if opErr != nil {
		return nil, toolError(opErr)
	}
	var result T
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// decodeResult turns one operation's canonical JSON rendering into the shared
// public result. A terminal provider failure writes a structured `failed`
// result and also returns an error; that pairing is a normal outcome the
// caller must see as data, not as a tool failure. Everything else — malformed
// input, invalid state transitions, configuration and transport failures — is
// a tool error.
func decodeResult(out *bytes.Buffer, opErr error) (*mcp.CallToolResult, *Result, error) {
	result, decodeErr := parseResult(out.Bytes())
	if opErr != nil {
		if decodeErr == nil && terminalDispatchState(result.State) {
			return nil, result, nil
		}
		return nil, nil, toolError(opErr)
	}
	if decodeErr != nil {
		return nil, nil, decodeErr
	}
	return nil, result, nil
}

func parseResult(data []byte) (*Result, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, errors.New("dispatch operation produced no result")
	}
	var result Result
	if err := json.Unmarshal(trimmed, &result); err != nil {
		return nil, fmt.Errorf("decode dispatch result: %w", err)
	}
	return &result, nil
}

// toolError converts a dispatch error into the text an MCP client sees. The
// exit category is dropped: MCP has no exit codes, and the message is already
// the actionable part.
func toolError(err error) error {
	var dispatchErr *ExitError
	if errors.As(err, &dispatchErr) {
		return errors.New(dispatchErr.Error())
	}
	return err
}

// relayProgress emits standards-based progress notifications while a long wait
// blocks, but only when the client supplied a progress token. It returns a stop
// function that must be called before the handler returns.
func relayProgress(ctx context.Context, request *mcp.CallToolRequest, interval time.Duration) func() {
	if request == nil || request.Session == nil || request.Params == nil || interval <= 0 {
		return func() {}
	}
	token := request.Params.GetProgressToken()
	if token == nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		started := time.Now()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(started)
				// Progress notifications are best effort: a client that stopped
				// listening must not fail an otherwise healthy wait.
				_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Progress:      elapsed.Seconds(),
					Message:       fmt.Sprintf("dispatch still running after %s", elapsed.Round(time.Second)),
				})
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}
