package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	t.Cleanup(func() {
		Version = originalVersion
		updateExecutable = originalExecutable
		updateEvalSymlinks = originalEvalSymlinks
		updateLookPath = originalLookPath
		updateCommandOutput = originalCommandOutput
		updateRunCommand = originalRunCommand
		updateHTTPClient = originalHTTPClient
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
