package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/install"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/version"
	"github.com/conn-castle/agent-layer/internal/wizard"
)

var runWizard = func(root string, pinVersion string) error {
	return wizard.Run(root, wizard.NewHuhUI(), alsync.Run, pinVersion)
}

var installRun = install.Run

func newInitCmd() *cobra.Command {
	var overwrite bool
	var force bool
	var noWizard bool
	var pinVersion string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize Agent Layer in this repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveInitRoot()
			if err != nil {
				return err
			}
			overwriteMode := overwrite || force
			if overwriteMode && !force && !isTerminal() {
				return fmt.Errorf("init overwrite prompts require an interactive terminal; re-run with --force to overwrite without prompts")
			}
			pinned, err := resolvePinVersion(pinVersion, Version)
			if err != nil {
				return err
			}
			warnInitUpdate(cmd, pinVersion)
			opts := install.Options{
				Overwrite:  overwriteMode,
				Force:      force,
				PinVersion: pinned,
			}
			if overwriteMode && !force {
				opts.PromptOverwrite = func(path string) (bool, error) {
					prompt := fmt.Sprintf("Overwrite %s with the template version?", path)
					return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, true)
				}
			}
			if err := installRun(root, opts); err != nil {
				return err
			}
			if noWizard || !isTerminal() {
				return nil
			}
			run, err := promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), "Run setup wizard now? (recommended)", true)
			if err != nil {
				return err
			}
			if !run {
				return nil
			}
			return runWizard(root, pinned)
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "Prompt before overwriting existing template files")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing template files without prompting (implies --overwrite)")
	cmd.Flags().BoolVar(&noWizard, "no-wizard", false, "Skip prompting to run the setup wizard after init")
	cmd.Flags().StringVar(&pinVersion, "version", "", "Pin the repo to a specific Agent Layer version (vX.Y.Z or X.Y.Z)")

	return cmd
}

// warnInitUpdate emits a warning when a newer Agent Layer release is available.
func warnInitUpdate(cmd *cobra.Command, flagVersion string) {
	if strings.TrimSpace(flagVersion) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv(dispatch.EnvVersionOverride)) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv(dispatch.EnvNoNetwork)) != "" {
		return
	}
	result, err := checkForUpdate(context.Background(), Version)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to check for updates: %v\n", err)
		return
	}
	if result.CurrentIsDev {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: running dev build; latest release is %s\n", result.Latest)
		return
	}
	if result.Outdated {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: update available: %s (current %s)\n", result.Latest, result.Current)
	}
}

// resolvePinVersion returns the normalized pin version for init, or empty when dev builds should not pin.
func resolvePinVersion(flagValue string, buildVersion string) (string, error) {
	if strings.TrimSpace(flagValue) != "" {
		normalized, err := version.Normalize(flagValue)
		if err != nil {
			return "", err
		}
		return normalized, nil
	}
	if version.IsDev(buildVersion) {
		return "", nil
	}
	normalized, err := version.Normalize(buildVersion)
	if err != nil {
		return "", err
	}
	return normalized, nil
}

// promptYesNo asks a yes/no question and returns the user's choice or an error.
// defaultYes controls the result when the user provides an empty response.
func promptYesNo(in io.Reader, out io.Writer, prompt string, defaultYes bool) (bool, error) {
	reader := bufio.NewReader(in)
	for {
		if defaultYes {
			if _, err := fmt.Fprintf(out, "%s [Y/n]: ", prompt); err != nil {
				return false, err
			}
		} else {
			if _, err := fmt.Fprintf(out, "%s [y/N]: ", prompt); err != nil {
				return false, err
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		response := strings.TrimSpace(line)
		if response == "" {
			if errors.Is(err, io.EOF) {
				return false, nil
			}
			if err == nil {
				return defaultYes, nil
			}
		}
		switch strings.ToLower(response) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		if errors.Is(err, io.EOF) {
			return false, fmt.Errorf("invalid response %q", response)
		}
		if _, err := fmt.Fprintln(out, "Please enter y or n."); err != nil {
			return false, err
		}
	}
}
