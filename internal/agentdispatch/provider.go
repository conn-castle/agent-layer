package agentdispatch

import (
	"bufio"
	"context"
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
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	dispatchModeFresh      = "fresh"
	dispatchModeResume     = "resume"
	statusUnknown          = "unknown"
	processStatusAlive     = "alive"
	processStatusDead      = "dead"
	providerVersionTimeout = 10 * time.Second
	maxRetainedAnswerBytes = 256 * 1024 * 1024
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
	truncatedAnswerNotice                 = "\n\n[Agent Layer truncated this final answer after retaining 256 MiB. Resume the conversation and ask the agent to summarize the final answer in another turn.]\n"
	grokEventDataTruncatedNotice          = "\n\n[Agent Layer truncated this Grok text event after retaining 64 KiB.]\n"
	// claudePermissionModeAcceptEdits auto-accepts file edits and common
	// filesystem commands for paths in the working directory. Other commands
	// still require an allow rule, which dispatch supplies via --allowedTools.
	claudePermissionModeAcceptEdits = "acceptEdits"
	// claudePermissionModeDontAsk denies anything without an allow rule instead
	// of waiting for a prompt that a headless run can never answer.
	claudePermissionModeDontAsk = "dontAsk"
	// claudePermissionsPassthroughKey and claudeDefaultModePassthroughKey locate
	// an explicit permission mode in agents.claude.agent_specific.
	claudePermissionsPassthroughKey = "permissions"
	claudeDefaultModePassthroughKey = "defaultMode"
	// permissionDenialsKey is the Claude terminal-result field listing tool calls
	// that were denied. Claude still reports such a run as a success.
	permissionDenialsKey          = "permission_denials"
	toolNameKey                   = "tool_name"
	textDeltaActivity             = "text_delta"
	thoughtActivity               = "thought"
	grokToolUpdateType            = "tool_call_update"
	grokToolFailedStatus          = "failed"
	grokUsageEventType            = "usage"
	grokToolDeniedPrefix          = "Tool `"
	grokPermissionDenied          = " was not executed: Denied by permission policy:"
	grokToolUpdateTruncatedNotice = "\n\n[Agent Layer truncated this Grok tool update after retaining 512 bytes.]\n"
	antigravityEffortLow          = "low"
	antigravityEffortMedium       = "medium"
	antigravityEffortHigh         = "high"
)

// codexDispatchSandboxMode resolves the Codex sandbox for a non-YOLO dispatch.
// Codex has no separate edit-approval rule, so the sandbox is what grants or
// denies unprompted edits; it therefore tracks whether commands are approved.
func codexDispatchSandboxMode(cfg config.Config, commandsAllow []string) string {
	if projection.BuildApprovals(cfg, commandsAllow).AllowCommands {
		return config.CodexSandboxWorkspaceWrite
	}
	return config.CodexSandboxReadOnly
}

// claudeDispatchPermissionMode resolves the Claude permission mode for a
// non-YOLO dispatch, mirroring codexDispatchSandboxMode.
func claudeDispatchPermissionMode(cfg config.Config, commandsAllow []string) string {
	if projection.BuildApprovals(cfg, commandsAllow).AllowCommands {
		return claudePermissionModeAcceptEdits
	}
	return claudePermissionModeDontAsk
}

// claudeDefaultPermissionModePinned reports whether the project pinned an
// explicit Claude permission mode, which dispatch must not override.
func claudeDefaultPermissionModePinned(passthrough config.ProviderPassthrough) bool {
	permissions, ok := passthrough[claudePermissionsPassthroughKey].(map[string]any)
	if !ok {
		return false
	}
	_, ok = permissions[claudeDefaultModePassthroughKey]
	return ok
}

var versionPattern = regexp.MustCompile(`\b(?:v)?(\d+\.\d+\.\d+)\b`)

const uuidExpression = `(?i:[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})`

var antigravityLogPrefix = regexp.MustCompile(`^[IWE]\d{4} \d{2}:\d{2}:\d{2}\.\d+ \d+ [^]]+\] (.*)$`)
var antigravityCreatedConversation = regexp.MustCompile(`^Created conversation (` + uuidExpression + `)$`)
var antigravityPrintConversation = regexp.MustCompile(`^Print mode: conversation=(` + uuidExpression + `), sending message$`)

type providerCommand struct {
	Path          string
	Args          []string
	Env           []string
	WorkDir       string
	SessionID     string
	LogPath       string
	Provider      string
	RunMode       string
	Model         string
	Effort        string
	ClaudeLineage bool
}

type providerEvent struct {
	Kind      string
	SessionID string
	Answer    string
	Activity  string
	Reason    string
	// Usage is retained in provider.stdout for benchmark attribution.  The
	// reducer keeps it visible to structured consumers rather than discarding
	// the only request-level billing evidence.
	Usage map[string]any
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
	AgentClaude:      claudeTestedVersion,
	AgentCodex:       "0.144.1",
	AgentAntigravity: "1.1.21",
	AgentGrok:        grok.SupportedVersion,
}

const (
	claudeTestedVersion         = "2.1.207"
	claudeLineageMinimumVersion = "2.1.211"
)

func claudeLineageSupported(providerVersion string) (bool, error) {
	comparison, err := version.Compare(providerVersion, claudeLineageMinimumVersion)
	if err != nil {
		return false, fmt.Errorf("claude reported version %q, which cannot be evaluated for descendant lineage: %w", providerVersion, err)
	}
	return comparison >= 0, nil
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
// shared by option availability (buildTargetOptions) and execution gating
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
	targetPinned bool,
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
		lineage, err := claudeLineageSupported(run.Record.ProviderVersion)
		if err != nil {
			return providerCommand{}, exitError(ExitConfig, err.Error())
		}
		args := []string{"--print", "--output-format", "stream-json", "--verbose", "--include-partial-messages"}
		if lineage {
			args = append(args, "--forward-subagent-text")
		}
		if mode == dispatchModeResume {
			args = append(args, "--resume", sessionID)
		} else {
			args = append(args, "--session-id", sessionID)
		}
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" && !targetPinned {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Claude.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		resolvedEffort := strings.TrimSpace(effort)
		if resolvedEffort == "" && !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Claude.AgentSpecific, "effortLevel") {
			resolvedEffort = strings.TrimSpace(project.Config.Agents.Claude.ReasoningEffort)
		}
		if resolvedEffort != "" {
			args = append(args, "--effort", resolvedEffort)
		}
		command.Model = resolvedModel
		command.Effort = resolvedEffort
		if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
			args = append(args, "--dangerously-skip-permissions")
		} else {
			if !claudeDefaultPermissionModePinned(project.Config.Agents.Claude.AgentSpecific) {
				args = append(args, "--permission-mode", claudeDispatchPermissionMode(project.Config, project.CommandsAllow))
			}
			// Claude ignores a project's permissions.allow rules until the
			// workspace trust dialog is accepted, and that dialog never appears
			// under --print. The generated settings file alone would leave every
			// approval inert, so the same rules are delivered on the command line.
			// The flag repeats rather than joining on "," so a command pattern
			// containing a comma cannot corrupt the list.
			for _, rule := range projection.ClaudeAllowRules(
				project.Config,
				project.CommandsAllow,
				projection.EffectiveServerIDs(project.Config, projection.ClientClaude),
			) {
				args = append(args, "--allowedTools", rule)
			}
		}
		command.Args = args
		command.Env = claude.ConfigureEnvironment(project.Root, env, project.Config.Agents.Claude, diagnostics)
		command.Env = clients.UnsetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv)
		command.Env = clients.SetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv, claudePrintBackgroundWaitCeilingValue)
		command.SessionID = sessionID
		command.ClaudeLineage = lineage
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
		if resolvedModel == "" && !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexModelKey) {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Codex.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		resolvedEffort := strings.TrimSpace(effort)
		if resolvedEffort == "" && !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexReasoningEffortKey) {
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
				args = append(args, "-c", config.CodexSandboxModeKey+"="+config.CodexSandboxDangerFullAccess)
			}
			if !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexWebSearchKey) {
				args = append(args, "-c", "web_search=live")
			}
		} else if !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexSandboxModeKey) {
			// `codex exec` defaults to a read-only sandbox, unlike the interactive
			// TUI, which starts a version-controlled folder at workspace-write.
			// Without this the sandbox silently contradicts approvals.mode: an
			// allowlisted command is approved and then fails on its first write.
			args = append(args, "-c", config.CodexSandboxModeKey+"="+codexDispatchSandboxMode(project.Config, project.CommandsAllow))
		}
		args = append(args, "-")
		command.Args = args
		command.Env = codex.ConfigureEnvironment(project.Root, env, project.Config.Agents.Codex, diagnostics)
		command.SessionID = sessionID
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
		if derivedEffort, ok := antigravitySlugEffort(resolvedModel); ok {
			configured := strings.TrimSpace(effort)
			if configured != "" && configured != derivedEffort {
				return providerCommand{}, exitError(ExitConfig, fmt.Sprintf("Antigravity model %q requires reasoning effort %q, got %q", resolvedModel, derivedEffort, configured))
			}
			// The exact benchmark model slug selects its thinking tier. Passing a
			// second effort flag risks contradictory client behavior.
			command.Effort = derivedEffort
		} else {
			command.Effort = strings.TrimSpace(effort)
		}
		args = append(args, "--output-format", "stream-json")
		args = append(args, "--print-timeout", AntigravityPrintTimeout, "--print", string(prompt))
		command.Args = args
		command.Env = antigravity.ConfigureEnvironment(env)
		command.SessionID = sessionID
		command.LogPath = logPath
	case AgentGrok:
		if mode == dispatchModeFresh && sessionID == "" {
			return providerCommand{}, exitError(ExitConfig, "new Grok dispatch requires a caller-assigned session ID")
		}
		promptPath := filepath.Join(run.Dir, "prompt.txt")
		if err := os.WriteFile(promptPath, prompt, 0o600); err != nil {
			return providerCommand{}, wrapExitError(ExitConfig, "write Grok dispatch prompt", err)
		}
		args := []string{"--no-auto-update", "--prompt-file", promptPath, "--output-format", "streaming-json"}
		if mode == dispatchModeResume {
			args = append(args, "--resume", sessionID)
		} else {
			args = append(args, "--session-id", sessionID)
		}
		resolvedModel := strings.TrimSpace(model)
		if resolvedModel == "" && !targetPinned {
			resolvedModel = strings.TrimSpace(project.Config.Agents.Grok.Model)
		}
		if resolvedModel != "" {
			args = append(args, "--model", resolvedModel)
		}
		resolvedEffort := strings.TrimSpace(effort)
		if resolvedEffort == "" && !targetPinned {
			resolvedEffort = strings.TrimSpace(project.Config.Agents.Grok.ReasoningEffort)
		}
		if resolvedEffort != "" {
			args = append(args, "--reasoning-effort", resolvedEffort)
		}
		command.Model = resolvedModel
		command.Effort = resolvedEffort
		if config.GrokDisableMemory(project.Config.Agents.Grok) {
			args = append(args, "--no-memory")
		}
		args = append(args, grok.SandboxArgs(project.Config, project.CommandsAllow)...)
		if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
			args = append(args, "--permission-mode", "bypassPermissions", "--always-approve")
		} else {
			if projection.BuildApprovals(project.Config, project.CommandsAllow).AllowCommands {
				args = append(args, "--permission-mode", claudePermissionModeAcceptEdits)
			} else {
				args = append(args, "--permission-mode", claudePermissionModeDontAsk)
			}
			for _, rule := range projection.ClaudeAllowRules(
				project.Config,
				project.CommandsAllow,
				projection.EffectiveServerIDs(project.Config, projection.ClientGrok),
			) {
				args = append(args, "--allow", rule)
			}
		}
		command.Args = args
		if err := grok.EnsureHome(project.Root); err != nil {
			return providerCommand{}, wrapExitError(ExitConfig, "prepare Grok home", err)
		}
		command.Env = grok.ConfigureEnvironment(project.Root, env, project.Config.Agents.Grok, diagnostics)
		command.SessionID = sessionID
	default:
		return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("unsupported dispatch provider %q", target.Name))
	}
	return command, nil
}

func antigravitySlugEffort(model string) (string, bool) {
	for _, effort := range []string{antigravityEffortLow, antigravityEffortMedium, antigravityEffortHigh} {
		if strings.HasSuffix(strings.TrimSpace(model), "-"+effort) {
			return effort, true
		}
	}
	return "", false
}

func antigravityEffortMatchesSlug(agent, model, effort string) bool {
	if agent != AgentAntigravity {
		return false
	}
	derived, ok := antigravitySlugEffort(model)
	return ok && derived == strings.ToLower(strings.TrimSpace(effort))
}

func reduceClaudeEvent(expected string, value map[string]any) []providerEvent {
	events := make([]providerEvent, 0, 2)
	if text, ok := claudeTextDeltaV013(value); ok && text != "" {
		events = append(events, providerEvent{Kind: eventProgress, Activity: textDeltaActivity})
	}
	eventType, _ := value[jsonTypeKey].(string)
	if eventType != jsonResultKey {
		if len(events) == 0 && eventType != "" {
			activity := eventType
			if nested, ok := mapValueV013(value, "event"); ok {
				if nestedType, _ := nested[jsonTypeKey].(string); nestedType != "" {
					activity = nestedType
				}
			}
			events = append(events, providerEvent{Kind: eventProgress, Activity: activity})
		}
		return events
	}
	if claudeResultIsErrorV013(value) {
		reason, _ := value[jsonResultKey].(string)
		if reason == "" {
			reason = "Claude reported a terminal failure"
		}
		return append(events, providerEvent{Kind: eventFailure, Reason: reason})
	}
	// Claude reports a run whose tool calls were all denied as a success, with
	// is_error false and a fluent final answer. Treating that as completion
	// hides work that never happened, so a denial fails the dispatch.
	if denied, ok := mapValueV013(value, permissionDenialsKey); ok {
		example := ""
		if name, _ := denied[toolNameKey].(string); name != "" {
			example = fmt.Sprintf(" (for example %s)", name)
		}
		// The remedy is not always a wider allowlist: a deny rule or managed
		// policy denies a call whatever approvals.mode says, and Agent Layer
		// itself denies AskUserQuestion, so the message points at the effective
		// permissions rather than prescribing one fix.
		reason := fmt.Sprintf("Claude denied at least one tool call%s, so the dispatch could not do the requested work. Check the effective Claude permissions before dispatching again: approvals.mode and .agent-layer/commands.allow decide what is allowed, and a deny rule or managed policy overrides both.", example)
		return append(events, providerEvent{Kind: eventFailure, Reason: reason})
	}
	id, _ := firstStringV013(value, "session_id", "sessionId")
	if id == "" || id != expected {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Claude terminal result did not return the caller-assigned session ID"})
	}
	answer, _ := value[jsonResultKey].(string)
	if answer == "" {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Claude terminal result did not contain a final answer"})
	}
	events = append(events, providerEvent{Kind: eventSession, SessionID: id}, providerEvent{Kind: eventAnswer, Answer: answer}, providerEvent{Kind: eventComplete})
	return events
}

func reduceCodexEvent(value map[string]any) []providerEvent {
	eventType, _ := value[jsonTypeKey].(string)
	switch eventType {
	case "thread.started":
		id, _ := firstStringV013(value, "thread_id", "threadId", "id")
		if id == "" {
			// Ignore a duplicate/incomplete lifecycle notification. The shared
			// completion invariant still rejects the run unless a separate
			// thread.started event supplied an exact thread ID.
			return nil
		}
		return []providerEvent{{Kind: eventSession, SessionID: id}}
	case "turn.completed":
		return []providerEvent{{Kind: eventComplete}}
	case "turn.failed", "turn.aborted", jsonErrorKey:
		reason, _ := firstStringV013(value, jsonMessageKey, jsonReasonKey, jsonErrorKey)
		if reason == "" {
			if details, ok := mapValueV013(value, jsonErrorKey); ok {
				reason, _ = firstStringV013(details, jsonMessageKey, jsonReasonKey)
			}
		}
		if reason == "" {
			reason = "Codex reported a terminal failure"
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}
	case codexAgentMessageType:
		if answer, ok := firstStringV013(value, jsonMessageKey, jsonTextKey); ok {
			return []providerEvent{{Kind: eventAnswer, Answer: answer}}
		}
	case codexItemCompletedType:
		if item, ok := mapValueV013(value, "item"); ok {
			if itemType, _ := item[jsonTypeKey].(string); itemType == codexAgentMessageType {
				if answer, found := firstStringV013(item, jsonMessageKey, jsonTextKey); found {
					return []providerEvent{{Kind: eventAnswer, Answer: answer}}
				}
			}
		}
	}
	if eventType != "" {
		return []providerEvent{{Kind: eventProgress, Activity: eventType}}
	}
	return nil
}

func appendRetainedGrokText(dst *strings.Builder, chunk string) {
	if dst == nil || chunk == "" || dst.Len() >= maxRetainedAnswerBytes {
		return
	}
	remain := maxRetainedAnswerBytes - dst.Len()
	if len(chunk) > remain {
		chunk = chunk[:remain]
	}
	dst.WriteString(chunk)
}

func reduceGrokEvent(expected string, value map[string]any, textAccumulator *strings.Builder, terminalSeen *bool) []providerEvent {
	eventType, _ := value[jsonTypeKey].(string)
	switch eventType {
	case thoughtActivity:
		return []providerEvent{{Kind: eventProgress, Activity: thoughtActivity}}
	case "text":
		chunk, _ := value["data"].(string)
		if chunk != "" && textAccumulator != nil {
			appendRetainedGrokText(textAccumulator, chunk)
		}
		return []providerEvent{{Kind: eventProgress, Activity: textDeltaActivity}}
	case grokUsageEventType:
		// The selective reader retains usage and the raw provider.stdout capture
		// keeps the complete record for benchmark attribution.  It is not a UI
		// lifecycle event, so ordinary dispatch output remains unchanged.
		return nil
	case grokToolUpdateType:
		if status, _ := value["status"].(string); status == grokToolFailedStatus {
			if content, ok := mapValueV013(value, "content"); ok {
				if nested, found := mapValueV013(content, "content"); found {
					if message, _ := nested[jsonTextKey].(string); strings.HasPrefix(message, grokToolDeniedPrefix) && strings.Contains(message, grokPermissionDenied) {
						return []providerEvent{{Kind: eventFailure, Reason: "Grok denied a tool call, so the dispatch could not do the requested work. Check the effective Grok permissions before dispatching again: approvals.mode and .agent-layer/commands.allow decide what is allowed, and a deny rule or managed policy overrides both. Provider detail: " + message}}
					}
				}
			}
		}
		return []providerEvent{{Kind: eventProgress, Activity: grokToolUpdateType}}
	case "end":
		if terminalSeen != nil && *terminalSeen {
			return []providerEvent{{Kind: eventFailure, Reason: "Grok stream has multiple terminal end events"}}
		}
		if terminalSeen != nil {
			*terminalSeen = true
		}
		id, _ := firstStringV013(value, "session_id", "sessionId")
		if id == "" || id != expected {
			return []providerEvent{{Kind: eventFailure, Reason: "Grok terminal result did not return the caller-assigned session ID"}}
		}
		stopReason, _ := firstStringV013(value, "stopReason", "stop_reason")
		if !grok.IsSuccessfulStopReason(stopReason) {
			return []providerEvent{{Kind: eventFailure, Reason: fmt.Sprintf("Grok terminated with abnormal stop reason %q", stopReason)}}
		}
		var answer string
		if textAccumulator != nil {
			answer = textAccumulator.String()
			if textAccumulator.Len() >= maxRetainedAnswerBytes {
				answer += truncatedAnswerNotice
			}
		}
		if answer == "" {
			return []providerEvent{{Kind: eventFailure, Reason: "Grok terminal result did not contain a final answer"}}
		}
		return []providerEvent{
			{Kind: eventSession, SessionID: id},
			{Kind: eventAnswer, Answer: answer},
			{Kind: eventComplete},
		}
	case "error":
		reason, _ := firstStringV013(value, "message", "error", "reason")
		if reason == "" {
			reason = "Grok reported a terminal failure"
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}
	}
	if eventType != "" {
		return []providerEvent{{Kind: eventProgress, Activity: eventType}}
	}
	return nil
}

func reduceAntigravityEvent(value map[string]any, terminalSeen *bool) []providerEvent {
	eventType, _ := value[jsonEventKey].(string)
	if eventType == "init" {
		id, _ := firstStringV013(value, "conversation_id")
		if id == "" {
			return []providerEvent{{Kind: eventFailure, Reason: "Antigravity init event has no conversation ID"}}
		}
		return []providerEvent{{Kind: eventSession, SessionID: id}}
	}
	if eventType != "result" {
		if eventType == "" {
			return nil
		}
		return []providerEvent{{Kind: eventProgress, Activity: eventType}}
	}
	if terminalSeen != nil && *terminalSeen {
		return []providerEvent{{Kind: eventFailure, Reason: "Antigravity stream has multiple terminal result events"}}
	}
	if terminalSeen != nil {
		*terminalSeen = true
	}
	result, _ := value[jsonResultKey].(map[string]any)
	if result == nil {
		return []providerEvent{{Kind: eventFailure, Reason: "Antigravity terminal result is not an object"}}
	}
	id, _ := firstStringV013(result, "conversation_id")
	var events []providerEvent
	if id != "" {
		events = append(events, providerEvent{Kind: eventSession, SessionID: id})
	}
	status, _ := result[jsonStatusKey].(string)
	if status != "SUCCESS" {
		reason := "Antigravity terminal result was unsuccessful"
		if providerError, _ := firstStringV013(result, "error"); providerError != "" {
			reason += ": " + providerError
		}
		return append(events, providerEvent{Kind: eventFailure, Reason: reason})
	}
	answer, _ := firstStringV013(result, jsonResponseKey)
	if id == "" || answer == "" {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Antigravity terminal result has no conversation ID or final answer"})
	}
	usage, _ := result[grokUsageEventType].(map[string]any)
	if usage == nil {
		return append(events, providerEvent{Kind: eventFailure, Reason: "Antigravity terminal result has no usage object"})
	}
	return append(events, providerEvent{Kind: eventAnswer, Answer: answer}, providerEvent{Kind: eventProgress, Activity: "usage", Usage: usage}, providerEvent{Kind: eventComplete})
}

func readStructuredEventsWithLineage(reader io.Reader, rawWriter io.Writer, agent string, expectedSession string, claudeLineage bool, consume func(providerEvent) error, consumeLineage func(claudeLineageEvidence) error) error {
	source := bufio.NewReaderSize(io.TeeReader(reader, rawWriter), structuredJSONBufferBytes)
	parser := newSelectiveJSONReader()
	normalizer := claudeLineageNormalizer{ignoredTasks: make(map[string]struct{})}
	var grokAccumulator strings.Builder
	var grokTerminalSeen, antigravityTerminalSeen bool
	emitInvalid := func(reason string) error {
		if !claudeLineage || consumeLineage == nil {
			return nil
		}
		return consumeLineage(claudeLineageEvidence{Kind: lineageKindInvalid, Reason: reason})
	}
	for {
		line := &structuredJSONLineReader{source: source}
		parser.reset(line)
		record, err := parser.next()
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
			if invalidErr := emitInvalid(lineageReasonEvidenceMalformed); invalidErr != nil {
				return invalidErr
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
			if invalidErr := emitInvalid(lineageReasonEvidenceMalformed); invalidErr != nil {
				return invalidErr
			}
			continue
		}
		var events []providerEvent
		var lineageEvents []claudeLineageEvidence
		switch agent {
		case AgentClaude:
			if claudeLineage && consumeLineage != nil {
				if record.Claude.InvalidReason != "" {
					lineageEvents = []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: record.Claude.InvalidReason}}
				} else {
					lineageEvents = normalizer.reduce(record)
				}
			}
			events = reduceClaudeEvent(expectedSession, record.Fields)
		case AgentCodex:
			events = reduceCodexEvent(record.Fields)
		case AgentGrok:
			events = reduceGrokEvent(expectedSession, record.Fields, &grokAccumulator, &grokTerminalSeen)
		case AgentAntigravity:
			events = reduceAntigravityEvent(record.Fields, &antigravityTerminalSeen)
		default:
			return fmt.Errorf("unsupported structured dispatch provider %q", agent)
		}
		for _, event := range events {
			if err := consume(event); err != nil {
				return err
			}
		}
		for _, lineageEvent := range lineageEvents {
			if err := consumeLineage(lineageEvent); err != nil {
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
// line. Conversation markers are short; oversized diagnostics are reduced to
// their prefix without constraining the provider log itself.
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
				line = append(line, fragment[:remaining]...)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if truncated {
			return string(line), err
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

func dispatchEnvironment(base []string, project *config.ProjectConfig, dispatchRun *dispatchRun, depth int) []string {
	info := &run.Info{ID: dispatchRun.Record.ID, Dir: dispatchRun.Dir}
	env := clients.BuildEnv(base, project.Env, info)
	env = clients.SetEnv(env, clients.EnvDispatchActive, fmt.Sprintf("%d", depth))
	return clients.SetEnv(env, updatewarn.EnvSuppress, "1")
}
