package main

// NOTE: Tests in this file mutate package-level globals (getwd, isTerminal,
// installRun, runWizard, checkForUpdate). Do not use t.Parallel() at the
// top level. Each test must restore globals via t.Cleanup().

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/update"
)

type slowReader struct {
	r io.Reader
}

func (sr *slowReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return sr.r.Read(p)
}

func TestInitCmd(t *testing.T) {
	// Capture original globals and restore them after the test.
	// Using a single defer avoids LIFO ordering issues with multiple defers.
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origCheckForUpdate := checkForUpdate

	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		checkForUpdate = origCheckForUpdate
	})

	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	tests := []struct {
		name                   string
		args                   []string
		isTerminal             bool
		mockInstallErr         error
		mockWizardErr          error
		userInput              string // for stdin
		wantErr                bool
		wantInstall            bool
		wantWizard             bool
		precreateAgentLayerDir bool
		checkErr               func(error) bool
	}{
		{
			name:        "Happy path non-interactive",
			args:        []string{},
			isTerminal:  false,
			wantInstall: true,
			wantWizard:  false,
		},
		{
			name:        "Happy path interactive no wizard",
			args:        []string{},
			isTerminal:  true,
			userInput:   "n\n", // Don't run wizard
			wantInstall: true,
			wantWizard:  false,
		},
		{
			name:        "Happy path interactive yes wizard",
			args:        []string{},
			isTerminal:  true,
			userInput:   "y\n", // Run wizard
			wantInstall: true,
			wantWizard:  true,
		},
		{
			name:           "Install fails",
			args:           []string{},
			isTerminal:     false,
			mockInstallErr: fmt.Errorf("install failed"),
			wantErr:        true,
			wantInstall:    true,
		},
		{
			name:                   "Already initialized errors",
			args:                   []string{},
			isTerminal:             false,
			precreateAgentLayerDir: true,
			wantErr:                true,
			wantInstall:            false,
			checkErr: func(err error) bool {
				return err != nil && strings.Contains(err.Error(), "already initialized")
			},
		},
		{
			name:        "No Wizard Flag",
			args:        []string{"--no-wizard"},
			isTerminal:  true, // even if terminal, should skip
			wantInstall: true,
			wantWizard:  false,
		},
		{
			name:    "Resolve Pin Version Error",
			args:    []string{"--version", "invalid"},
			wantErr: true,
			checkErr: func(err error) bool {
				return err != nil // Specific error message check if needed
			},
		},
		{
			name:    "Resolve Root Error",
			args:    []string{},
			wantErr: true,
			checkErr: func(err error) bool {
				return err != nil && err.Error() == "getwd failed"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp dir as root
			tmpDir := t.TempDir()
			// Create .git to make it a valid root
			if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
				t.Fatal(err)
			}
			if tt.precreateAgentLayerDir {
				if err := os.MkdirAll(filepath.Join(tmpDir, ".agent-layer"), 0o755); err != nil {
					t.Fatal(err)
				}
			}

			// Mock getwd
			getwd = func() (string, error) {
				return tmpDir, nil
			}

			// Custom mock for root error
			if tt.name == "Resolve Root Error" {
				getwd = func() (string, error) {
					return "", fmt.Errorf("getwd failed")
				}
			}

			// Mock isTerminal
			isTerminal = func() bool {
				return tt.isTerminal
			}

			// Mock installRun
			installCalled := false
			installRun = func(root string, opts install.Options) error {
				installCalled = true
				if root != tmpDir {
					t.Errorf("installRun root = %v, want %v", root, tmpDir)
				}
				if opts.Overwrite {
					t.Errorf("installRun opts.Overwrite = true, want false")
				}
				if opts.Force {
					t.Errorf("installRun opts.Force = true, want false")
				}
				if opts.Prompter != nil {
					t.Errorf("installRun opts.Prompter = %T, want nil", opts.Prompter)
				}

				return tt.mockInstallErr
			}

			// Mock runWizard
			wizardCalled := false
			runWizard = func(root string, pinVersion string) error {
				wizardCalled = true
				return tt.mockWizardErr
			}

			cmd := newInitCmd()
			cmd.SetArgs(tt.args)

			// Setup stdin/stdout
			var stdin bytes.Buffer
			if tt.userInput != "" {
				stdin.WriteString(tt.userInput)
			}
			cmd.SetIn(&slowReader{r: &stdin})
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			var stderr bytes.Buffer
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.checkErr != nil && err != nil {
				if !tt.checkErr(err) {
					t.Errorf("Execute() error = %v, failed checkErr", err)
				}
			}

			// If we expect resolve/init-gating error, installRun should not run.
			if tt.name == "Resolve Root Error" || tt.name == "Resolve Pin Version Error" || tt.name == "Already initialized errors" {
				if installCalled {
					t.Error("installRun called unexpectedly")
				}
			} else {
				if installCalled != tt.wantInstall {
					t.Errorf("installCalled = %v, want %v", installCalled, tt.wantInstall)
				}
			}

			if wizardCalled != tt.wantWizard {
				t.Errorf("wizardCalled = %v, want %v", wizardCalled, tt.wantWizard)
			}
		})
	}
}

func TestResolvePinVersion(t *testing.T) {
	tests := []struct {
		name         string
		flagValue    string
		buildVersion string
		want         string
		wantErr      bool
	}{
		{
			name:         "Explicit valid version",
			flagValue:    "v1.2.3",
			buildVersion: "dev",
			want:         "1.2.3",
			wantErr:      false,
		},
		{
			name:         "Explicit valid version no v",
			flagValue:    "1.2.3",
			buildVersion: "dev",
			want:         "1.2.3",
			wantErr:      false,
		},
		{
			name:         "Explicit invalid version",
			flagValue:    "invalid",
			buildVersion: "dev",
			want:         "",
			wantErr:      true,
		},
		{
			name:         "No flag, dev build",
			flagValue:    "",
			buildVersion: "dev",
			want:         "",
			wantErr:      false,
		},
		{
			name:         "No flag, explicit build version",
			flagValue:    "",
			buildVersion: "v2.0.0",
			want:         "2.0.0",
			wantErr:      false,
		},
		{
			name:         "No flag, invalid build version",
			flagValue:    "",
			buildVersion: "invalid-build",
			want:         "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePinVersion(tt.flagValue, tt.buildVersion)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolvePinVersion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("resolvePinVersion() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolvePinVersionForInit(t *testing.T) {
	origResolveLatestPinVersion := resolveLatestPinVersion
	t.Cleanup(func() {
		resolveLatestPinVersion = origResolveLatestPinVersion
	})

	resolveLatestPinVersion = func(context.Context, string) (string, error) {
		return "3.4.5", nil
	}
	got, err := resolvePinVersionForInit(context.Background(), "latest", "1.0.0")
	if err != nil {
		t.Fatalf("resolvePinVersionForInit(latest) error: %v", err)
	}
	if got != "3.4.5" {
		t.Fatalf("resolvePinVersionForInit(latest) = %q, want 3.4.5", got)
	}

	resolveLatestPinVersion = func(context.Context, string) (string, error) {
		return "", errors.New("network failed")
	}
	_, err = resolvePinVersionForInit(context.Background(), "LATEST", "1.0.0")
	if err == nil {
		t.Fatal("expected error resolving latest pin")
	}
	if !strings.Contains(err.Error(), "resolve latest version") {
		t.Fatalf("expected latest-resolution error, got %v", err)
	}

	got, err = resolvePinVersionForInit(context.Background(), "v2.0.1", "1.0.0")
	if err != nil {
		t.Fatalf("resolvePinVersionForInit(explicit) error: %v", err)
	}
	if got != "2.0.1" {
		t.Fatalf("resolvePinVersionForInit(explicit) = %q, want 2.0.1", got)
	}
}

func TestValidatePinnedReleaseVersion(t *testing.T) {
	origReleaseValidationHTTPClient := releaseValidationHTTPClient
	origReleaseValidationBaseURL := releaseValidationBaseURL
	t.Cleanup(func() {
		releaseValidationHTTPClient = origReleaseValidationHTTPClient
		releaseValidationBaseURL = origReleaseValidationBaseURL
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tag/v1.2.3":
			w.WriteHeader(http.StatusOK)
		case "/tag/v9.9.9":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	releaseValidationHTTPClient = server.Client()
	releaseValidationBaseURL = server.URL

	if err := validatePinnedReleaseVersion(context.Background(), "1.2.3"); err != nil {
		t.Fatalf("validatePinnedReleaseVersion success error: %v", err)
	}

	err := validatePinnedReleaseVersion(context.Background(), "9.9.9")
	if err == nil {
		t.Fatal("expected not-found error for missing release")
	}
	if !strings.Contains(err.Error(), "requested release v9.9.9 not found") {
		t.Fatalf("expected not-found message, got %v", err)
	}
}

func TestInitCmd_VersionLatestPinsResolvedRelease(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origResolveLatestPinVersion := resolveLatestPinVersion
	origReleaseValidationHTTPClient := releaseValidationHTTPClient
	origReleaseValidationBaseURL := releaseValidationBaseURL
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		resolveLatestPinVersion = origResolveLatestPinVersion
		releaseValidationHTTPClient = origReleaseValidationHTTPClient
		releaseValidationBaseURL = origReleaseValidationBaseURL
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tag/v2.1.0" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }
	resolveLatestPinVersion = func(context.Context, string) (string, error) {
		return "2.1.0", nil
	}
	releaseValidationHTTPClient = server.Client()
	releaseValidationBaseURL = server.URL
	runWizard = func(string, string) error { return nil }

	var pinned string
	installRun = func(root string, opts install.Options) error {
		pinned = opts.PinVersion
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--version", "latest"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init --version latest failed: %v", err)
	}
	if pinned != "2.1.0" {
		t.Fatalf("expected pinned version 2.1.0, got %q", pinned)
	}
}

func TestInitCmd_VersionValidationFailureBlocksInstall(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origReleaseValidationHTTPClient := releaseValidationHTTPClient
	origReleaseValidationBaseURL := releaseValidationBaseURL
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		releaseValidationHTTPClient = origReleaseValidationHTTPClient
		releaseValidationBaseURL = origReleaseValidationBaseURL
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }
	releaseValidationHTTPClient = server.Client()
	releaseValidationBaseURL = server.URL
	runWizard = func(string, string) error { return nil }

	installCalled := false
	installRun = func(string, install.Options) error {
		installCalled = true
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--version", "9.9.9"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected init to fail when requested release does not exist")
	}
	if !strings.Contains(err.Error(), "requested release v9.9.9 not found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if installCalled {
		t.Fatal("install should not run when release validation fails")
	}
}

func TestInitCmd_UpdateWarning(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origCheckForUpdate := checkForUpdate
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		checkForUpdate = origCheckForUpdate
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }
	installRun = func(string, install.Options) error { return nil }
	runWizard = func(string, string) error { return nil }
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil
	}

	cmd := newInitCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "Warning: update available") {
		t.Fatalf("expected update warning, got %q", output)
	}
	if !strings.Contains(output, "Homebrew: brew upgrade conn-castle/tap/agent-layer") {
		t.Fatalf("expected Homebrew upgrade command, got %q", output)
	}
	if !strings.Contains(output, "al upgrade plan") {
		t.Fatalf("expected upgrade plan guidance, got %q", output)
	}
	if !strings.Contains(output, "al upgrade --force") {
		t.Fatalf("expected --force safety note, got %q", output)
	}
}

func TestInitCmd_UpdateWarningDevBuild(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origCheckForUpdate := checkForUpdate
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		checkForUpdate = origCheckForUpdate
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }
	installRun = func(string, install.Options) error { return nil }
	runWizard = func(string, string) error { return nil }
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "dev", Latest: "2.0.0", CurrentIsDev: true}, nil
	}

	cmd := newInitCmd()
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "dev build") {
		t.Fatalf("expected dev build warning, got %q", output)
	}
}

func TestInitCmd_WizardPromptError(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origCheckForUpdate := checkForUpdate
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		checkForUpdate = origCheckForUpdate
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return true }
	installRun = func(string, install.Options) error { return nil }
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	// Set stdin that will cause promptYesNo to fail
	cmd.SetIn(strings.NewReader(""))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	// Should not error since empty stdin results in "no" response
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

func TestInitCmd_WizardPromptYes(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origCheckForUpdate := checkForUpdate
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		checkForUpdate = origCheckForUpdate
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return true }
	installRun = func(string, install.Options) error { return nil }
	wizardCalled := false
	runWizard = func(string, string) error {
		wizardCalled = true
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if !wizardCalled {
		t.Fatalf("expected wizard to be called")
	}
}

func TestInitCmd_UpdateWarningSkipped(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		envKey     string
		envValue   string
		shouldCall bool
	}{
		{
			name:       "Skip when --version is set",
			args:       []string{"--version", "1.2.3"},
			shouldCall: false,
		},
		{
			name:       "Skip when AL_VERSION is set",
			envKey:     dispatch.EnvVersionOverride,
			envValue:   "1.2.3",
			shouldCall: false,
		},
		{
			name:       "Skip when AL_NO_NETWORK is set",
			envKey:     dispatch.EnvNoNetwork,
			envValue:   "1",
			shouldCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origGetwd := getwd
			origIsTerminal := isTerminal
			origInstallRun := installRun
			origRunWizard := runWizard
			origCheckForUpdate := checkForUpdate
			origValidatePinnedReleaseVersionFunc := validatePinnedReleaseVersionFunc
			t.Cleanup(func() {
				getwd = origGetwd
				isTerminal = origIsTerminal
				installRun = origInstallRun
				runWizard = origRunWizard
				checkForUpdate = origCheckForUpdate
				validatePinnedReleaseVersionFunc = origValidatePinnedReleaseVersionFunc
			})

			tmpDir := t.TempDir()
			if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
				t.Fatal(err)
			}
			getwd = func() (string, error) { return tmpDir, nil }
			isTerminal = func() bool { return false }
			installRun = func(string, install.Options) error { return nil }
			runWizard = func(string, string) error { return nil }
			validatePinnedReleaseVersionFunc = func(context.Context, string) error { return nil }

			calls := 0
			checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
				calls++
				return update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil
			}

			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envValue)
			}

			cmd := newInitCmd()
			cmd.SetArgs(tt.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("init failed: %v", err)
			}

			if tt.shouldCall && calls == 0 {
				t.Fatal("expected update check to run")
			}
			if !tt.shouldCall && calls != 0 {
				t.Fatalf("expected update check to be skipped, got %d calls", calls)
			}
		})
	}
}

func TestInitCmd_StatAgentLayerErrorFailsFast(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	origRunWizard := runWizard
	origStat := statAgentLayerPath
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
		runWizard = origRunWizard
		statAgentLayerPath = origStat
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }

	installCalled := false
	installRun = func(string, install.Options) error {
		installCalled = true
		return nil
	}
	runWizard = func(string, string) error { return nil }

	statAgentLayerPath = func(string) (os.FileInfo, error) {
		return nil, &os.PathError{Op: "stat", Path: ".agent-layer", Err: os.ErrPermission}
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-wizard"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
	if installCalled {
		t.Fatal("expected installRun not to be called when stat fails")
	}
}

func TestInitCmd_AgentLayerIsFileErrors(t *testing.T) {
	origGetwd := getwd
	origIsTerminal := isTerminal
	origInstallRun := installRun
	t.Cleanup(func() {
		getwd = origGetwd
		isTerminal = origIsTerminal
		installRun = origInstallRun
	})

	tmpDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, ".agent-layer"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	getwd = func() (string, error) { return tmpDir, nil }
	isTerminal = func() bool { return false }
	installRun = func(string, install.Options) error {
		t.Fatal("installRun should not be called when .agent-layer is not a directory")
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-wizard"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exists but is not a directory") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "move or remove") {
		t.Fatalf("expected remediation guidance, got %v", err)
	}
}
