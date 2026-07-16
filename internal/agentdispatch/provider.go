package agentdispatch

import (
	"bufio"
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
	dispatchModeFresh      = "fresh"
	dispatchModeResume     = "resume"
	statusUnknown          = "unknown"
	processStatusAlive     = "alive"
	processStatusDead      = "dead"
	providerVersionTimeout = 10 * time.Second
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

	codexAgentMessageType  = "agent_message"
	codexItemCompletedType = "item.completed"
	invalidStructuredEvent = "invalid_structured_event"
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
	eventType, _ := value[jsonTypeKey].(string)
	if eventType != "result" {
		if len(events) == 0 && eventType != "" {
			activity := eventType
			if nested, ok := mapValueV013(value, "event"); ok {
				if nestedType, _ := nested[jsonTypeKey].(string); nestedType != "" {
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
	eventType, _ := value[jsonTypeKey].(string)
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
	case "turn.failed", "turn.aborted", jsonErrorKey:
		reason, _ := firstStringV013(value, jsonMessageKey, "reason", jsonErrorKey)
		if reason == "" {
			if details, ok := mapValueV013(value, jsonErrorKey); ok {
				reason, _ = firstStringV013(details, jsonMessageKey, "reason")
			}
		}
		if reason == "" {
			reason = "Codex reported a terminal failure"
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}, nil
	case codexAgentMessageType:
		if answer, ok := firstStringV013(value, jsonMessageKey, jsonTextKey); ok {
			return []providerEvent{{Kind: eventAnswer, Answer: answer}}, nil
		}
	case codexItemCompletedType:
		if item, ok := mapValueV013(value, "item"); ok {
			if itemType, _ := item[jsonTypeKey].(string); itemType == codexAgentMessageType {
				if answer, found := firstStringV013(item, jsonMessageKey, jsonTextKey); found {
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
	source := bufio.NewReaderSize(io.TeeReader(reader, rawWriter), structuredJSONBufferBytes)
	for {
		line := &structuredJSONLineReader{source: source}
		parser := newSelectiveJSONReader(line)
		value, err := parser.next()
		if err == io.EOF {
			if line.sourceErr != nil {
				return line.sourceErr
			}
			if line.sourceEOF {
				return nil
			}
			continue
		}
		if err != nil {
			if discardErr := parser.discard(); discardErr != nil && discardErr != io.EOF {
				return fmt.Errorf("read %s structured event after parse failure: %w", agent, discardErr)
			}
			if line.sourceErr != nil {
				return line.sourceErr
			}
			if consumeErr := consume(providerEvent{Kind: eventProgress, Activity: invalidStructuredEvent, Reason: err.Error()}); consumeErr != nil {
				return consumeErr
			}
			if line.sourceEOF {
				return nil
			}
			continue
		}
		trailing, trailingErr := parser.next()
		if trailingErr != io.EOF {
			if trailingErr == nil {
				trailingErr = fmt.Errorf("structured JSONL record contains multiple values: %#v", trailing)
			}
			if discardErr := parser.discard(); discardErr != nil && discardErr != io.EOF {
				return discardErr
			}
			if consumeErr := consume(providerEvent{Kind: eventProgress, Activity: invalidStructuredEvent, Reason: trailingErr.Error()}); consumeErr != nil {
				return consumeErr
			}
			continue
		}
		var events []providerEvent
		switch agent {
		case AgentClaude:
			events, err = reduceClaudeEvent(expectedSession, value)
		case AgentCodex:
			events, err = reduceCodexEvent(value)
		default:
			err = fmt.Errorf("unsupported structured dispatch provider %q", agent)
		}
		if err != nil {
			return err
		}
		for _, event := range events {
			if err := consume(event); err != nil {
				return err
			}
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
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := readDiagnosticLine(reader, 4*1024)
		if readErr != nil && readErr != io.EOF {
			return "", fmt.Errorf("read Antigravity dispatch log: %w", readErr)
		}
		if line == "" && readErr == io.EOF {
			break
		}
		candidate := strings.TrimSpace(line)
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
		if readErr == io.EOF {
			break
		}
	}
	return found, nil
}

// readDiagnosticLine retains only a small prefix while consuming the complete
// line. Conversation markers are short; oversized unrelated diagnostics are
// skipped without constraining the provider log itself.
func readDiagnosticLine(reader *bufio.Reader, retainBytes int) (string, error) {
	line := make([]byte, 0, retainBytes)
	truncated := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !truncated {
			remaining := retainBytes - len(line)
			if len(fragment) <= remaining {
				line = append(line, fragment...)
			} else {
				truncated = true
				line = line[:0]
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if truncated {
			return "", err
		}
		return string(line), err
	}
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
