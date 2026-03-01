package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/version"
	"github.com/conn-castle/agent-layer/internal/wizard"
)

var runWizard = func(root string, pinVersion string) error {
	return wizard.RunWithWriter(root, wizard.NewHuhUI(), alsync.Run, pinVersion, os.Stdout)
}

var installRun = install.Run
var installRollbackUpgradeSnapshot = install.RollbackUpgradeSnapshot
var statAgentLayerPath = os.Stat

var resolveLatestPinVersion = func(ctx context.Context, currentVersion string) (string, error) {
	result, err := checkForUpdate(ctx, currentVersion)
	if err != nil {
		return "", err
	}
	latest := strings.TrimSpace(result.Latest)
	if latest == "" {
		return "", fmt.Errorf(messages.InitLatestVersionMissing)
	}
	return latest, nil
}

var releaseValidationHTTPClient = &http.Client{Timeout: 10 * time.Second}
var releaseValidationBaseURL = update.ReleasesBaseURL
var validatePinnedReleaseVersionFunc = validatePinnedReleaseVersion

func newInitCmd() *cobra.Command {
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
			agentLayerPath := filepath.Join(root, ".agent-layer")
			if info, err := statAgentLayerPath(agentLayerPath); err == nil {
				if !info.IsDir() {
					return fmt.Errorf(messages.RootPathNotDirFmt, agentLayerPath)
				}
				return fmt.Errorf(messages.InitAlreadyInitialized)
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf(messages.InstallFailedStatFmt, agentLayerPath, err)
			}
			pinned, err := resolvePinVersionForInit(cmd.Context(), pinVersion, Version)
			if err != nil {
				return err
			}
			if strings.TrimSpace(pinVersion) != "" && !strings.EqualFold(strings.TrimSpace(pinVersion), "latest") {
				if err := validatePinnedReleaseVersionFunc(cmd.Context(), pinned); err != nil {
					return err
				}
			}
			warnInitUpdate(cmd, pinVersion)
			opts := install.Options{
				Overwrite:  false,
				PinVersion: pinned,
				System:     install.RealSystem{},
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
		if update.IsRateLimitError(err) {
			return
		}
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

// resolvePinVersionForInit resolves the init pin target, including --version latest.
func resolvePinVersionForInit(ctx context.Context, flagValue string, buildVersion string) (string, error) {
	flag := strings.TrimSpace(flagValue)
	if strings.EqualFold(flag, "latest") {
		latest, err := resolveLatestPinVersion(ctx, buildVersion)
		if err != nil {
			return "", fmt.Errorf(messages.InitResolveLatestVersionFmt, err)
		}
		return latest, nil
	}
	return resolvePinVersion(flag, buildVersion)
}

// validatePinnedReleaseVersion checks that a requested pin version exists upstream.
func validatePinnedReleaseVersion(ctx context.Context, pinVersion string) error {
	normalized, err := version.Normalize(pinVersion)
	if err != nil {
		return err
	}
	releaseURL := fmt.Sprintf("%s/tag/v%s", releaseValidationBaseURL, normalized)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releaseURL, nil)
	if err != nil {
		return fmt.Errorf(messages.InitCreateReleaseValidationRequestFmt, err)
	}
	req.Header.Set("User-Agent", "agent-layer")
	resp, err := releaseValidationHTTPClient.Do(req) //nolint:gosec // URL is a validated GitHub release URL
	if err != nil {
		return fmt.Errorf(messages.InitValidateReleaseVersionRequestFmt, normalized, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf(messages.InitReleaseVersionNotFoundFmt, normalized, releaseValidationBaseURL)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf(messages.InitValidateReleaseVersionStatusFmt, normalized, resp.Status)
	}
	return nil
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

// printFilePaths prints a list of file paths with a header.
func printFilePaths(out io.Writer, header string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, header); err != nil {
		return err
	}
	for _, path := range paths {
		if _, err := fmt.Fprintf(out, messages.InstallDiffLineFmt, path); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return nil
}
