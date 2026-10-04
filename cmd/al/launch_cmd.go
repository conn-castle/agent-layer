package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/clients/claude"
	"github.com/conn-castle/agent-layer/internal/clients/codex"
	"github.com/conn-castle/agent-layer/internal/clients/copilotcli"
	"github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/clients/muse"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// Client names reported in enabled/disabled errors.
const (
	agentClaude      = "claude"
	agentCodex       = "codex"
	agentVSCode      = "vscode"
	agentAntigravity = "antigravity"
	agentCopilot     = "copilot"
	agentGrok        = "grok"
	agentMuse        = "muse"
)

// launchSpec describes one agent launcher subcommand.
type launchSpec struct {
	use, short, long string
	// agent names the client in enabled/disabled errors.
	agent   string
	enabled clients.EnabledSelector
	launch  clients.LaunchFunc
	// noSync accepts --no-sync, for launchers that open VS Code.
	noSync bool
}

// newLaunchCmds builds every agent launcher subcommand.
func newLaunchCmds() []*cobra.Command {
	specs := []launchSpec{
		{
			use: messages.ClaudeUse, short: messages.ClaudeShort, agent: agentClaude,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.Claude.Enabled },
			launch:  claude.Launch,
		},
		{
			use: messages.CodexUse, short: messages.CodexShort, agent: agentCodex,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.Codex.Enabled },
			launch:  codex.Launch,
		},
		{
			use: messages.VSCodeUse, short: messages.VSCodeShort, agent: agentVSCode,
			enabled: func(cfg *config.Config) *bool {
				v := config.IsAgentEnabled(cfg.Agents.VSCode.Enabled) || config.IsAgentEnabled(cfg.Agents.ClaudeVSCode.Enabled)
				return &v
			},
			launch: vscode.Launch,
			noSync: true,
		},
		{
			use: messages.AntigravityUse, short: messages.AntigravityShort, long: messages.AntigravityLong, agent: agentAntigravity,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.Antigravity.Enabled },
			launch:  antigravity.Launch,
		},
		{
			use: messages.CopilotUse, short: messages.CopilotShort, agent: agentCopilot,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.CopilotCLI.Enabled },
			launch:  copilotcli.Launch,
		},
		{
			use: messages.GrokUse, short: messages.GrokShort, agent: agentGrok,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.Grok.Enabled },
			launch:  grok.Launch,
		},
		{
			use: "muse [args...]", short: "Launch Muse Code for this repository", agent: agentMuse,
			enabled: func(cfg *config.Config) *bool { return cfg.Agents.Muse.Enabled },
			launch:  muse.Launch,
			noSync:  true,
		},
	}
	cmds := make([]*cobra.Command, 0, len(specs))
	for _, spec := range specs {
		cmds = append(cmds, newLaunchCmd(spec))
	}
	return cmds
}

// newLaunchCmd builds a launcher that forwards its arguments to the client.
// Flag parsing is disabled so client flags pass through untouched.
func newLaunchCmd(spec launchSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:                spec.use,
		Short:              spec.short,
		Long:               spec.long,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			noSync, quiet, passArgs, err := splitLaunchArgs(args, spec.noSync)
			if err != nil {
				return err
			}
			if noSync {
				return clients.RunNoSyncWithStderr(root, spec.agent, spec.enabled, spec.launch, quiet, passArgs, cmd.ErrOrStderr())
			}
			return clients.RunWithStderr(cmd.Context(), root, spec.agent, spec.enabled, spec.launch, quiet, passArgs, Version, cmd.ErrOrStderr())
		},
	}
	if spec.noSync {
		cmd.Flags().Bool("no-sync", false, "Skip sync before launching")
	}
	return cmd
}

// splitLaunchArgs parses --quiet/-q, and --no-sync when acceptNoSync is set,
// from pass-through args and returns the args to forward to the client.
// Everything after "--" is forwarded unparsed.
func splitLaunchArgs(args []string, acceptNoSync bool) (noSync, quiet bool, passArgs []string, err error) {
	passArgs = []string{}
	for i, arg := range args {
		switch {
		case arg == "--":
			passArgs = append(passArgs, args[i+1:]...)
			return noSync, quiet, passArgs, nil
		case acceptNoSync && arg == noSyncFlag:
			noSync = true
		case acceptNoSync && strings.HasPrefix(arg, noSyncPrefix):
			value := strings.TrimPrefix(arg, noSyncPrefix)
			if noSync, err = strconv.ParseBool(value); err != nil {
				return false, false, nil, fmt.Errorf(messages.NoSyncInvalidFmt, value)
			}
		case arg == flagQuiet || arg == flagQuietShort:
			quiet = true
		case strings.HasPrefix(arg, flagQuietPrefix):
			value := strings.TrimPrefix(arg, flagQuietPrefix)
			if quiet, err = strconv.ParseBool(value); err != nil {
				return false, false, nil, fmt.Errorf(messages.QuietInvalidFmt, value)
			}
		default:
			passArgs = append(passArgs, arg)
		}
	}
	return noSync, quiet, passArgs, nil
}
