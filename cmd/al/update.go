package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/version"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

const (
	agentLayerFormulaName     = "agent-layer"
	homebrewAgentLayerFormula = "conn-castle/tap/agent-layer"
	updateInstallerMaxBytes   = 1 << 20
)

var (
	updateExecutable    = os.Executable
	updateEvalSymlinks  = filepath.EvalSymlinks
	updateLookPath      = exec.LookPath
	updateCommandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // Callers supply the resolved Homebrew executable and fixed arguments.
	}
	updateRunCommand = func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
		command := exec.CommandContext(ctx, name, args...) //nolint:gosec // Callers select only resolved Homebrew or fixed bash commands.
		command.Stdin = stdin
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	}
	updateHTTPClient = &http.Client{Timeout: 30 * time.Second}
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.UpdateUse,
		Short: messages.UpdateShort,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(cmd)
		},
	}
}

func runUpdate(cmd *cobra.Command) error {
	if version.IsDev(Version) {
		return errors.New(messages.UpdateDevBuildUnsupported)
	}

	executable, err := updateExecutable()
	if err != nil {
		return fmt.Errorf(messages.UpdateResolveExecutableErrFmt, err)
	}
	resolvedExecutable, err := updateEvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf(messages.UpdateResolveExecutableLinkErrFmt, err)
	}

	isHomebrew, brew, err := detectHomebrewInstallation(cmd.Context(), resolvedExecutable)
	if err != nil {
		return err
	}
	if isHomebrew {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.UpdateHomebrewStart)
		if err := updateRunCommand(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), brew, "upgrade", homebrewAgentLayerFormula); err != nil {
			return fmt.Errorf(messages.UpdateHomebrewRunErrFmt, err)
		}
	} else {
		prefix, err := scriptInstallPrefix(resolvedExecutable)
		if err != nil {
			if strings.TrimSpace(os.Getenv(versiondispatch.EnvShimActive)) != "" {
				return errors.New(messages.UpdateDispatchedCLIUnsupported)
			}
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), messages.UpdateScriptStartFmt, prefix)
		installerPath, cleanup, err := downloadUpdateInstaller(cmd.Context())
		if err != nil {
			return err
		}
		defer cleanup()
		if err := updateRunCommand(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), "bash", installerPath, "--prefix", prefix, "--no-completions"); err != nil {
			return fmt.Errorf(messages.UpdateScriptRunErrFmt, err)
		}
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.UpdateComplete)
	return nil
}

func detectHomebrewInstallation(ctx context.Context, executable string) (bool, string, error) {
	brew, err := updateLookPath("brew")
	if err != nil {
		if looksHomebrewManaged(executable) {
			return false, "", fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, err)
		}
		return false, "", nil
	}
	formulaPrefixOutput, err := updateCommandOutput(ctx, brew, "--prefix", homebrewAgentLayerFormula)
	if err != nil {
		if looksHomebrewManaged(executable) {
			return false, "", fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, commandOutputError(err, formulaPrefixOutput))
		}
		return false, "", nil
	}
	formulaPrefix := strings.TrimSpace(string(formulaPrefixOutput))
	if formulaPrefix == "" {
		return false, "", nil
	}
	resolvedFormulaPrefix, err := updateEvalSymlinks(formulaPrefix)
	if err != nil {
		if looksHomebrewManaged(executable) {
			return false, "", fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, err)
		}
		return false, "", nil
	}
	if !pathWithin(executable, resolvedFormulaPrefix) {
		if looksHomebrewManaged(executable) {
			return false, "", errors.New(messages.UpdateHomebrewOwnershipMismatch)
		}
		return false, "", nil
	}
	return true, brew, nil
}

func commandOutputError(err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func scriptInstallPrefix(executable string) (string, error) {
	if filepath.Base(executable) != "al" || filepath.Base(filepath.Dir(executable)) != "bin" {
		return "", fmt.Errorf(messages.UpdateScriptLayoutErrFmt, executable)
	}
	return filepath.Dir(filepath.Dir(executable)), nil
}

func looksHomebrewManaged(path string) bool {
	if cellar := strings.TrimSpace(os.Getenv("HOMEBREW_CELLAR")); cellar != "" && pathWithin(path, filepath.Join(cellar, agentLayerFormulaName)) {
		return true
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "Cellar" && parts[i+1] == agentLayerFormulaName {
			return true
		}
	}
	return false
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return relative != ".." && relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func downloadUpdateInstaller(ctx context.Context) (string, func(), error) {
	url := update.ReleasesBaseURL + "/latest/download/al-install.sh"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerRequestErrFmt, err)
	}
	request.Header.Set("User-Agent", "agent-layer")
	response, err := updateHTTPClient.Do(request) //nolint:gosec // The URL is the fixed official Agent Layer release endpoint.
	if err != nil {
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerDownloadErrFmt, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerStatusErrFmt, response.Status)
	}

	temporary, err := os.CreateTemp("", "agent-layer-update-*.sh")
	if err != nil {
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerTempErrFmt, err)
	}
	path := temporary.Name()
	cleanup := func() { _ = os.Remove(path) }
	written, err := io.Copy(temporary, io.LimitReader(response.Body, updateInstallerMaxBytes+1))
	if err != nil {
		_ = temporary.Close()
		cleanup()
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerWriteErrFmt, err)
	}
	if written > updateInstallerMaxBytes {
		_ = temporary.Close()
		cleanup()
		return "", func() {}, errors.New(messages.UpdateInstallerTooLarge)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf(messages.UpdateInstallerCloseErrFmt, err)
	}
	return path, cleanup, nil
}
