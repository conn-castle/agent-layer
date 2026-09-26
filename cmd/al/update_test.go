package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

type updateRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn updateRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func preserveUpdateGlobals(t *testing.T) {
	t.Helper()
	t.Setenv(versiondispatch.EnvShimActive, "")
	originalVersion := Version
	originalExecutable := updateExecutable
	originalEvalSymlinks := updateEvalSymlinks
	originalLookPath := updateLookPath
	originalCommandOutput := updateCommandOutput
	originalRunCommand := updateRunCommand
	originalHTTPClient := updateHTTPClient
	originalInstalledVersion := updateInstalledVersion
	t.Cleanup(func() {
		Version = originalVersion
		updateExecutable = originalExecutable
		updateEvalSymlinks = originalEvalSymlinks
		updateLookPath = originalLookPath
		updateCommandOutput = originalCommandOutput
		updateRunCommand = originalRunCommand
		updateHTTPClient = originalHTTPClient
		updateInstalledVersion = originalInstalledVersion
	})
}

func TestUpdateUsesHomebrewForFormulaOwnedExecutable(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	updateExecutable = func() (string, error) { return "/opt/homebrew/bin/al", nil }
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/opt/homebrew/bin/al" {
			return "/opt/homebrew/Cellar/agent-layer/1.2.3/bin/al", nil
		}
		if path == "/opt/homebrew/opt/agent-layer" {
			return "/opt/homebrew/Cellar/agent-layer/1.2.3", nil
		}
		return path, nil
	}
	updateLookPath = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	updateCommandOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "/opt/homebrew/bin/brew" || strings.Join(args, " ") != "--prefix conn-castle/tap/agent-layer" {
			t.Fatalf("unexpected detection command: %s %v", name, args)
		}
		return []byte("/opt/homebrew/opt/agent-layer\n"), nil
	}
	var ranName string
	var ranArgs []string
	updateRunCommand = func(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
		ranName = name
		ranArgs = append([]string(nil), args...)
		return nil
	}
	updateInstalledVersion = func(_ context.Context, executable string) (string, error) {
		if executable != "/opt/homebrew/bin/al" {
			t.Fatalf("installed version executable = %q, want /opt/homebrew/bin/al", executable)
		}
		return "4.5.6", nil
	}

	command := newUpdateCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if ranName != "/opt/homebrew/bin/brew" || strings.Join(ranArgs, " ") != "upgrade conn-castle/tap/agent-layer" {
		t.Fatalf("ran %s %v, want Homebrew formula upgrade", ranName, ranArgs)
	}
	if !strings.Contains(output.String(), "Homebrew installation") {
		t.Fatalf("expected Homebrew-specific output, got %q", output.String())
	}
	if !strings.Contains(output.String(), "Agent Layer CLI update complete: v1.2.3 -> v4.5.6.") {
		t.Fatalf("expected before/after versions in completion message, got %q", output.String())
	}
	if !strings.Contains(output.String(), "Repository pins are unchanged; run `al upgrade plan`") {
		t.Fatalf("expected repository upgrade guidance, got %q", output.String())
	}
}

func TestUpdateUsesInstallerAndPreservesScriptPrefix(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	installRoot := filepath.Join(t.TempDir(), "custom-prefix")
	executable := filepath.Join(installRoot, "bin", "al")
	updateExecutable = func() (string, error) { return executable, nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	updateLookPath = func(string) (string, error) { return "", errors.New("brew not found") }
	updateHTTPClient = &http.Client{Transport: updateRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("User-Agent") != "agent-layer" {
			t.Fatalf("missing Agent Layer user agent")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("#!/usr/bin/env bash\n")),
		}, nil
	})}
	var installerPath string
	var ranArgs []string
	updateRunCommand = func(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
		if name != "bash" {
			t.Fatalf("command = %q, want bash", name)
		}
		installerPath = args[0]
		ranArgs = append([]string(nil), args...)
		if _, err := os.Stat(installerPath); err != nil {
			t.Fatalf("installer unavailable during command: %v", err)
		}
		return nil
	}
	updateInstalledVersion = func(_ context.Context, got string) (string, error) {
		if got != executable {
			t.Fatalf("installed version executable = %q, want %q", got, executable)
		}
		return "v4.5.6", nil
	}

	command := newUpdateCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	if err := command.Execute(); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if strings.Join(ranArgs[1:], " ") != "--prefix "+installRoot+" --no-completions" {
		t.Fatalf("installer args = %v, want preserved prefix %s", ranArgs, installRoot)
	}
	if _, err := os.Stat(installerPath); !os.IsNotExist(err) {
		t.Fatalf("temporary installer was not removed: %v", err)
	}
	if !strings.Contains(output.String(), "script installation at "+installRoot) {
		t.Fatalf("expected script-specific output, got %q", output.String())
	}
	if !strings.Contains(output.String(), "Agent Layer CLI update complete: v1.2.3 -> v4.5.6.") {
		t.Fatalf("expected before/after versions in completion message, got %q", output.String())
	}
}

func TestUpdateRejectsVersionDispatchedInvocation(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	t.Setenv(versiondispatch.EnvShimActive, "1")
	updateExecutable = func() (string, error) { return "/tmp/cache/versions/1.2.3/linux-amd64/al-linux-amd64", nil }
	updateEvalSymlinks = func(path string) (string, error) { return path, nil }
	updateLookPath = func(string) (string, error) { return "", errors.New("brew not found") }
	err := newUpdateCmd().Execute()
	if err == nil || !strings.Contains(err.Error(), "older global CLI") || !strings.Contains(err.Error(), "brew upgrade") {
		t.Fatalf("error = %v, want manual-update guidance", err)
	}
}

func TestUpdateRejectsDevelopmentBuild(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "dev"
	updateExecutable = func() (string, error) {
		t.Fatal("development update should stop before resolving the executable")
		return "", nil
	}
	if err := newUpdateCmd().Execute(); err == nil || !strings.Contains(err.Error(), "development builds") {
		t.Fatalf("error = %v, want development-build guidance", err)
	}
}

func TestUpdateSurfacesHomebrewCommandFailure(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	updateExecutable = func() (string, error) { return "/linuxbrew/bin/al", nil }
	updateEvalSymlinks = func(path string) (string, error) {
		switch path {
		case "/linuxbrew/bin/al":
			return "/linuxbrew/Cellar/agent-layer/1.2.3/bin/al", nil
		case "/linuxbrew/opt/agent-layer":
			return "/linuxbrew/Cellar/agent-layer/1.2.3", nil
		default:
			return path, nil
		}
	}
	updateLookPath = func(string) (string, error) { return "/linuxbrew/bin/brew", nil }
	updateCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("/linuxbrew/opt/agent-layer\n"), nil
	}
	updateRunCommand = func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		return errors.New("brew failed")
	}
	if err := newUpdateCmd().Execute(); err == nil || !strings.Contains(err.Error(), "update Agent Layer with Homebrew: brew failed") {
		t.Fatalf("error = %v, want contextual Homebrew failure", err)
	}
}

func TestDetectHomebrewInstallationFailsForUnqueryableCellarBinary(t *testing.T) {
	preserveUpdateGlobals(t)
	updateLookPath = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	updateCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("Error: tap is unavailable\n"), errors.New("broken brew")
	}
	_, _, err := detectHomebrewInstallation(context.Background(), "/opt/homebrew/Cellar/agent-layer/1.2.3/bin/al")
	if err == nil || !strings.Contains(err.Error(), "appears Homebrew-managed") || !strings.Contains(err.Error(), "tap is unavailable") {
		t.Fatalf("error = %v, want actionable Homebrew detection failure", err)
	}
}

func TestDetectHomebrewInstallationUsesBrewAssociatedWithExecutableCellar(t *testing.T) {
	preserveUpdateGlobals(t)
	updateLookPath = func(string) (string, error) {
		t.Fatal("PATH brew should not be used for a Cellar-owned executable")
		return "/usr/local/bin/brew", nil
	}
	updateCommandOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "/opt/homebrew/bin/brew" || strings.Join(args, " ") != "--prefix conn-castle/tap/agent-layer" {
			t.Fatalf("unexpected detection command: %s %v", name, args)
		}
		return []byte("/opt/homebrew/opt/agent-layer\n"), nil
	}
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/opt/homebrew/opt/agent-layer" {
			return "/opt/homebrew/Cellar/agent-layer/1.2.3", nil
		}
		return path, nil
	}

	isHomebrew, brew, err := detectHomebrewInstallation(context.Background(), "/opt/homebrew/Cellar/agent-layer/1.2.3/bin/al")
	if err != nil || !isHomebrew || brew != "/opt/homebrew/bin/brew" {
		t.Fatalf("isHomebrew = %v, brew = %q, err = %v; want associated Apple Silicon Homebrew", isHomebrew, brew, err)
	}
}

func TestDetectHomebrewInstallationFailsForCustomUnqueryableCellarBinary(t *testing.T) {
	preserveUpdateGlobals(t)
	t.Setenv("HOMEBREW_CELLAR", "/packages/kegs")
	updateLookPath = func(string) (string, error) { return "/packages/bin/brew", nil }
	updateCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("Error: formula unavailable\n"), errors.New("broken brew")
	}
	_, _, err := detectHomebrewInstallation(context.Background(), "/packages/kegs/agent-layer/1.2.3/bin/al")
	if err == nil || !strings.Contains(err.Error(), "appears Homebrew-managed") {
		t.Fatalf("error = %v, want custom-Cellar protection", err)
	}
}

func TestDetectHomebrewInstallationRejectsInactiveCellarKeg(t *testing.T) {
	preserveUpdateGlobals(t)
	updateLookPath = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	updateCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("/opt/homebrew/opt/agent-layer\n"), nil
	}
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/opt/homebrew/opt/agent-layer" {
			return "/opt/homebrew/Cellar/agent-layer/2.0.0", nil
		}
		return path, nil
	}
	_, _, err := detectHomebrewInstallation(context.Background(), "/opt/homebrew/Cellar/agent-layer/1.2.3/bin/al")
	if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("error = %v, want inactive-keg protection", err)
	}
}

func TestDetectHomebrewInstallationTreatsNonFormulaExecutableAsScriptInstall(t *testing.T) {
	preserveUpdateGlobals(t)
	updateLookPath = func(string) (string, error) { return "/home/linuxbrew/.linuxbrew/bin/brew", nil }
	updateCommandOutput = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("/home/linuxbrew/.linuxbrew/opt/agent-layer\n"), nil
	}
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/home/linuxbrew/.linuxbrew/opt/agent-layer" {
			return "/home/linuxbrew/.linuxbrew/Cellar/agent-layer/2.0.0", nil
		}
		return path, nil
	}
	isHomebrew, _, err := detectHomebrewInstallation(context.Background(), "/home/user/.local/bin/al")
	if err != nil || isHomebrew {
		t.Fatalf("isHomebrew = %v, err = %v; want script installation", isHomebrew, err)
	}
}

func TestScriptInstallPrefixRejectsUnexpectedLayout(t *testing.T) {
	if _, err := scriptInstallPrefix("/usr/local/agent-layer"); err == nil || !strings.Contains(err.Error(), "expected <prefix>/bin/al") {
		t.Fatalf("error = %v, want executable-layout guidance", err)
	}
}

func TestDownloadUpdateInstallerRejectsHTTPFailure(t *testing.T) {
	preserveUpdateGlobals(t)
	updateHTTPClient = &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       io.NopCloser(strings.NewReader("failure")),
		}, nil
	})}
	_, _, err := downloadUpdateInstaller(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Fatalf("error = %v, want HTTP status", err)
	}
}

func TestDownloadUpdateInstallerRejectsOversizedResponse(t *testing.T) {
	preserveUpdateGlobals(t)
	updateHTTPClient = &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", updateInstallerMaxBytes+1))),
		}, nil
	})}
	_, _, err := downloadUpdateInstaller(context.Background())
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("error = %v, want size-limit failure", err)
	}
}

func TestUpdateCompleteFallsBackToPostUpdateResolvedExecutable(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	updateExecutable = func() (string, error) { return "/opt/homebrew/bin/al", nil }
	resolvedPrefix := "/opt/homebrew/Cellar/agent-layer/1.2.3"
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/opt/homebrew/bin/al" {
			return resolvedPrefix + "/bin/al", nil
		}
		if path == "/opt/homebrew/opt/agent-layer" {
			return resolvedPrefix, nil
		}
		return path, nil
	}
	updateLookPath = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	updateCommandOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "/opt/homebrew/bin/brew" && strings.Join(args, " ") == "--prefix conn-castle/tap/agent-layer" {
			return []byte("/opt/homebrew/opt/agent-layer\n"), nil
		}
		t.Fatalf("unexpected detection command: %s %v", name, args)
		return nil, nil
	}
	updateRunCommand = func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		resolvedPrefix = "/opt/homebrew/Cellar/agent-layer/4.5.6"
		return nil
	}
	var queried []string
	updateInstalledVersion = func(_ context.Context, executable string) (string, error) {
		queried = append(queried, executable)
		if executable == "/opt/homebrew/bin/al" {
			return "", errors.New("gone")
		}
		if executable == "/opt/homebrew/Cellar/agent-layer/4.5.6/bin/al" {
			return "4.5.6", nil
		}
		t.Fatalf("unexpected executable %s", executable)
		return "", errors.New("unexpected executable")
	}

	command := newUpdateCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.Execute(); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if strings.Join(queried, ",") != "/opt/homebrew/bin/al,/opt/homebrew/Cellar/agent-layer/4.5.6/bin/al" {
		t.Fatalf("queried = %v, want original then post-update resolved executable", queried)
	}
	if !strings.Contains(stdout.String(), "Agent Layer CLI update complete: v1.2.3 -> v4.5.6.") {
		t.Fatalf("expected resolved after version, got %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestUpdateCompleteWarnsWhenInstalledVersionUnavailable(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "1.2.3"
	updateExecutable = func() (string, error) { return "/opt/homebrew/bin/al", nil }
	updateEvalSymlinks = func(path string) (string, error) {
		if path == "/opt/homebrew/bin/al" {
			return "/opt/homebrew/Cellar/agent-layer/1.2.3/bin/al", nil
		}
		if path == "/opt/homebrew/opt/agent-layer" {
			return "/opt/homebrew/Cellar/agent-layer/1.2.3", nil
		}
		return path, nil
	}
	updateLookPath = func(string) (string, error) { return "/opt/homebrew/bin/brew", nil }
	updateCommandOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "/opt/homebrew/bin/brew" && strings.Join(args, " ") == "--prefix conn-castle/tap/agent-layer" {
			return []byte("/opt/homebrew/opt/agent-layer\n"), nil
		}
		t.Fatalf("unexpected detection command: %s %v", name, args)
		return nil, nil
	}
	updateRunCommand = func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		return nil
	}
	updateInstalledVersion = func(context.Context, string) (string, error) {
		return "", errors.New("binary missing")
	}

	command := newUpdateCmd()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.Execute(); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Agent Layer CLI update complete: v1.2.3 -> unknown.") {
		t.Fatalf("expected unknown after version, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "could not determine installed CLI version: binary missing") {
		t.Fatalf("expected version warning, got %q", stderr.String())
	}
}

func TestUpdateReportsInstalledVersionInsidePinnedRepository(t *testing.T) {
	preserveUpdateGlobals(t)
	Version = "0.22.0"
	t.Setenv(versiondispatch.EnvDevelopmentBypassVersionDispatch, "")
	t.Setenv(versiondispatch.EnvNoNetwork, "1")

	installRoot := t.TempDir()
	binDir := filepath.Join(installRoot, "bin")
	if err := os.MkdirAll(binDir, 0o750); err != nil {
		t.Fatalf("create install bin directory: %v", err)
	}
	executable := filepath.Join(binDir, "al")
	build := exec.Command("go", "build", "-ldflags=-X main.Version=0.22.0", "-o", executable, ".") //nolint:gosec // Test builds this package into a temporary installation.
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installed CLI: %v\n%s", err, output)
	}
	updateExecutable = func() (string, error) { return executable, nil }
	updateLookPath = func(string) (string, error) { return "", errors.New("brew not found") }
	updateHTTPClient = &http.Client{Transport: updateRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("#!/usr/bin/env bash\n")),
		}, nil
	})}
	updateRunCommand = func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		return nil
	}

	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".agent-layer"), 0o750); err != nil {
		t.Fatalf("create pinned repository: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agent-layer", "al.version"), []byte("0.21.1\n"), 0o600); err != nil {
		t.Fatalf("write repository pin: %v", err)
	}
	testutil.WithWorkingDir(t, repo, func() {
		command := newUpdateCmd()
		var stdout, stderr bytes.Buffer
		command.SetOut(&stdout)
		command.SetErr(&stderr)
		if err := command.Execute(); err != nil {
			t.Fatalf("update failed: %v", err)
		}
		if !strings.Contains(stdout.String(), "Agent Layer CLI update complete: v0.22.0 -> v0.22.0.") {
			t.Fatalf("completion used repository pin instead of installed CLI: %q", stdout.String())
		}
		if !strings.Contains(stdout.String(), "Repository pins are unchanged") {
			t.Fatalf("missing pin guidance: %q", stdout.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("unexpected version probe diagnostic: %q", stderr.String())
		}
	})
}

func TestReadInstalledCLIVersionRejectsNonVersionOutput(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "al")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf 'Agent Layer version source: 0.21.1 (pin)\\n'\n"), 0o700); err != nil { // #nosec G306 -- test-owned executable fixture.
		t.Fatalf("write test executable: %v", err)
	}
	_, err := readInstalledCLIVersion(context.Background(), executable)
	if err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("error = %v, want invalid-version diagnostic", err)
	}
}
