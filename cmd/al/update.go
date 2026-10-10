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
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
		// Only the script installer uses fixed "bash"; resolved Homebrew keeps
		// its original foreground group so terminal prompts can read stdin.
		if name == "bash" {
			return runUpdateCommand(ctx, stdin, stdout, stderr, name, args...)
		}
		command := exec.CommandContext(ctx, name, args...) //nolint:gosec // Callers supply the resolved Homebrew executable and fixed arguments.
		command.Stdin, command.Stdout, command.Stderr = stdin, stdout, stderr
		return command.Run()
	}
	updateHTTPClient       = &http.Client{Timeout: 30 * time.Second}
	updateInstalledVersion = readInstalledCLIVersion
)

// runUpdateCommand keeps the group leader unreaped until cancellation signaling
// finishes. Bash gets time to run the installer's EXIT trap before escalation.
func runUpdateCommand(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.Command(name, args...) //nolint:gosec,noctx // Fixed update commands; cancellation owns the unreaped process group below.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	output := &updateCommandOutputGuard{}
	command.Stdin = stdin
	command.Stdout = output.wrap(stdout)
	command.Stderr = output.wrap(stderr)
	if err := command.Start(); err != nil {
		return err
	}
	pid := command.Process.Pid
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var shutdownErr error
	observationFailures := 0
	for {
		// Even an exited leader reserves its ID until the entire group stops.
		stopped, err := updateCommandStopped(pid)
		if err != nil {
			observationFailures++
			if observationFailures < 3 {
				err = nil
			}
		} else {
			observationFailures = 0
		}
		if ctx.Err() != nil || err != nil {
			shutdownErr = errors.Join(ctx.Err(), err)
			// No Wait has run, so even an exited leader still reserves this ID.
			shutdownErr = errors.Join(shutdownErr, updateSignalGroup(pid, syscall.SIGTERM))
			time.Sleep(time.Second)
			shutdownErr = errors.Join(shutdownErr, updateSignalGroup(pid, syscall.SIGKILL))
			// Also stop the owned leader if it moved out of its original group.
			if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				shutdownErr = errors.Join(shutdownErr, err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				stopped, err := updateCommandStopped(pid)
				if err == nil && stopped {
					break
				}
				if time.Now().After(deadline) {
					shutdownErr = errors.Join(shutdownErr, err, errors.New("update process group stop unproven after SIGKILL"))
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			break
		}
		if stopped {
			break
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	// All signaling precedes Wait: no timer can later hit a reused process group.
	// WaitDelay bounds pipe draining; the outer deadline bounds reaping too.
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer output.stopped.Store(true)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		if shutdownErr != nil && err != nil {
			// Cancellation stays a CLI error (exit 1), rather than exposing the
			// installer's TERM trap status as the CLI's normal command exit.
			return fmt.Errorf("%w: %v", shutdownErr, err)
		}
		return errors.Join(shutdownErr, err)
	case <-timer.C:
		return errors.Join(shutdownErr, errors.New("update command did not finish reaping within shutdown allowance"))
	}
}

func updateSignalGroup(pid int, signal syscall.Signal) error {
	err := syscall.Kill(-pid, signal)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

type updateCommandOutputGuard struct {
	sync.Mutex
	stopped atomic.Bool
}

type updateCommandWriter struct {
	*updateCommandOutputGuard
	writer io.Writer
}

func (g *updateCommandOutputGuard) wrap(writer io.Writer) io.Writer {
	if writer == nil {
		return nil
	}
	// Terminal writes must come from the foreground parent, even with TOSTOP.
	if file, direct := writer.(*os.File); direct && !term.IsTerminal(int(file.Fd())) { //nolint:gosec // Unix file descriptors are small non-negative ints.
		return writer
	}
	return &updateCommandWriter{g, writer}
}

func (w *updateCommandWriter) Write(data []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	if w.stopped.Load() {
		return len(data), nil
	}
	// Serialize destination writes, but stopping never waits on this mutex.
	// An arbitrary writer's already-started Write cannot be interrupted.
	return w.writer.Write(data)
}

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

	homebrew, err := detectHomebrewInstallation(cmd.Context(), resolvedExecutable)
	if err != nil {
		return err
	}
	versionProbe := executable
	if homebrew != nil {
		// On Linux, os.Executable already resolves to the pre-upgrade keg,
		// which brew upgrade removes or leaves at the old version. The formula
		// prefix is Homebrew's opt link, which the upgrade repoints to the new keg.
		versionProbe = filepath.Join(homebrew.formulaPrefix, "bin", "al")
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), messages.UpdateHomebrewStart)
		if err := updateRunCommand(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), homebrew.brew, "upgrade", homebrewAgentLayerFormula); err != nil {
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
		if err := updateRunCommand(cmd.Context(), nil, cmd.OutOrStdout(), cmd.ErrOrStderr(), "bash", installerPath, "--prefix", prefix, "--no-completions"); err != nil {
			return fmt.Errorf(messages.UpdateScriptRunErrFmt, err)
		}
	}

	fromVersion := formatCLIVersion(Version)
	toVersion := unknownVersion
	installed, err := readUpdatedCLIVersion(cmd.Context(), versionProbe)
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), messages.UpdateInstalledVersionWarnFmt, err)
	} else {
		toVersion = formatCLIVersion(installed)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), messages.UpdateCompleteFmt, fromVersion, toVersion)
	return nil
}

func readUpdatedCLIVersion(ctx context.Context, executable string) (string, error) {
	installed, err := updateInstalledVersion(ctx, executable)
	if err == nil {
		return installed, nil
	}
	resolved, resolveErr := updateEvalSymlinks(executable)
	if resolveErr != nil || resolved == "" || resolved == executable {
		return "", err
	}
	installed, resolvedErr := updateInstalledVersion(ctx, resolved)
	if resolvedErr != nil {
		return "", err
	}
	return installed, nil
}

func readInstalledCLIVersion(ctx context.Context, executable string) (string, error) {
	command := exec.CommandContext(ctx, executable, "--version") //nolint:gosec // executable is the running CLI's installation path.
	// The version probe must inspect the installed binary itself. A normal
	// `al --version` inside a repository can dispatch to its pinned CLI.
	command.Env = append(os.Environ(), versiondispatch.EnvDevelopmentBypassVersionDispatch+"=1")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", commandOutputError(err, []byte(fmt.Sprintf("%s\n%s", output, exitErr.Stderr)))
		}
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if line == "" {
		return "", errors.New(messages.UpdateInstalledVersionEmpty)
	}
	token := strings.Fields(line)[0]
	if _, err := version.Normalize(token); err != nil {
		return "", fmt.Errorf(messages.UpdateInstalledVersionInvalidFmt, line, err)
	}
	return line, nil
}

func formatCLIVersion(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return unknownVersion
	}
	token := trimmed
	if idx := strings.IndexAny(trimmed, " \t("); idx >= 0 {
		token = trimmed[:idx]
	}
	if version.IsDev(token) {
		return token
	}
	normalized, err := version.Normalize(token)
	if err != nil {
		return trimmed
	}
	return "v" + normalized
}

type homebrewInstallation struct {
	brew          string
	formulaPrefix string
}

// detectHomebrewInstallation returns nil when executable does not belong to the
// active agent-layer keg.
func detectHomebrewInstallation(ctx context.Context, executable string) (*homebrewInstallation, error) {
	brew, associatedBrew := homebrewExecutableFor(executable)
	if !associatedBrew {
		var err error
		brew, err = updateLookPath("brew")
		if err != nil {
			if looksHomebrewManaged(executable) {
				return nil, fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, err)
			}
			return nil, nil
		}
	}
	formulaPrefixOutput, err := updateCommandOutput(ctx, brew, "--prefix", homebrewAgentLayerFormula)
	if err != nil {
		if looksHomebrewManaged(executable) {
			return nil, fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, commandOutputError(err, formulaPrefixOutput))
		}
		return nil, nil
	}
	formulaPrefix := strings.TrimSpace(string(formulaPrefixOutput))
	if formulaPrefix == "" {
		return nil, nil
	}
	resolvedFormulaPrefix, err := updateEvalSymlinks(formulaPrefix)
	if err != nil {
		if looksHomebrewManaged(executable) {
			return nil, fmt.Errorf(messages.UpdateHomebrewPrefixErrFmt, err)
		}
		return nil, nil
	}
	if !pathWithin(executable, resolvedFormulaPrefix) {
		if looksHomebrewManaged(executable) {
			return nil, errors.New(messages.UpdateHomebrewOwnershipMismatch)
		}
		return nil, nil
	}
	return &homebrewInstallation{brew: brew, formulaPrefix: formulaPrefix}, nil
}

func homebrewExecutableFor(executable string) (string, bool) {
	for directory := filepath.Dir(filepath.Clean(executable)); ; directory = filepath.Dir(directory) {
		parent := filepath.Dir(directory)
		if filepath.Base(directory) == agentLayerFormulaName && filepath.Base(parent) == "Cellar" {
			return filepath.Join(filepath.Dir(parent), "bin", "brew"), true
		}
		if parent == directory {
			break
		}
	}

	cellar := strings.TrimSpace(os.Getenv("HOMEBREW_CELLAR"))
	prefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX"))
	if cellar != "" && prefix != "" && pathWithin(executable, filepath.Join(cellar, agentLayerFormulaName)) {
		return filepath.Join(prefix, "bin", "brew"), true
	}
	return "", false
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
