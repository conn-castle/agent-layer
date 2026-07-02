package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const agyDisableAutoUpdateEnv = "AGY_CLI_DISABLE_AUTO_UPDATE"

// AntigravityPromptMaxBytes caps the prompt size for the Antigravity adapter.
// agy --print accepts the prompt only as an argv element (no stdin/file path
// exists today), which is subject to the OS ARG_MAX limit (~128 KB Linux /
// ~256 KB macOS). 100 KB is well below both caps and leaves headroom for the
// other argv elements; oversize prompts fail fast with ExitUsage rather than
// surfacing as an opaque execve failure.
const AntigravityPromptMaxBytes = 100 * 1024

// AntigravityPrintTimeout is the print-mode timeout used for dispatch. The
// value and required space-separated flag form come from the 2026-05-22 local
// `agy --help` and print-mode probe recorded in .agent-layer/tmp/agent-dispatch-design.md.
const AntigravityPrintTimeout = "24h"

func runTarget(target targetMeta, project *config.ProjectConfig, env []string, prompt []byte, opts RunOptions) error {
	factory := opts.NewCommand
	if factory == nil {
		factory = defaultCommandFactory
	}
	switch target.Name {
	case AgentClaude:
		return runClaude(target, project, env, prompt, opts, factory)
	case AgentCodex:
		return runCodex(target, project, env, prompt, opts, factory)
	case AgentAntigravity:
		return runAntigravity(target, project, env, prompt, opts, factory)
	default:
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchUnknownTargetFmt, target.Name))
	}
}

func defaultCommandFactory(name string, args ...string) *exec.Cmd {
	// #nosec G204 -- dispatch target binaries come from the static registry.
	return exec.CommandContext(context.Background(), name, args...)
}

func runClaude(target targetMeta, project *config.ProjectConfig, env []string, prompt []byte, opts RunOptions, factory CommandFactory) error {
	args := []string{"--print", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(project.Config.Agents.Claude.Model)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	effort := strings.TrimSpace(opts.ReasoningEffort)
	if effort == "" && !config.HasProviderPassthroughKey(project.Config.Agents.Claude.AgentSpecific, "effortLevel") {
		effort = strings.TrimSpace(project.Config.Agents.Claude.ReasoningEffort)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
	}
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	env = configureClaudeEnv(project.Root, env, project.Config.Agents.Claude, opts.Stderr)
	cmd := factory(target.Binary, args...)
	cmd.Dir = project.Root
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(prompt)
	return runStructuredCommand(cmd, AgentClaude, opts.Stdout, opts.Stderr, decodeClaudeStream)
}

func runCodex(target targetMeta, project *config.ProjectConfig, env []string, prompt []byte, opts RunOptions, factory CommandFactory) error {
	args := []string{"exec", "--json"}
	if model := strings.TrimSpace(opts.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := strings.TrimSpace(opts.ReasoningEffort); effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+effort)
	}
	args = append(args, "-")
	env = configureCodexEnv(project.Root, env, project.Config.Agents.Codex, opts.Stderr)
	cmd := factory(target.Binary, args...)
	cmd.Dir = project.Root
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(prompt)
	return runStructuredCommand(cmd, AgentCodex, opts.Stdout, opts.Stderr, decodeCodexStream)
}

func runAntigravity(target targetMeta, project *config.ProjectConfig, env []string, prompt []byte, opts RunOptions, factory CommandFactory) error {
	// Antigravity has no stdin/file path for the prompt today, so the prompt
	// becomes a single argv element. Reject oversize prompts here so callers
	// get a clear error instead of an opaque execve `argument list too long`.
	if len(prompt) > AntigravityPromptMaxBytes {
		return exitError(ExitUsage, fmt.Sprintf(messages.DispatchAntigravityPromptTooLargeFmt, len(prompt), AntigravityPromptMaxBytes))
	}
	geminiDir := filepath.Join(project.Root, ".agy")
	args := []string{"--gemini_dir=" + geminiDir}
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--dangerously-skip-permissions")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(project.Config.Agents.Antigravity.Model)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, "--print-timeout", AntigravityPrintTimeout, "--print", string(prompt))
	env = clients.SetEnv(env, agyDisableAutoUpdateEnv, "1")
	cmd := factory(target.Binary, args...)
	cmd.Dir = project.Root
	cmd.Env = env
	cmd.Stdin = nil
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	if err := cmd.Start(); err != nil {
		return startError(AgentAntigravity, err)
	}
	return waitWithSignal(cmd, AgentAntigravity)
}

func configureClaudeEnv(root string, env []string, cfg config.ClaudeConfig, stderr io.Writer) []string {
	expected := filepath.Join(root, ".claude-config")
	if cfg.LocalConfigDir != nil && *cfg.LocalConfigDir {
		current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
		if !ok || current == "" {
			return clients.SetEnv(env, "CLAUDE_CONFIG_DIR", expected)
		}
		if !clients.SamePath(current, expected) && stderr != nil {
			_, _ = fmt.Fprintf(stderr, messages.ClientsClaudeConfigDirWarningFmt, current, expected)
		}
		return env
	}
	current, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if ok && clients.SamePath(current, expected) {
		return clients.UnsetEnv(env, "CLAUDE_CONFIG_DIR")
	}
	return env
}

func configureCodexEnv(root string, env []string, cfg config.CodexConfig, stderr io.Writer) []string {
	if !config.CodexLocalConfigDirEnabled(cfg) {
		return env
	}

	expected := filepath.Join(root, ".codex")
	current, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || current == "" {
		return clients.SetEnv(env, "CODEX_HOME", expected)
	}
	if !clients.SamePath(current, expected) && stderr != nil {
		_, _ = fmt.Fprintf(stderr, messages.ClientsCodexHomeWarningFmt, current, expected)
	}
	return env
}

type streamDecoder func(io.Reader, io.Writer, io.Writer) error

// syncWriter serializes Write calls so multiple goroutines can share an
// underlying io.Writer (such as os.Stderr or bytes.Buffer) without
// corrupting partial multi-byte messages or racing.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func newSyncWriter(w io.Writer) *syncWriter {
	return &syncWriter{w: w}
}

// Write writes the entire buffer under the mutex.
func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.w == nil {
		return len(p), nil
	}
	return s.w.Write(p)
}

func runStructuredCommand(cmd *exec.Cmd, target string, stdout io.Writer, stderr io.Writer, decoder streamDecoder) error {
	outPipe, err := cmd.StdoutPipe()
	if err != nil {
		return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStartTargetFailedFmt, target, err), err)
	}
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStartTargetFailedFmt, target, err), err)
	}
	if err := cmd.Start(); err != nil {
		return startError(target, err)
	}
	// Install the signal forwarder before draining pipes so SIGINT/SIGTERM
	// arriving during the streaming window is forwarded to the child. Doing
	// signal setup only inside cmd.Wait would leave the bulk of the dispatch
	// lifetime (while the decoder/copier goroutines are still reading) with
	// no forwarding, orphaning the child on Ctrl-C.
	caughtSig, stopForwarder := installSignalForwarder(cmd)
	defer stopForwarder()

	// Both the stderr copier and the decoder's progress writes share the
	// caller-supplied stderr; wrap it once so concurrent writes serialize.
	sharedStderr := newSyncWriter(stderr)
	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(sharedStderr, errPipe)
		close(copyDone)
	}()
	decodeDone := make(chan error, 1)
	go func() {
		decodeDone <- decoder(outPipe, stdout, sharedStderr)
	}()
	// os/exec.Cmd.StdoutPipe: "It is incorrect to call Wait before all reads
	// from the pipe have completed." The dispatched targets write and then
	// exit, so EOF on the pipes is guaranteed and these reads will return
	// without help from Wait. Drain both readers first, then reap the child.
	decodeErr := <-decodeDone
	if decodeErr != nil {
		// The decoder returned early (e.g. malformed JSON or a downstream
		// write error) and is no longer reading outPipe. A child that
		// keeps producing stdout would block on a full pipe and prevent
		// cmd.Wait from ever returning. Drain the rest of stdout into the
		// void so the child can finish and the wait can complete; if the
		// child hangs producing more output forever, the user's SIGINT
		// still flows through installSignalForwarder above.
		_, _ = io.Copy(io.Discard, outPipe)
	}
	<-copyDone
	waitErr := cmd.Wait()
	if sig := caughtSig(); sig != nil {
		if sig == os.Interrupt {
			return exitError(ExitSigint, fmt.Sprintf(messages.DispatchSignalExitFmt, target, "SIGINT"))
		}
		return exitError(ExitSigterm, fmt.Sprintf(messages.DispatchSignalExitFmt, target, "SIGTERM"))
	}
	if waitErr != nil {
		return mapWaitError(target, waitErr)
	}
	if decodeErr != nil {
		return decodeErr
	}
	return nil
}

// installSignalForwarder begins forwarding SIGINT/SIGTERM received by this
// process to cmd.Process. The returned caughtSig getter reports the first
// signal observed (nil until one arrives). stop must be called to release
// signal.Notify resources and join the forwarder goroutine.
func installSignalForwarder(cmd *exec.Cmd) (caughtSig func() os.Signal, stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	exited := make(chan struct{})
	var (
		mu    sync.Mutex
		first os.Signal
	)
	go func() {
		defer close(exited)
		for {
			select {
			case sig := <-signals:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
				mu.Lock()
				if first == nil {
					first = sig
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	getter := func() os.Signal {
		mu.Lock()
		defer mu.Unlock()
		return first
	}
	stopper := func() {
		signal.Stop(signals)
		close(done)
		<-exited
	}
	return getter, stopper
}

func startError(target string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		meta, _ := lookupTarget(target)
		return exitError(ExitUnavailable, fmt.Sprintf(messages.DispatchMissingBinaryFmt, target, meta.Binary))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStartTargetFailedFmt, target, err), err)
}

func waitWithSignal(cmd *exec.Cmd, target string) error {
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case sig := <-signals:
		if cmd.Process != nil {
			_ = cmd.Process.Signal(sig)
		}
		<-waitDone
		if sig == os.Interrupt {
			return exitError(ExitSigint, fmt.Sprintf(messages.DispatchSignalExitFmt, target, "SIGINT"))
		}
		return exitError(ExitSigterm, fmt.Sprintf(messages.DispatchSignalExitFmt, target, "SIGTERM"))
	case err := <-waitDone:
		return mapWaitError(target, err)
	}
}

func mapWaitError(target string, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code <= 0 {
			code = 1
		}
		return exitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchTargetNonZeroFmt, target, code))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStartTargetFailedFmt, target, err), err)
}

func decodeClaudeStream(reader io.Reader, stdout io.Writer, stderr io.Writer) error {
	decoder := json.NewDecoder(reader)
	sawRecognizedTextEvent := false
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				if !sawRecognizedTextEvent {
					return exitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchNoRecognizedTextEventFmt, AgentClaude))
				}
				return nil
			}
			return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInvalidStructuredOutputFmt, AgentClaude, err), err)
		}
		if text, ok := claudeTextDelta(raw); ok {
			sawRecognizedTextEvent = true
			if _, err := io.WriteString(stdout, text); err != nil {
				return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStdoutWriteFailedFmt, AgentClaude, err), err)
			}
			continue
		}
		// Surface result events that report errors (e.g. permission/policy
		// failures that Claude reports in-stream while still exiting 0).
		if eventType, _ := raw["type"].(string); eventType == "result" {
			if claudeResultIsError(raw) && stderr != nil {
				writeClaudeResultError(stderr, raw)
			}
		}
	}
}

// claudeResultIsError reports whether a Claude `result` event signals an
// error via the documented `is_error: true` or `subtype: error*` shape.
func claudeResultIsError(raw map[string]any) bool {
	if isErr, ok := raw["is_error"].(bool); ok && isErr {
		return true
	}
	if subtype, ok := raw["subtype"].(string); ok && strings.HasPrefix(subtype, "error") {
		return true
	}
	return false
}

// writeClaudeResultError emits the error payload from a Claude `result`
// event as a single stderr line so callers see the failure cause even when
// Claude reports the error in-stream.
func writeClaudeResultError(stderr io.Writer, raw map[string]any) {
	payload, _ := raw["result"].(string)
	subtype, _ := raw["subtype"].(string)
	if payload == "" {
		payload = subtype
	}
	if payload == "" {
		payload = "error"
	}
	_, _ = fmt.Fprintf(stderr, "%s: error: %s\n", AgentClaude, payload)
}

func claudeTextDelta(raw map[string]any) (string, bool) {
	if delta, ok := mapValue(raw, "delta"); ok {
		return textFromDelta(delta)
	}
	if event, ok := mapValue(raw, "event"); ok {
		if delta, ok := mapValue(event, "delta"); ok {
			return textFromDelta(delta)
		}
	}
	return "", false
}

func textFromDelta(delta map[string]any) (string, bool) {
	if deltaType, _ := delta["type"].(string); deltaType != "text_delta" {
		return "", false
	}
	text, ok := delta["text"].(string)
	return text, ok
}

func decodeCodexStream(reader io.Reader, stdout io.Writer, stderr io.Writer) error {
	// Use json.NewDecoder rather than bufio.Scanner: Codex `item.completed`
	// events can embed large command outputs that exceed any fixed line cap,
	// and json.NewDecoder reads each JSON value token-by-token without a
	// per-line limit.
	decoder := json.NewDecoder(reader)
	sawRecognizedTextEvent := false
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				if !sawRecognizedTextEvent {
					return exitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchNoRecognizedTextEventFmt, AgentCodex))
				}
				return nil
			}
			return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchInvalidStructuredOutputFmt, AgentCodex, err), err)
		}
		if text, ok := codexAgentText(raw); ok {
			sawRecognizedTextEvent = true
			if _, err := io.WriteString(stdout, text); err != nil {
				return wrapExitError(ExitTargetFailure, fmt.Sprintf(messages.DispatchStdoutWriteFailedFmt, AgentCodex, err), err)
			}
			continue
		}
		if eventType, ok := raw["type"].(string); ok && stderr != nil {
			_, _ = fmt.Fprintf(stderr, "%s: %s\n", AgentCodex, eventType)
		}
	}
}

func codexAgentText(raw map[string]any) (string, bool) {
	if eventType, _ := raw["type"].(string); eventType == "agent_message" {
		return firstString(raw, "message", "text")
	}
	if item, ok := mapValue(raw, "item"); ok {
		if itemType, _ := item["type"].(string); itemType == "agent_message" {
			return firstString(item, "message", "text")
		}
	}
	return "", false
}

func firstString(values map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value, true
		}
	}
	return "", false
}

func mapValue(values map[string]any, key string) (map[string]any, bool) {
	value, ok := values[key].(map[string]any)
	return value, ok
}
