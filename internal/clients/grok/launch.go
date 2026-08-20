package grok

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/run"
)

const (
	executableName = "grok"
	// SupportedVersion is the Grok CLI version Agent Layer tests against.
	SupportedVersion = "1.0.5"
	// EnvHome is the Grok home directory override.
	EnvHome = "GROK_HOME"
	// EnvMemory is Grok's experimental memory switch.
	EnvMemory = "GROK_MEMORY"
	// SandboxWorkspace allows writes in the project directory.
	SandboxWorkspace = "workspace"
	// SandboxReadOnly allows reads everywhere and writes only to Grok home/temp.
	SandboxReadOnly = "read-only"
	// SuccessfulStopReason is the current terminal reason for a completed turn.
	SuccessfulStopReason = "end_turn"
)

// IsSuccessfulStopReason accepts Grok 1.0.5's documented completed-turn reason.
// Other terminal reasons must fail dispatch/probe.
func IsSuccessfulStopReason(reason string) bool {
	return reason == SuccessfulStopReason
}

// execFunc is overridable for tests; on success it never returns.
var execFunc = clients.ExecHandoff

// HomeDir is the repo-local GROK_HOME Agent Layer always sets.
func HomeDir(root string) string {
	return filepath.Join(root, ".grok-config")
}

// EnsureHome creates or tightens the repo-local GROK_HOME to owner-only
// permissions so the Grok CLI cannot mkdir it with group/other access.
func EnsureHome(root string) error {
	home := HomeDir(root)
	if err := fsutil.EnsurePrivateDir(home); err != nil {
		return fmt.Errorf(messages.SyncGrokHomeEnsureFailedFmt, err)
	}
	return nil
}

// SandboxArgs maps approvals.mode onto Grok --sandbox flags. YOLO leaves
// sandbox off (Grok's default). Command-approved modes use workspace so
// implementation work can write; otherwise the sandbox is read-only.
func SandboxArgs(cfg config.Config, commandsAllow []string) []string {
	if cfg.Approvals.Mode == config.ApprovalModeYOLO {
		return nil
	}
	if projection.BuildApprovals(cfg, commandsAllow).AllowCommands {
		return []string{"--sandbox", SandboxWorkspace}
	}
	return []string{"--sandbox", SandboxReadOnly}
}

// Launch starts the Grok Build CLI with the configured options.
func Launch(cfg *config.ProjectConfig, runInfo *run.Info, env []string, passArgs []string) error {
	args := []string{}
	model := strings.TrimSpace(cfg.Config.Agents.Grok.Model)
	if model != "" {
		args = append(args, "--model", model)
	}

	effort := strings.TrimSpace(cfg.Config.Agents.Grok.ReasoningEffort)
	if effort != "" {
		args = append(args, "--reasoning-effort", effort)
	}

	if config.GrokDisableMemory(cfg.Config.Agents.Grok) {
		args = append(args, "--no-memory")
	}
	if cfg.Config.Approvals.Mode == config.ApprovalModeYOLO {
		args = append(args, "--permission-mode", "bypassPermissions", "--always-approve")
	}
	args = append(args, passArgs...)

	path, err := exec.LookPath(executableName)
	if err != nil {
		return fmt.Errorf(messages.ClientsExecLookupErrorFmt, executableName, err)
	}
	if err := EnsureHome(cfg.Root); err != nil {
		return err
	}
	env = ConfigureEnvironment(cfg.Root, env, cfg.Config.Agents.Grok, os.Stderr)

	argv := append([]string{executableName}, args...)
	if err := execFunc(path, argv, env); err != nil {
		return fmt.Errorf(messages.ClientsExecHandoffErrorFmt, executableName, err)
	}
	return nil
}

// ConfigureEnvironment applies the Grok environment rules shared by
// interactive launching, headless Agent Dispatch, and al vscode.
// Callers that set GROK_HOME must call EnsureHome first so the Grok CLI
// cannot create the isolated home with default 0755 permissions.
func ConfigureEnvironment(root string, env []string, cfg config.GrokConfig, warning io.Writer) []string {
	env = ensureGrokHomeWithWarning(root, env, warning)
	if config.GrokDisableMemory(cfg) {
		env = clients.SetEnv(env, EnvMemory, "0")
	}
	return env
}

// ClearStaleHome removes GROK_HOME only when it points at this repo's
// .grok-config directory.
func ClearStaleHome(root string, env []string) []string {
	expected := HomeDir(root)
	current, ok := clients.GetEnv(env, EnvHome)
	if ok && clients.SamePath(current, expected) {
		return clients.UnsetEnv(env, EnvHome)
	}
	return env
}

func ensureGrokHomeWithWarning(root string, env []string, warning io.Writer) []string {
	expected := HomeDir(root)
	current, ok := clients.GetEnv(env, EnvHome)
	if ok && current != "" && !clients.SamePath(current, expected) && warning != nil {
		_, _ = fmt.Fprintf(warning, messages.ClientsGrokHomeWarningFmt, current, expected)
	}
	return clients.SetEnv(env, EnvHome, expected)
}
