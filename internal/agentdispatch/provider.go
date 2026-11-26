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
	"github.com/conn-castle/agent-layer/internal/clients/muse"
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
	// claudePrintBackgroundWaitCeilingEnv removes headless Claude's wait limit
	// for background subagents and workflows. Bash has a separate exit grace;
	// interactive Claude launches do not use this override.
	claudePrintBackgroundWaitCeilingEnv   = "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"
	claudePrintBackgroundWaitCeilingValue = "0"
	truncatedAnswerNotice                 = "\n\n[Agent Layer truncated this final answer after retaining 256 MiB. Resume the conversation and ask the agent to summarize the final answer in another turn.]\n"
	// Keep grokEventDataTruncatedNotice and grokToolUpdateTruncatedNotice for
	// selectiveJSONReader bounded field truncation compatibility.
	grokEventDataTruncatedNotice    = "\n\n[Agent Layer truncated this Grok text event after retaining 64 KiB.]\n"
	claudePermissionModeAcceptEdits = "acceptEdits"
	claudePermissionModeDontAsk     = "dontAsk"
	claudePermissionsPassthroughKey = "permissions"
	claudeDefaultModePassthroughKey = "defaultMode"
	permissionDenialsKey            = "permission_denials"
	toolNameKey                     = "tool_name"
	textDeltaActivity               = "text_delta"
	thoughtActivity                 = "thought"
	grokToolUpdateType              = "tool_call_update"
	grokUsageEventType              = "usage"
	grokToolUpdateTruncatedNotice   = "\n\n[Agent Layer truncated this Grok tool update after retaining 512 bytes.]\n"
	antigravityEffortLow            = "low"
	antigravityEffortMedium         = "medium"
	antigravityEffortHigh           = "high"
	execSubcommand                  = "exec"
	promptFileFlag                  = "--prompt-file"
	jsonRPCKey                      = "jsonrpc"
	jsonRPCVersion                  = "2.0"
	jsonMethodKey                   = "method"
	jsonParamsKey                   = "params"
	jsonSessionIDCamelKey           = "sessionId"
	grokSandboxEnv                  = "GROK_SANDBOX"
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

type providerCommand struct {
	Path                 string
	Args                 []string
	Env                  []string
	WorkDir              string
	SessionID            string
	LogPath              string
	Provider             string
	RunMode              string
	Model                string
	Effort               string
	ClaudeLineage        bool
	ObserveMuseApprovals bool
	MuseBaseline         *museRecoveryBaseline
	SessionMCPServers    []string
	UseACP               bool
	ACP                  *acpLaunchConfig
}

type providerEvent struct {
	Kind            string
	ReceivedAt      time.Time `json:",omitempty"`
	RootID          string    `json:",omitempty"`
	CandidateID     string    `json:",omitempty"`
	BackgroundID    string    `json:",omitempty"`
	Cursor          string    `json:",omitempty"`
	Sequence        string    `json:",omitempty"`
	CompletionScope string    `json:",omitempty"`
	LineageSource   string    `json:",omitempty"`
	SessionID       string
	Answer          string
	Activity        string
	Reason          string
	// Usage is retained in provider.stdout for benchmark attribution.  The
	// reducer keeps it visible to structured consumers rather than discarding
	// the only request-level billing evidence.
	Recovery  *museRecoveryBaseline      `json:",omitempty"`
	Lifecycle *providerLifecycleSnapshot `json:",omitempty"`
	Usage     map[string]any
}

const (
	eventSession      = "session"
	eventAnswer       = "answer"
	eventProgress     = "progress"
	eventComplete     = "complete"
	eventFailure      = "failure"
	eventContinuation = "continuation"
	museRootLinked    = "session.run.linked"
	museSessionRead   = "session/read"

	museRecoveredRoot = "session/read_after_launch"
	museRunStreamKind = "run"

	codexAgentMessageType  = "agent_message"
	codexItemCompletedType = "item.completed"
	invalidStructuredEvent = "invalid_structured_event"
)

var supportedProviderVersions = map[string]string{
	AgentClaude:      claudeTestedVersion,
	AgentCodex:       "0.144.1",
	AgentAntigravity: "1.5.1",
	AgentGrok:        grok.SupportedVersion,
	AgentMuse:        muse.SupportedVersion,
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
	return providerVersionWithContext(context.Background(), path, agent)
}

func providerVersionWithContext(parent context.Context, path string, agent string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, providerVersionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version") // #nosec G204 -- path is resolved from the static provider registry.
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("read %s version: %w", agent, ctx.Err())
	}
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
	return buildProviderCommandForSession(target, project, "", env, prompt, model, effort, targetPinned, mode, sessionID, nil, run, diagnostics)
}

func buildProviderCommandForSession(
	target targetMeta,
	project *config.ProjectConfig,
	workDir string,
	env []string,
	prompt []byte,
	model string,
	effort string,
	targetPinned bool,
	mode string,
	sessionID string,
	sessionMCP []SessionMCPServer,
	run *dispatchRun,
	diagnostics io.Writer,
) (providerCommand, error) {
	if project == nil || run == nil {
		return providerCommand{}, exitError(ExitConfig, "build dispatch provider command without project or run")
	}
	if len(prompt) > MaxStdinPromptBytes {
		return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("dispatch prompt is %d bytes; the maximum is %d bytes", len(prompt), MaxStdinPromptBytes))
	}
	if err := validateSessionMCPServersForProject(project, target, sessionMCP); err != nil {
		return providerCommand{}, err
	}
	effectiveWorkDir := workDir
	if effectiveWorkDir == "" {
		effectiveWorkDir = project.Root
	}
	command := providerCommand{Path: target.Binary, Env: env, Provider: target.Name, RunMode: mode, SessionMCPServers: sessionMCPServerNames(sessionMCP)}
	switch target.Name {
	case AgentClaude:
		return buildClaudeCommand(command, project, effectiveWorkDir, model, effort, targetPinned, mode, sessionID, sessionMCP, run, diagnostics)
	case AgentCodex:
		return buildCodexCommand(command, project, effectiveWorkDir, model, effort, targetPinned, mode, sessionID, sessionMCP, run, diagnostics)
	case AgentAntigravity:
		return buildAntigravityCommand(command, target, project, effectiveWorkDir, model, effort, sessionID, sessionMCP, run)
	case AgentGrok:
		return buildGrokCommand(command, target, project, effectiveWorkDir, model, effort, targetPinned, sessionID, sessionMCP, run, diagnostics)
	case AgentMuse:
		return buildMuseCommand(command, project, effectiveWorkDir, prompt, model, effort, targetPinned, mode, sessionID, sessionMCP, run)
	default:
		return providerCommand{}, exitError(ExitUsage, fmt.Sprintf("unsupported dispatch provider %q", target.Name))
	}
}

func buildClaudeCommand(command providerCommand, project *config.ProjectConfig, workDir string, model, effort string, targetPinned bool, mode, sessionID string, sessionMCP []SessionMCPServer, run *dispatchRun, diagnostics io.Writer) (providerCommand, error) {
	if err := requireFreshSessionID("Claude", mode, sessionID); err != nil {
		return providerCommand{}, err
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
	resolvedModel := resolveSetting(model, project.Config.Agents.Claude.Model, !targetPinned)
	args = appendFlag(args, "--model", resolvedModel)
	resolvedEffort := resolveSetting(effort, project.Config.Agents.Claude.ReasoningEffort, !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Claude.AgentSpecific, "effortLevel"))
	args = appendFlag(args, "--effort", resolvedEffort)
	command.Model = resolvedModel
	command.Effort = resolvedEffort
	if len(sessionMCP) > 0 {
		staged, err := stageSessionMCPSpec(run.Dir, workDir, command.Env, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
		mcpConfigPath, err := stageClaudeSessionMCPConfig(run.Dir, staged, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
		args = append(args, "--mcp-config", mcpConfigPath)
	}
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
		seenRule := make(map[string]bool)
		for _, rule := range projection.ClaudeAllowRules(
			project.Config,
			project.CommandsAllow,
			projection.EffectiveServerIDs(project.Config, projection.ClientClaude),
		) {
			seenRule[rule] = true
			args = append(args, "--allowedTools", rule)
		}
		for _, rule := range projection.MCPRules(sessionMCPServerNames(sessionMCP), projection.ClaudeMCPRule) {
			if seenRule[rule] {
				continue
			}
			seenRule[rule] = true
			args = append(args, "--allowedTools", rule)
		}
	}
	command.Args = args
	command.Env = claude.ConfigureEnvironment(project.Root, command.Env, project.Config.Agents.Claude, diagnostics)
	command.Env = clients.UnsetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv)
	command.Env = clients.SetEnv(command.Env, claudePrintBackgroundWaitCeilingEnv, claudePrintBackgroundWaitCeilingValue)
	command.SessionID = sessionID
	command.ClaudeLineage = lineage
	return command, nil
}

func buildCodexCommand(command providerCommand, project *config.ProjectConfig, workDir string, model, effort string, targetPinned bool, mode, sessionID string, sessionMCP []SessionMCPServer, run *dispatchRun, diagnostics io.Writer) (providerCommand, error) {
	args := []string{execSubcommand}
	if mode == dispatchModeResume {
		args = append(args, "resume")
	}
	args = append(args, "--json")
	if mode == dispatchModeResume {
		args = append(args, sessionID)
	}
	resolvedModel := resolveSetting(model, project.Config.Agents.Codex.Model, !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexModelKey))
	args = appendFlag(args, "--model", resolvedModel)
	resolvedEffort := resolveSetting(effort, project.Config.Agents.Codex.ReasoningEffort, !targetPinned && !config.HasProviderPassthroughKey(project.Config.Agents.Codex.AgentSpecific, config.CodexReasoningEffortKey))
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
	if len(sessionMCP) > 0 {
		staged, err := stageSessionMCPSpec(run.Dir, workDir, command.Env, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
		for _, server := range normalizeSessionMCPServers(sessionMCP) {
			args = append(args, "-c", codexSessionMCPConfigArg(run.Dir, staged, server.Name))
		}
	}
	args = append(args, "-")
	command.Args = args
	command.Env = codex.ConfigureEnvironment(project.Root, command.Env, project.Config.Agents.Codex, diagnostics)
	if err := codex.EnsureSelectedHome(project.Root, command.Env, project.Config.Agents.Codex); err != nil {
		return providerCommand{}, &ExitError{Code: ExitConfig, Err: err}
	}
	command.SessionID = sessionID
	return command, nil
}

func buildAntigravityCommand(command providerCommand, target targetMeta, project *config.ProjectConfig, workDir string, model, effort, sessionID string, sessionMCP []SessionMCPServer, run *dispatchRun) (providerCommand, error) {
	baseArgs, err := antigravity.BaseArgs(project.Root, project.Config)
	if err != nil {
		return providerCommand{}, wrapExitError(ExitConfig, "prepare Antigravity launch", err)
	}
	args := append([]string{"acp", "--stdio"}, baseArgs...)
	if project.Config.Approvals.Mode != config.ApprovalModeYOLO {
		args = append(args, "--sandbox")
	}
	resolvedModel := resolveSetting(model, project.Config.Agents.Antigravity.Model, true)
	command.Model = resolvedModel
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
	var staged stagedSessionMCP
	if len(sessionMCP) > 0 {
		staged, err = stageSessionMCPSpec(run.Dir, workDir, command.Env, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
	}
	command.Args = args
	command.Env = antigravity.ConfigureEnvironment(command.Env)
	command.SessionID = sessionID
	command.UseACP = true
	command.ACP = buildACPLaunchConfig(target, project, workDir, staged, sessionMCP, run.Dir)
	return command, nil
}

func buildGrokCommand(command providerCommand, target targetMeta, project *config.ProjectConfig, workDir string, model, effort string, targetPinned bool, sessionID string, sessionMCP []SessionMCPServer, run *dispatchRun, diagnostics io.Writer) (providerCommand, error) {
	args := []string{"--no-auto-update"}
	if config.GrokDisableMemory(project.Config.Agents.Grok) {
		args = append(args, "--no-memory")
	}
	if project.Config.Approvals.Mode != config.ApprovalModeYOLO {
		// Pin default permission mode so user or project bypassPermissions /
		// broad allow settings cannot bypass ACP session/request_permission.
		args = append(args, "--permission-mode", "default")
	}
	args = append(args, "agent", "--no-leader")
	resolvedModel := resolveSetting(model, project.Config.Agents.Grok.Model, !targetPinned)
	args = appendFlag(args, "--model", resolvedModel)
	resolvedEffort := resolveSetting(effort, project.Config.Agents.Grok.ReasoningEffort, !targetPinned)
	args = appendFlag(args, "--reasoning-effort", resolvedEffort)
	command.Model = resolvedModel
	command.Effort = resolvedEffort
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--always-approve")
	}
	args = append(args, "stdio")
	if err := grok.EnsureHome(project.Root); err != nil {
		return providerCommand{}, wrapExitError(ExitConfig, "prepare Grok home", err)
	}
	env := grok.ConfigureEnvironment(project.Root, command.Env, project.Config.Agents.Grok, diagnostics)
	switch {
	case project.Config.Approvals.Mode == config.ApprovalModeYOLO:
		env = clients.UnsetEnv(env, grokSandboxEnv)
	case projection.BuildApprovals(project.Config, project.CommandsAllow).AllowCommands:
		env = clients.SetEnv(env, grokSandboxEnv, grok.SandboxWorkspace)
	default:
		env = clients.SetEnv(env, grokSandboxEnv, grok.SandboxReadOnly)
	}
	var staged stagedSessionMCP
	if len(sessionMCP) > 0 {
		var err error
		staged, err = stageSessionMCPSpec(run.Dir, workDir, command.Env, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
	}
	command.Args = args
	command.Env = env
	command.SessionID = sessionID
	command.UseACP = true
	command.ACP = buildACPLaunchConfig(target, project, workDir, staged, sessionMCP, run.Dir)
	return command, nil
}

func buildMuseCommand(command providerCommand, project *config.ProjectConfig, workDir string, prompt []byte, model, effort string, targetPinned bool, mode, sessionID string, sessionMCP []SessionMCPServer, run *dispatchRun) (providerCommand, error) {
	if err := requireFreshSessionID("Muse", mode, sessionID); err != nil {
		return providerCommand{}, err
	}
	promptPath, err := writeDispatchPromptFile(run, prompt, "Muse")
	if err != nil {
		return providerCommand{}, err
	}
	args := []string{execSubcommand, "--json", promptFileFlag, promptPath, "--workspace", project.Root, "--trust-workspace", "--session-id", sessionID, "--user-input-auto-resolve"}
	resolvedModel := resolveSetting(model, project.Config.Agents.Muse.Model, !targetPinned)
	args = appendFlag(args, "--model", resolvedModel)
	resolvedEffort := resolveSetting(effort, project.Config.Agents.Muse.ReasoningEffort, !targetPinned)
	args = appendFlag(args, "--reasoning-effort", resolvedEffort)
	if project.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--yolo")
	} else {
		args = append(args, "--approval-mode", "untrusted", "--approval-judge", "off")
		command.ObserveMuseApprovals = true
	}
	if len(sessionMCP) > 0 {
		staged, err := stageSessionMCPSpec(run.Dir, workDir, command.Env, sessionMCP)
		if err != nil {
			return providerCommand{}, err
		}
		for _, server := range normalizeSessionMCPServers(sessionMCP) {
			cfgArg, err := museSessionMCPConfigArg(run.Dir, staged, server.Name)
			if err != nil {
				return providerCommand{}, err
			}
			args = append(args, "-c", cfgArg)
		}
	}
	command.Args = args
	command.SessionID = sessionID
	command.Model = resolvedModel
	command.Effort = resolvedEffort
	return command, nil
}

func resolveSetting(explicit, configured string, inherit bool) string {
	value := strings.TrimSpace(explicit)
	if value == "" && inherit {
		value = strings.TrimSpace(configured)
	}
	return value
}

func appendFlag(args []string, flag, value string) []string {
	if value != "" {
		args = append(args, flag, value)
	}
	return args
}

func requireFreshSessionID(provider string, mode, sessionID string) error {
	if mode == dispatchModeFresh && sessionID == "" {
		return exitError(ExitConfig, "new "+provider+" dispatch requires a caller-assigned session ID")
	}
	return nil
}

func writeDispatchPromptFile(run *dispatchRun, prompt []byte, provider string) (string, error) {
	path := filepath.Join(run.Dir, "prompt.txt")
	if err := os.WriteFile(path, prompt, 0o600); err != nil {
		return "", wrapExitError(ExitConfig, "write "+provider+" dispatch prompt", err)
	}
	return path, nil
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
	id, _ := firstStringV013(value, "session_id", jsonSessionIDCamelKey)
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

// claudeTaskTracker withholds completion from a successful Claude result while
// a Claude-managed task is still running, so an earlier result must not start
// the shutdown grace that would kill the work. Claude can stop background Bash
// at exit even with claudePrintBackgroundWaitCeilingEnv set to zero; an earlier
// answer cannot stand as success if a task then stops or fails without a reply.
type claudeTaskTracker struct {
	outstanding        map[string]struct{}
	completionPending  bool
	pendingTaskFailure string
}

func (t *claudeTaskTracker) reduce(record structuredRecord, events []providerEvent) []providerEvent {
	eventType, _ := record.Fields[jsonTypeKey].(string)
	if eventType == claudeSystemEventType && record.Claude.TaskID.Valid && strings.TrimSpace(record.Claude.TaskID.Value) != "" {
		switch subtype, _ := record.Fields["subtype"].(string); subtype {
		case "task_started":
			t.outstanding[record.Claude.TaskID.Value] = struct{}{}
		case "task_notification":
			t.finishTask(record.Claude.TaskID.Value, record.Claude.Status.Value)
		case "task_updated":
			if patch, ok := mapValueV013(record.Fields, "patch"); ok {
				status, _ := patch[jsonStatusKey].(string)
				switch status {
				case dispatchStateCompleted, dispatchStateFailed, claudeTaskStatusStopped, "killed":
					if status == "killed" {
						status = claudeTaskStatusStopped
					}
					t.finishTask(record.Claude.TaskID.Value, status)
				}
			}
		}
		return events
	}
	if len(events) == 0 || events[len(events)-1].Kind != eventComplete {
		return events
	}
	// A later validated result can acknowledge the failure or report recovery.
	t.pendingTaskFailure = ""
	t.completionPending = len(t.outstanding) > 0
	if t.completionPending {
		events[len(events)-1] = providerEvent{Kind: eventProgress, Activity: "claude_waiting_for_background_tasks"}
	}
	return events
}

func (t *claudeTaskTracker) finishTask(id, status string) {
	_, tracked := t.outstanding[id]
	if tracked && t.completionPending && (status == dispatchStateFailed || status == claudeTaskStatusStopped) {
		t.pendingTaskFailure = status
	}
	delete(t.outstanding, id)
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
	case "turn.failed", "turn.aborted":
		reason := codexEventReason(value)
		if reason == "" {
			reason = "Codex reported a terminal failure"
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}
	case jsonErrorKey:
		// Codex also emits error events for stream retries it recovers from
		// ("Reconnecting... 2/5 (request timed out)"). A fatal error still ends
		// the turn with turn.failed or a nonzero exit, so the error is kept as a
		// diagnostic for those failure paths instead of ending the dispatch.
		return []providerEvent{{Kind: eventProgress, Activity: eventType, Reason: codexEventReason(value)}}
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

func codexEventReason(value map[string]any) string {
	reason, _ := firstStringV013(value, jsonMessageKey, jsonReasonKey, jsonErrorKey)
	if reason == "" {
		if details, ok := mapValueV013(value, jsonErrorKey); ok {
			reason, _ = firstStringV013(details, jsonMessageKey, jsonReasonKey)
		}
	}
	return reason
}

type museReducer struct {
	expectedSession    string
	rootRunID          string
	terminalSeen       bool
	awaitingBackground bool
	backgroundRunID    string
	recovery           *museRecoveryBaseline
	lineageSource      string
	candidateRunID     string
}

func (m *museReducer) reduce(value map[string]any) []providerEvent {
	if fmt.Sprint(value["schema_version"]) != "1" {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse event has unsupported or missing schema_version"}}
	}
	stream, _ := mapValueV013(value, "stream")
	streamKind, _ := stream[jsonKindKey].(string)
	streamID, _ := stream["id"].(string)
	if streamKind == eventSession && streamID != "" && streamID != m.expectedSession {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse event returned a different provider session ID"}}
	}
	payloadType, _ := value["payload_type"].(string)
	payload, _ := mapValueV013(value, "payload")
	commandID, _ := payload["command_id"].(string)
	runStream, _ := mapValueV013(payload, "run_stream")
	runKind, _ := runStream[jsonKindKey].(string)
	runID, _ := runStream["id"].(string)
	// A text-bearing terminal closes a provider turn, but an authenticated
	// background command can still continue the workflow in this invocation.
	if payloadType == "runtime.command.accepted" && m.terminalSeen &&
		streamKind == eventSession && streamID == m.expectedSession &&
		payload["client_id"] == "muse-runtime-background-terminal" && payload["command_kind"] == "turn.submit" {
		if commandID == "" || commandID == m.rootRunID || (m.backgroundRunID != "" && m.backgroundRunID != commandID) {
			return []providerEvent{{Kind: eventFailure, Reason: "Muse background command has invalid or conflicting root identity"}}
		}
		m.backgroundRunID = commandID
		m.awaitingBackground = true
		return []providerEvent{{Kind: eventContinuation, Activity: "muse_background_announced"}}
	}
	if (payloadType == museRootLinked || strings.HasPrefix(payloadType, "run.terminal.")) && (streamKind != eventSession || streamID != m.expectedSession) {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse root event omitted the expected session stream"}}
	}
	if payloadType == museRootLinked {
		if runKind != museRunStreamKind || runID == "" || commandID == "" || runID != commandID {
			return []providerEvent{{Kind: eventFailure, Reason: "Muse root run linkage is invalid"}}
		}
		if m.rootRunID != "" && m.rootRunID != runID {
			if !m.terminalSeen || !m.awaitingBackground || runID != m.backgroundRunID {
				return []providerEvent{{Kind: eventFailure, Reason: "Muse stream linked multiple root runs without a background continuation"}}
			}
			m.terminalSeen = false
			m.awaitingBackground = false
			m.backgroundRunID = ""
		}
		m.rootRunID = runID
		m.lineageSource = museRootLinked
		return []providerEvent{{Kind: eventSession, SessionID: streamID}, {Kind: eventProgress, Activity: payloadType}}
	}
	if payloadType == "task.stream.linked" && m.rootRunID == "" && m.candidateRunID == "" && streamKind == eventSession && streamID == m.expectedSession && commandID != "" && commandID == runID && runKind == museRunStreamKind {
		m.candidateRunID = runID
	}
	if !strings.HasPrefix(payloadType, "run.terminal.") {
		if payloadType != "" {
			return []providerEvent{{Kind: eventProgress, Activity: payloadType}}
		}
		return nil
	}
	// Resume stdout may begin after the root link. Only an active/interrupted
	// turn read from the provider's durable session before launch can seed it.
	if m.rootRunID == "" && m.recovery != nil && m.recovery.verifyRoot != nil && runID != "" && runID == commandID && runKind == museRunStreamKind && runID == m.candidateRunID && runID != m.recovery.RootID {
		observed, err := m.recovery.verifyRoot(runID)
		if err != nil {
			return []providerEvent{{Kind: eventFailure, Reason: "Muse resumed terminal has no authoritative root/cursor evidence: " + singleLineMuseText(err.Error())}}
		}
		m.recovery = observed
	}
	if m.rootRunID == "" && m.recovery != nil && m.recovery.RootID != "" {
		m.rootRunID = m.recovery.RootID
		m.lineageSource = museSessionRead
		if m.recovery.Source != "" {
			m.lineageSource = m.recovery.Source
		}
	}
	if m.terminalSeen {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse stream has multiple root terminal events"}}
	}
	if m.rootRunID == "" || runKind != museRunStreamKind || runID != m.rootRunID || commandID != m.rootRunID {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse terminal event does not belong to the linked root run"}}
	}
	m.terminalSeen = true
	terminal, _ := payload["terminal"].(string)
	if m.lineageSource == museRecoveredRoot && terminal != m.recovery.TerminalState {
		return []providerEvent{{Kind: eventFailure, Reason: "Muse terminal conflicts with authoritative recovered state"}}
	}
	if payloadType != "run.terminal.completed" || terminal != "completed" {
		reason, _ := payload[jsonReasonKey].(string)
		if reason == "" {
			reason = "Muse reported terminal state " + terminal
		}
		return []providerEvent{{Kind: eventFailure, Reason: reason}}
	}
	answer, _ := payload[jsonTextKey].(string)
	if answer == "" {
		// This is turn completion, not invocation completion. Do not trigger
		// termination or the provider shutdown deadline while its workflow runs.
		m.awaitingBackground = true
		return []providerEvent{{Kind: eventProgress, Activity: "muse_waiting_for_background_answer"}}
	}
	events := []providerEvent{{Kind: eventAnswer, Answer: answer}, {Kind: eventComplete}}
	if m.recovery != nil && m.lineageSource != museRootLinked {
		events = append([]providerEvent{{Kind: eventSession, SessionID: m.expectedSession}}, events...)
	}
	return events
}

// claudeSessionMCPTracker enforces no silent degradation for Claude
// session-scoped MCP servers. Claude Code continues running even when an
// --mcp-config server fails to start; this tracker inspects system/init and
// immediately fails the invocation if any required session server is missing or
// not connected, or if Claude finishes without confirming startup.
type claudeSessionMCPTracker struct {
	expected  []string
	confirmed bool
}

func (t *claudeSessionMCPTracker) reduce(record structuredRecord, events []providerEvent) []providerEvent {
	if len(t.expected) == 0 {
		return events
	}
	eventType, _ := record.Fields[jsonTypeKey].(string)
	subtype, _ := record.Fields["subtype"].(string)
	if eventType == claudeSystemEventType && subtype == "init" {
		statuses := make(map[string]string, len(record.Claude.MCPServers))
		for _, srv := range record.Claude.MCPServers {
			statuses[srv.Name] = srv.Status
		}
		for _, name := range t.expected {
			status, ok := statuses[name]
			if !ok {
				return []providerEvent{{
					Kind:   eventFailure,
					Reason: fmt.Sprintf("Claude session MCP server %q was not initialized (missing from Claude init mcp_servers); check the server command and configuration", name),
				}}
			}
			if status != "connected" {
				return []providerEvent{{
					Kind:   eventFailure,
					Reason: fmt.Sprintf("Claude session MCP server %q failed to start (status: %q); check the server command, arguments, and environment", name, status),
				}}
			}
		}
		t.confirmed = true
		return events
	}
	if !t.confirmed {
		for _, event := range events {
			if event.Kind == eventComplete || event.Kind == eventAnswer {
				return []providerEvent{{
					Kind:   eventFailure,
					Reason: fmt.Sprintf("Claude produced output without confirming startup of required session MCP servers (%s)", strings.Join(t.expected, ", ")),
				}}
			}
		}
	}
	return events
}

func readStructuredEventsWithLineage(reader io.Reader, rawWriter io.Writer, agent string, expectedSession string, claudeLineage bool, consume func(providerEvent) error, consumeLineage func(claudeLineageEvidence) error, recovery ...*museRecoveryBaseline) error {
	return readStructuredEventsWithSessionMCP(reader, rawWriter, agent, expectedSession, claudeLineage, nil, consume, consumeLineage, recovery...)
}

func readStructuredEventsWithSessionMCP(reader io.Reader, rawWriter io.Writer, agent string, expectedSession string, claudeLineage bool, expectedSessionMCP []string, consume func(providerEvent) error, consumeLineage func(claudeLineageEvidence) error, recovery ...*museRecoveryBaseline) error {
	source := bufio.NewReaderSize(io.TeeReader(reader, rawWriter), structuredJSONBufferBytes)
	parser := newSelectiveJSONReader()
	normalizer := claudeLineageNormalizer{ignoredTasks: make(map[string]struct{})}
	museState := museReducer{expectedSession: expectedSession}
	if len(recovery) > 0 {
		museState.recovery = recovery[0]
		if museState.recovery != nil && museState.recovery.SessionID != expectedSession {
			return fmt.Errorf("muse recovery baseline belongs to another session")
		}
	}
	// All emitted records, including EOF/protocol diagnostics, carry local
	// receipt time and the current reducer ownership state.
	consumeEvent := consume
	consume = func(event providerEvent) error {
		if event.ReceivedAt.IsZero() {
			event.ReceivedAt = time.Now().UTC()
		}
		if agent == AgentMuse {
			if event.RootID == "" {
				event.RootID = museState.rootRunID
			}
			if event.BackgroundID == "" {
				event.BackgroundID = museState.backgroundRunID
			}
			if event.CandidateID == "" {
				event.CandidateID = museState.candidateRunID
			}
			if event.LineageSource == "" {
				event.LineageSource = museState.lineageSource
			}
		}
		return consumeEvent(event)
	}
	claudeTasks := claudeTaskTracker{outstanding: make(map[string]struct{})}
	claudeSessionMCP := claudeSessionMCPTracker{expected: expectedSessionMCP}
	finish := func() error {
		if agent == AgentMuse && museState.awaitingBackground {
			return consume(providerEvent{Kind: eventFailure, Reason: "Muse completed without a final answer"})
		}
		if agent == AgentClaude && claudeTasks.completionPending {
			if claudeTasks.pendingTaskFailure != "" {
				return consume(providerEvent{Kind: eventFailure, Reason: fmt.Sprintf("Claude background task reported %s after the latest successful result; Claude exited without a later result", claudeTasks.pendingTaskFailure)})
			}
			if len(claudeSessionMCP.expected) > 0 && !claudeSessionMCP.confirmed {
				return consume(providerEvent{Kind: eventFailure, Reason: fmt.Sprintf("Claude completed without confirming startup of required session MCP servers (%s)", strings.Join(claudeSessionMCP.expected, ", "))})
			}
			// The stream ended without a later result, so the latest
			// successful result is the final answer.
			return consume(providerEvent{Kind: eventComplete})
		}
		return nil
	}
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
				return finish()
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
				return finish()
			}
			continue
		}
		receivedAt := time.Now().UTC()
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
			events = claudeSessionMCP.reduce(record, claudeTasks.reduce(record, reduceClaudeEvent(expectedSession, record.Fields)))
		case AgentCodex:
			events = reduceCodexEvent(record.Fields)
		case AgentMuse:
			events = museState.reduce(record.Fields)
			for i := range events {
				events[i].RootID = museState.rootRunID
				events[i].BackgroundID = museState.backgroundRunID
				events[i].CandidateID = museState.candidateRunID
				events[i].Cursor, _ = record.Fields["id"].(string)
				if sequence := record.Fields["sequence"]; sequence != nil {
					events[i].Sequence = fmt.Sprint(sequence)
				}
				events[i].LineageSource = museState.lineageSource
				if museState.lineageSource == museRecoveredRoot {
					events[i].Recovery = museState.recovery
				}
				if events[i].Kind == eventComplete {
					events[i].CompletionScope = "provider_turn"
				}
			}
		default:
			return fmt.Errorf("unsupported structured dispatch provider %q", agent)
		}
		for _, event := range events {
			event.ReceivedAt = receivedAt
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
	// A dispatched worker belongs to its own conversation, never the caller's
	// HerdR pane or session MCP wrapper environment. Remove both namespaces
	// before the native child and its hooks can inherit them.
	env = withoutHerdREnvironment(env)
	env = withoutSessionMCPEnvironment(env)
	env = clients.SetEnv(env, clients.EnvDispatchActive, fmt.Sprintf("%d", depth))
	return clients.SetEnv(env, updatewarn.EnvSuppress, "1")
}

func withoutHerdREnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, "HERDR_") {
			result = append(result, entry)
		}
	}
	return result
}
