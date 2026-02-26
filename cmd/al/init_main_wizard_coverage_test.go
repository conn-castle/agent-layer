package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
	"github.com/conn-castle/agent-layer/internal/update"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestResolveLatestPinVersion_Branches(t *testing.T) {
	origCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = origCheck })

	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{}, errors.New("upstream error")
	}
	if _, err := resolveLatestPinVersion(context.Background(), "1.0.0"); err == nil {
		t.Fatal("expected upstream error")
	}

	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Latest: "   "}, nil
	}
	if _, err := resolveLatestPinVersion(context.Background(), "1.0.0"); err == nil {
		t.Fatal("expected empty-latest error")
	}
}

func TestValidatePinnedReleaseVersion_ErrorBranches(t *testing.T) {
	origClient := releaseValidationHTTPClient
	origBaseURL := releaseValidationBaseURL
	t.Cleanup(func() {
		releaseValidationHTTPClient = origClient
		releaseValidationBaseURL = origBaseURL
	})

	if err := validatePinnedReleaseVersion(context.Background(), "not-a-version"); err == nil {
		t.Fatal("expected normalize error")
	}

	releaseValidationBaseURL = "://bad-url"
	if err := validatePinnedReleaseVersion(context.Background(), "1.2.3"); err == nil {
		t.Fatal("expected request creation error")
	}

	releaseValidationBaseURL = "https://example.invalid"
	releaseValidationHTTPClient = &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		}),
	}
	if err := validatePinnedReleaseVersion(context.Background(), "1.2.3"); err == nil || !strings.Contains(err.Error(), "validate requested release v1.2.3") {
		t.Fatalf("expected request error, got %v", err)
	}
}

func TestPrintFilePaths_Branches(t *testing.T) {
	t.Run("empty paths no-op", func(t *testing.T) {
		var out bytes.Buffer
		if err := printFilePaths(&out, "header", nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if out.Len() != 0 {
			t.Fatalf("expected no output, got %q", out.String())
		}
	})

	t.Run("newline write error", func(t *testing.T) {
		err := printFilePaths(&errorWriter{failAfter: 0}, "header", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("header write error", func(t *testing.T) {
		err := printFilePaths(&errorWriter{failAfter: 1}, "header", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("path line write error", func(t *testing.T) {
		err := printFilePaths(&errorWriter{failAfter: 2}, "header", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})

	t.Run("trailing newline write error", func(t *testing.T) {
		err := printFilePaths(&errorWriter{failAfter: 3}, "header", []string{"a"})
		if err == nil || !strings.Contains(err.Error(), "write failed") {
			t.Fatalf("expected write failure, got %v", err)
		}
	})
}

func TestSilentExitError_ErrorString(t *testing.T) {
	got := (&SilentExitError{Code: 7}).Error()
	if got != "exit 7" {
		t.Fatalf("unexpected error string: %q", got)
	}
}

func TestFirstCommandArg_AdditionalBranches(t *testing.T) {
	if got := firstCommandArg([]string{"--"}); got != "" {
		t.Fatalf("expected empty arg for trailing separator, got %q", got)
	}
	if got := firstCommandArg([]string{" ", "\t", "doctor"}); got != "doctor" {
		t.Fatalf("expected doctor, got %q", got)
	}
}

func TestHasQuietFlag_InvalidBoolIgnored(t *testing.T) {
	if got := hasQuietFlag([]string{"al", "--quiet=definitely-not-bool"}); got {
		t.Fatal("expected invalid bool to be ignored")
	}
}

func TestQuietFromConfig_Branches(t *testing.T) {
	origFind := findAgentLayerRoot
	t.Cleanup(func() { findAgentLayerRoot = origFind })

	findAgentLayerRoot = func(string) (string, bool, error) {
		return "", false, nil
	}
	if quietFromConfig(t.TempDir()) {
		t.Fatal("expected false when root not found")
	}

	findAgentLayerRoot = func(string) (string, bool, error) {
		return "", false, errors.New("lookup failed")
	}
	if quietFromConfig(t.TempDir()) {
		t.Fatal("expected false on root lookup error")
	}
}

func TestWizardCommand_AdditionalBranches(t *testing.T) {
	t.Run("cleanup backups none", func(t *testing.T) {
		origGetwd := getwd
		origCleanup := cleanupWizardBackups
		origRunWizardProfile := runWizardProfile
		origRunWizard := runWizard
		t.Cleanup(func() {
			getwd = origGetwd
			cleanupWizardBackups = origCleanup
			runWizardProfile = origRunWizardProfile
			runWizard = origRunWizard
		})

		root := t.TempDir()
		getwd = func() (string, error) { return root, nil }
		cleanupWizardBackups = func(string) ([]string, error) { return nil, nil }

		var out bytes.Buffer
		cmd := newWizardCmd()
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--cleanup-backups"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !strings.Contains(out.String(), messages.WizardCleanupBackupsNone) {
			t.Fatalf("expected none message, got %q", out.String())
		}
	})

	t.Run("cleanup backups error", func(t *testing.T) {
		origGetwd := getwd
		origCleanup := cleanupWizardBackups
		origRunWizardProfile := runWizardProfile
		origRunWizard := runWizard
		t.Cleanup(func() {
			getwd = origGetwd
			cleanupWizardBackups = origCleanup
			runWizardProfile = origRunWizardProfile
			runWizard = origRunWizard
		})

		root := t.TempDir()
		getwd = func() (string, error) { return root, nil }
		cleanupWizardBackups = func(string) ([]string, error) { return nil, errors.New("cleanup failed") }

		cmd := newWizardCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"--cleanup-backups"})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "cleanup failed") {
			t.Fatalf("expected cleanup error, got %v", err)
		}
	})

	t.Run("non-interactive without profile requires terminal", func(t *testing.T) {
		origGetwd := getwd
		origIsTerminal := isTerminal
		origRunWizard := runWizard
		origRunWizardProfile := runWizardProfile
		origCleanup := cleanupWizardBackups
		t.Cleanup(func() {
			getwd = origGetwd
			isTerminal = origIsTerminal
			runWizard = origRunWizard
			runWizardProfile = origRunWizardProfile
			cleanupWizardBackups = origCleanup
		})

		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		getwd = func() (string, error) { return root, nil }
		isTerminal = func() bool { return false }

		cmd := newWizardCmd()
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), messages.WizardRequiresTerminal) {
			t.Fatalf("expected terminal-required error, got %v", err)
		}
	})
}

func TestInitWarnUpdate_FailureBranch(t *testing.T) {
	origCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = origCheck })

	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{}, errors.New("check failed")
	}

	cmd := newInitCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	warnInitUpdate(cmd, "")
	if !strings.Contains(errBuf.String(), "failed to check for updates") {
		t.Fatalf("expected warning output, got %q", errBuf.String())
	}
}

func TestRunWizard_DefaultClosurePath(t *testing.T) {
	root := t.TempDir()
	err := runWizard(root, "1.2.3")
	if err == nil {
		t.Fatal("expected error when default wizard runs without interactive terminal")
	}
}

func TestResolveLatestPinVersion_RespectsNoNetworkEnvOutsideResolver(t *testing.T) {
	origCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = origCheck })

	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Latest: "2.0.0"}, nil
	}

	t.Setenv(dispatch.EnvNoNetwork, "1")
	latest, err := resolveLatestPinVersion(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("expected resolver to ignore env and return latest, got %v", err)
	}
	if latest != "2.0.0" {
		t.Fatalf("expected latest 2.0.0, got %q", latest)
	}
}

func TestPrintFilePaths_WithWorkingDirUtility(t *testing.T) {
	root := t.TempDir()
	testutil.WithWorkingDir(t, root, func() {
		var out bytes.Buffer
		if err := printFilePaths(&out, "Header", []string{"a.txt", "b.txt"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if !strings.Contains(out.String(), "a.txt") || !strings.Contains(out.String(), "b.txt") {
			t.Fatalf("expected output paths, got %q", out.String())
		}
	})
}
