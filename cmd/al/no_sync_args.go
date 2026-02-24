package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// newNoSyncLaunchCmd builds a cobra.Command that supports --no-sync pass-through
// for agent launchers that open VS Code.
func newNoSyncLaunchCmd(
	use, short, agentName string,
	enabledFn clients.EnabledSelector,
	launchFn clients.LaunchFunc,
) *cobra.Command {
	cmd := &cobra.Command{
		Use:                use,
		Short:              short,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			noSync, quiet, passArgs, err := splitNoSyncArgs(args)
			if err != nil {
				return err
			}
			if noSync {
				return clients.RunNoSync(root, agentName, enabledFn, launchFn, quiet, passArgs)
			}
			return clients.Run(cmd.Context(), root, agentName, enabledFn, launchFn, quiet, passArgs, Version)
		},
	}

	cmd.Flags().Bool("no-sync", false, "Skip sync before launching")

	return cmd
}

// splitNoSyncArgs manually parses --no-sync because flag parsing is disabled for pass-through.
func splitNoSyncArgs(args []string) (bool, bool, []string, error) {
	noSync := false
	quiet := false
	passArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			passArgs = append(passArgs, args[i+1:]...)
			break
		}
		if arg == "--no-sync" {
			noSync = true
			continue
		}
		if strings.HasPrefix(arg, "--no-sync=") {
			value := strings.TrimPrefix(arg, "--no-sync=")
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, false, nil, fmt.Errorf(messages.NoSyncInvalidFmt, value)
			}
			noSync = parsed
			continue
		}
		if arg == flagQuiet || arg == flagQuietShort {
			quiet = true
			continue
		}
		if strings.HasPrefix(arg, flagQuietPrefix) {
			value := strings.TrimPrefix(arg, flagQuietPrefix)
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, false, nil, fmt.Errorf(messages.QuietInvalidFmt, value)
			}
			quiet = parsed
			continue
		}
		passArgs = append(passArgs, arg)
	}
	return noSync, quiet, passArgs, nil
}
