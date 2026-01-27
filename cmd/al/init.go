package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
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
		Use:   messages.InitUse,
		Short: messages.InitShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveInitRoot()
			if err != nil {
				return err
			}
			overwriteMode := overwrite || force
			if overwriteMode && !force && !isTerminal() {
				return fmt.Errorf(messages.InitOverwriteRequiresTerminal)
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
				opts.PromptOverwriteAll = func() (bool, error) {
					return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.InitOverwriteAllPrompt, true)
				}
				opts.PromptOverwrite = func(path string) (bool, error) {
					prompt := fmt.Sprintf(messages.InitOverwritePromptFmt, path)
					return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, true)
				}
				opts.PromptDeleteUnknownAll = func(paths []string) (bool, error) {
					if len(paths) > 0 {
						if _, err := fmt.Fprintln(cmd.OutOrStdout(), messages.InstallUnknownHeader); err != nil {
							return false, err
						}
						for _, path := range paths {
							if _, err := fmt.Fprintf(cmd.OutOrStdout(), messages.InstallDiffLineFmt, path); err != nil {
								return false, err
							}
						}
					}
					return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.InitDeleteUnknownAllPrompt, false)
				}
				opts.PromptDeleteUnknown = func(path string) (bool, error) {
					prompt := fmt.Sprintf(messages.InitDeleteUnknownPromptFmt, path)
					return promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, false)
				}
			}
			if err := installRun(root, opts); err != nil {
				return err
			}
			if noWizard || !isTerminal() {
				return nil
			}
			run, err := promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), messages.InitRunWizardPrompt, true)
			if err != nil {
				return err
			}
			if !run {
				return nil
			}
			return runWizard(root, pinned)
		},
	}

	cmd.Flags().BoolVar(&overwrite, "overwrite", false, messages.InitFlagOverwrite)
	cmd.Flags().BoolVar(&force, "force", false, messages.InitFlagForce)
	cmd.Flags().BoolVar(&noWizard, "no-wizard", false, messages.InitFlagNoWizard)
	cmd.Flags().StringVar(&pinVersion, "version", "", messages.InitFlagVersion)

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
	warnColor := color.New(color.FgYellow)
	result, err := checkForUpdate(cmd.Context(), Version)
	if err != nil {
		_, _ = warnColor.Fprintf(cmd.ErrOrStderr(), messages.InitWarnUpdateCheckFailedFmt, err)
		return
	}
	if result.CurrentIsDev {
		_, _ = warnColor.Fprintf(cmd.ErrOrStderr(), messages.InitWarnDevBuildFmt, result.Latest)
		return
	}
	if result.Outdated {
		_, _ = warnColor.Fprintf(cmd.ErrOrStderr(), messages.InitWarnUpdateAvailableFmt, result.Latest, result.Current)
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
			if _, err := fmt.Fprintf(out, messages.PromptYesDefaultFmt, prompt); err != nil {
				return false, err
			}
		} else {
			if _, err := fmt.Fprintf(out, messages.PromptNoDefaultFmt, prompt); err != nil {
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
			return false, fmt.Errorf(messages.PromptInvalidResponse, response)
		}
		if _, err := fmt.Fprintln(out, messages.PromptRetryYesNo); err != nil {
			return false, err
		}
	}
}
