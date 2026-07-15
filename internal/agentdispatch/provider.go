package agentdispatch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	maxStructuredEventBytes = 1024 * 1024
	maxAnswerBytes          = 16 * 1024 * 1024
	maxCaptureBytes         = 64 * 1024 * 1024
	dispatchModeFresh       = "fresh"
	dispatchModeResume      = "resume"
	statusUnknown           = "unknown"
	processStatusAlive      = "alive"
	processStatusDead       = "dead"
	providerVersionTimeout  = 10 * time.Second
	// AntigravityPromptMaxBytes retains headroom below common ARG_MAX limits
	// because Antigravity accepts print-mode prompts only as an argument.
	AntigravityPromptMaxBytes = 100 * 1024
	// AntigravityPrintTimeout keeps a headless dispatch alive long enough for
	// a normal agent turn while the runner remains responsible for cancellation.
	AntigravityPrintTimeout = "24h"
	// claudePrintBackgroundWaitCeilingEnv keeps headless Claude dispatches alive
	// for Claude-managed background work; interactive Claude launches do not use it.
	claudePrintBackgroundWaitCeilingEnv   = "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"
	claudePrintBackgroundWaitCeilingValue = "0"
)

var versionPattern = regexp.MustCompile(`\b(?:v)?(\d+\.\d+\.\d+)\b`)

const uuidExpression = `(?i:[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})`

var antigravityLogPrefix = regexp.MustCompile(`^[IWE]\d{4} \d{2}:\d{2}:\d{2}\.\d+ \d+ [^]]+\] (.*)$`)
var antigravityCreatedConversation = regexp.MustCompile(`^Created conversation (` + uuidExpression + `)$`)
var antigravityPrintConversation = regexp.MustCompile(`^Print mode: conversation=(` + uuidExpression + `), sending message$`)

type providerCommand struct {
	Path       string
	Args       []string
	Env        []string
	WorkDir    string
	Plain      bool
	SessionID  string
	LogPath    string
	Provider   string
	RunMode    string
	Structured bool
	Model      string
	Effort     string
}

type providerEvent struct {
	Kind      string
	SessionID string
	Answer    string
	Activity  string
	Reason    string
}

const (
	eventSession  = "session"
	eventAnswer   = "answer"
	eventProgress = "progress"
	eventComplete = "complete"
	eventFailure  = "failure"

	codexAgentMessageType = "agent_message"
)

var supportedProviderVersions = map[string]string{
	AgentClaude:      "2.1.207",
	AgentCodex:       "0.144.1",
	AgentAntigravity: "1.1.1",
}

func providerVersion(path string, agent string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), providerVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version") // #nosec G204 -- path is resolved from the static provider registry.
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("read %s version: %w", agent, err)
	}
	match := versionPattern.FindStringSubmatch(string(output))
	if len(match) != 2 {
		return "", fmt.Errorf("read %s version: no semantic version in %q", agent, strings.TrimSpace(string(output)))
	}
	return match[1], nil
}

// providerVersionCompatibility is the canonical compatibility comparison
// shared by capability reporting (buildTargetOptions) and execution gating
// (requireSupportedVersion). An installed version equal to the tested pin is
// compatible with no warning; a newer semantic version is compatible with a
// single warning naming both versions; an older or non-semantic version
// returns an error. The agent must exist in supportedProviderVersions.
func providerVersionCompatibility(agent string, installed string) (string, error) {
	tested := supportedProviderVersions[agent]
	comparison, err := version.Compare(installed, tested)
	if err != nil {
		return "", fmt.Errorf("%s reported version %q, which is not a semantic version; the Agent Dispatch tested version is %s", agent, installed, tested)
	}
	switch {
	case comparison < 0:
		return "", fmt.Errorf("%s version %s is older than the Agent Dispatch tested version %s and is not supported; install the tested version or update Agent Layer after compatibility evidence is available", agent, installed, tested)
	case comparison > 0:
		return fmt.Sprintf("warning: %s version %s is newer than the Agent Dispatch tested version %s; attempting dispatch optimistically", agent, installed, tested), nil
	default:
		return "", nil
	}
}

func requireSupportedVersion(path string, agent string, lookup func(string, string) (string, error)) (string, error) {
	if lookup == nil {
		lookup = providerVersion
	}
	installed, err := lookup(path, agent)
	if err != nil {
		return "", exitError(ExitUnavailable, fmt.Sprintf("cannot verify %s version before dispatch: %v", agent, err))
	}
	if _, ok := supportedProviderVersions[agent]; !ok {
		return "", exitError(ExitUsage, fmt.Sprintf("unsupported dispatch provider %q", agent))
	}
	if _, compatErr := providerVersionCompatibility(agent, installed); compatErr != nil {
		return "", exitError(ExitUnavailable, compatErr.Error())
	}
	return installed, nil
}

func buildProviderCommand(
	target targetMeta,
	project *config.ProjectConfig,
	env []string,
	prompt []byte,
	model string,
	effort string,
	mode string,
	sessionID string,
	run *dispatchRun,
	diagnostics io.Writer,
) (providerCommand, error) {
	if project == nil || run == nil {
		return providerCommand{}, exitError(ExitConfig, "build dispatch provider command without project or run")
	}
	if len(prompt) > MaxStdinPromptBytes {
		return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(prompt), MaxStdinPromptBytes))
	}
	command := providerCommand{Path: target.Binary, Env: env, Provider: target.Name, RunMode: mode}
	switch target.Name {
	case AgentClaude:
		if mode == dispatchModeFresh && sessionID == "" {
			return providerCommand{}, exitError(ExitConfig, "new Claude dispatch requires a caller-assigned session ID")
		}
		args := []string{"--print", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
		if mode == dispatchModeResume {
			args = append(args, "--resume", sessionID)
		} else {
			args = append(args, "--session-id", sessionID)
		}
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Claude.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		resolvedEffort := strings.TrimSpace(effort)
		if resolvedEffort == "" && !config.HasProviderPassthroughKey(project.Config.Agents.Claude.AgentSpecific, "effortLevel") {
			resolvedEffort = strings.TrimSpace(project.Config.Agents.Claude.ReasoningEffort)
		}
		if resolvedEffort != "" {
			args = append(args, "--effort", resolvedEffort)
		}
		command.Model = resolvedModel
		command.Effort = resolvedEffort
		if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
			args = append(args, "--dangerously-skip-permissions")
		}
		command.Args = args
		command.Env = claude.ConfigureEnvironment(project.Root, env, project.Config.Agents.Claude, diagnostics)
		command.Env = clients.UnsetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv)
		command.Env = clients.SetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv, claudePrintBackgroundWaitCeilingValue)
		command.SessionID = sessionID
		command.Structured = true
	case AgentCodex:
		args := []string{"exec"}
		if mode == dispatchModeResume {
			args = append(args, "resume")
		}
		args = append(args, "--json")
		if mode == dispatchModeResume {
			args = append(args, sessionID)
		}
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexModelKey) {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Codex.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		resolvedEffort := strings.TrimSpace(effort)
		if resolvedEffort == "" && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexReasoningEffortKey) {
			resolvedEffort = strings.TrimSpace(project.Config.Agents.Codex.ReasoningEffort)
		}
		if resolvedEffort != "" {
			args = append(args, "-c", "model_reasoning_effort="+resolvedEffort)
		}
		command.Model = resolvedModel
		command.Effort = resolvedEffort
		if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
			if !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexApprovalPolicyKey) {
				args = append(args, "-c", "approval_policy=never")
			}
			if !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexSandboxModeKey) {
				args = append(args, "-c", "sandbox_mode=danger-full-access")
			}
			if !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexWebSearchKey) {
				args = append(args, "-c", "web_search=live")
			}
		}
		args = append(args, "-")
		command.Args = args
		command.Env = codex.ConfigureEnvironment(project.Root, env, project.Config.Agents.Codex, diagnostics)
		command.SessionID = sessionID
		command.Structured = true
	case AgentAntigravity:
		if len(prompt) > AntigravityPromptMaxBytes {
			return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("antigravity prompt is %d bytes; `al dispatch` caps it at %d bytes because agy --print has no stdin/file path. Use --agent claude or --agent codex for larger prompts.", len(prompt), AntigravityPromptMaxBytes))
		}
		args, err := antigravity.BaseArgs(project.Root, project.Config)
		if err != nil {
			return providerCommand{}, wrapExitError(ExitConfig, "prepare Antigravity launch", err)
		}
		logPath := filepath.Join(run.Dir, "antigravity.log")
		file, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 -- path is inside an isolated UUID run directory.
		if err != nil {
			return providerCommand{}, wrapExitError(ExitConfig, "create Antigravity dispatch log", err)
		}
		if closeErr := file.Close(); closeErr != nil {
			return providerCommand{}, wrapExitError(ExitConfig, "close Antigravity dispatch log", closeErr)
		}
		args = append(args, "--log-file", logPath)
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Antigravity.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		command.Model = resolvedModel
		if mode == dispatchModeResume {
			args = append(args, "--conversation", sessionID)
		}
		args = append(args, "--print-timeout", AntigravityPrintTimeout, "--print", string(prompt))
		command.Args = args
		command.Env = antigravity.ConfigureEnvironment(env)
		command.SessionID = sessionID
		command.LogPath = logPath
		command.Plain = true
	default:
		return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("unsupported dispatch provider %q", target.Name))
	}
	return command, nil
}

func reduceStructuredEvent(agent string, expectedSession string, raw []byte) ([]providerEvent, error) {
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s emitted unreadable structured output: %w", agent, err)
	}
	switch agent {
	case AgentClaude:
		return reduceClaudeEvent(expectedSession, value)
	case AgentCodex:
		return reduceCodexEvent(value)
	default:
		return nil, fmt.Errorf("unsupported structured dispatch provider %q", agent)
	}
}

func reduceClaudeEvent(expected string, value map[string]any) ([]providerEvent, error) {
	events := make([]providerEvent, 0, 2)
	if text, ok := claudeTextDeltaV013(value); ok && text != "" {
		events = append(events, providerEvent{Kind: eventProgress, Activity: "text_delta"})
	}
	eventType, _ := value["type"].(string)
	if eventType != "result" {
		if len(events) == 0 && eventType != "" {
			activity := eventType
			if nested, ok := mapValueV013(value, "event"); ok {
				if nestedType, _ := nested["type"].(string); nestedType != "" {
					activity = nestedType
				}
			}
			events = append(events, providerEvent{Kind: eventProgress, Activity: activity})
		}
		return events, nil
	}
	if claudeResultIsErrorV013(value) {
		reason, _ := value["result"].(string)
		if reason == "" {
			reason = "Claude reported a terminal failure"
		}
		return append(events, providerEvent{Kind: eventFailure, Reason: reason}), nil
	}
	id, _ := firstStringV013(value, "session_id", "sessionId")
	if id == "" || id != expected {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Claude terminal result did not return the caller-assigned session ID"}), nil
	}
	answer, _ := value["result"].(string)
	if answer == "" {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Claude terminal result did not contain a final answer"}), nil
	}
	events = append(events, providerEvent{Kind: eventSession, SessionID: id}, providerEvent{Kind: eventAnswer, Answer: answer}, providerEvent{Kind: eventComplete})
	return events, nil
}

func reduceCodexEvent(value map[string]any) ([]providerEvent, error) {
	eventType, _ := value["type"].(string)
	switch eventType {
	case "thread.started":
		id, _ := firstStringV013(value, "thread_id", "threadId", "id")
		if id == "" {
			// Ignore a duplicate/incomplete lifecycle notification. The shared
			// completion invariant still rejects the run unless a separate
			// thread.started event supplied an exact thread ID.
			return nil, nil
		}
		return []providerEvent{{Kind: eventSession, SessionID: id}}, nil
	case "turn.completed":
		return []providerEvent{{Kind: eventComplete}}, nil
	case "turn.failed", "turn.aborted", "error":
		reason, _ := firstStringV013(value, "message", "reason", "error")
		if reason == "" {
			if details, ok := mapValueV013(value, "error"); ok {
				reason, _ = firstStringV013(details, "message", "reason")
			}
		}
		if reason == "" {
			reason = "Codex reported a terminal failure"
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}, nil
	case codexAgentMessageType:
		if answer, ok := firstStringV013(value, "message", "text"); ok {
			return []providerEvent{{Kind: eventAnswer, Answer: answer}}, nil
		}
	case "item.completed":
		if item, ok := mapValueV013(value, "item"); ok {
			if itemType, _ := item["type"].(string); itemType == codexAgentMessageType {
				if answer, found := firstStringV013(item, "message", "text"); found {
					return []providerEvent{{Kind: eventAnswer, Answer: answer}}, nil
				}
			}
		}
	}
	if eventType != "" {
		return []providerEvent{{Kind: eventProgress, Activity: eventType}}, nil
	}
	return nil, nil
}

func readStructuredEvents(reader io.Reader, rawWriter io.Writer, agent string, expectedSession string, consume func(providerEvent) error) error {
	buffered := bufio.NewReaderSize(reader, 64*1024)
	line := make([]byte, 0, 64*1024)
	for {
		fragment, readErr := buffered.ReadSlice('\n')
		if len(fragment) > 0 {
			if _, err := rawWriter.Write(fragment); err != nil {
				return err
			}
			line = append(line, fragment...)
			contentBytes := len(line)
			if line[contentBytes-1] == '\n' {
				contentBytes--
			}
			if contentBytes > maxStructuredEventBytes {
				return fmt.Errorf("read %s structured event: event exceeded %d byte limit", agent, maxStructuredEventBytes)
			}
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr != nil && readErr != io.EOF {
			return fmt.Errorf("read %s structured event (maximum %d bytes): %w", agent, maxStructuredEventBytes, readErr)
		}
		if len(line) == 0 && readErr == io.EOF {
			return nil
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			events, err := reduceStructuredEvent(agent, expectedSession, trimmed)
			if err != nil {
				return err
			}
			for _, event := range events {
				if err := consume(event); err != nil {
					return err
				}
			}
		}
		line = line[:0]
		if readErr == io.EOF {
			return nil
		}
	}
}

// antigravitySessionID extracts one consistent conversation ID from a run log.
func antigravitySessionID(logPath string) (string, error) {
	file, err := os.Open(logPath) // #nosec G304 -- path is created in this run's private directory.
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	found := ""
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxCaptureBytes)
	for scanner.Scan() {
		candidate := strings.TrimSpace(scanner.Text())
		if match := antigravityLogPrefix.FindStringSubmatch(candidate); len(match) == 2 {
			candidate = match[1]
		}
		var id string
		if match := antigravityCreatedConversation.FindStringSubmatch(candidate); len(match) == 2 {
			id = strings.ToLower(match[1])
		} else if match := antigravityPrintConversation.FindStringSubmatch(candidate); len(match) == 2 {
			id = strings.ToLower(match[1])
		}
		if id == "" {
			continue
		}
		if found != "" && found != id {
			return "", fmt.Errorf("antigravity dispatch log reported conflicting conversation IDs %s and %s", found, id)
		}
		found = id
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read Antigravity dispatch log: %w", err)
	}
	return found, nil
}

func compatibleTargetVersion(path string, target targetMeta, lookup func(string, string) (string, error)) (targetMeta, string, error) {
	installed, err := requireSupportedVersion(path, target.Name, lookup)
	if err != nil {
		return targetMeta{}, "", err
	}
	target.Binary = path
	return target, installed, nil
}

func dispatchEnvironment(base []string, project *config.ProjectConfig, dispatchRun *dispatchRun, depth int, target string) []string {
	info := &run.Info{ID: dispatchRun.Record.ID, Dir: dispatchRun.Dir}
	env := clients.BuildEnvForAgent(base, project.Env, info, target)
	return clients.SetEnv(env, clients.EnvDispatchActive, fmt.Sprintf("%d", depth))
}
