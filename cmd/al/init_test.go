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
		name                         string
		args                         []string
		isTerminal                   bool
		mockInstallErr               error
		mockWizardErr                error
		userInput                    string // for stdin
		wantErr                      bool
		wantInstall                  bool
		wantWizard                   bool
		wantOverwrite                bool // Expect install options overwrite
		wantForce                    bool // Expect install options force
		wantPromptOverwriteAll       bool
		wantPromptOverwriteMemoryAll bool
		wantPromptOverwrite          bool
		wantPromptDeleteAll          bool
		wantPromptDelete             bool
		checkErr                     func(error) bool
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
			name:       "Overwrite requires interactive if not forced",
			args:       []string{"--overwrite"},
			isTerminal: false,
			wantErr:    true,
			checkErr: func(err error) bool {
				return err.Error() == "init overwrite prompts require an interactive terminal; re-run with --force to overwrite without prompts"
			},
		},
		{
			name:          "Force works non-interactive",
			args:          []string{"--force"},
			isTerminal:    false,
			wantInstall:   true,
			wantOverwrite: true,
			wantForce:     true,
		},
		{
			name:                         "Overwrite interactive",
			args:                         []string{"--overwrite"},
			isTerminal:                   true,
			userInput:                    "y\nn\ny\nn\n", // OverwriteAll managed (y), OverwriteAll memory (n), DeleteAll (y), Wizard (n)
			wantInstall:                  true,
			wantOverwrite:                true,
			wantForce:                    false,
			wantWizard:                   false,
			wantPromptOverwriteAll:       true,
			wantPromptOverwriteMemoryAll: false,
			wantPromptOverwrite:          true,
			wantPromptDeleteAll:          true,
			wantPromptDelete:             true,
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
		{
			name:                         "Prompt Overwrite Callback Yes",
			args:                         []string{"--overwrite"},
			isTerminal:                   true,
			userInput:                    "n\nn\ny\ny\nn\n", // OverwriteAll managed (n), OverwriteAll memory (n), Overwrite (y), DeleteAll (y), Wizard (n)
			wantInstall:                  true,
			wantOverwrite:                true,
			wantForce:                    false,
			wantWizard:                   false,
			wantPromptOverwriteAll:       false,
			wantPromptOverwriteMemoryAll: false,
			wantPromptOverwrite:          true,
			wantPromptDeleteAll:          true,
			wantPromptDelete:             true,
		}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup temp dir as root
			tmpDir := t.TempDir()
			// Create .git to make it a valid root
			if err := os.Mkdir(filepath.Join(tmpDir, ".git"), 0755); err != nil {
				t.Fatal(err)
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
				if opts.Overwrite != (tt.wantOverwrite || tt.wantForce) {
					t.Errorf("installRun opts.Overwrite = %v, want %v", opts.Overwrite, tt.wantOverwrite || tt.wantForce)
				}
				if opts.Force != tt.wantForce {
					t.Errorf("installRun opts.Force = %v, want %v", opts.Force, tt.wantForce)
				}

				if tt.wantOverwrite && !tt.wantForce {
					if opts.Prompter == nil {
						t.Error("Expected Prompter to be set")
					} else {
						yes, err := opts.Prompter.OverwriteAll([]string{"managed"})
						if err != nil {
							t.Errorf("OverwriteAll error: %v", err)
						}
						if yes != tt.wantPromptOverwriteAll {
							t.Errorf("OverwriteAll returned %v, want %v", yes, tt.wantPromptOverwriteAll)
						}
						yes, err = opts.Prompter.OverwriteAllMemory([]string{"docs/agent-layer/ISSUES.md"})
						if err != nil {
							t.Errorf("OverwriteAllMemory error: %v", err)
						}
						if yes != tt.wantPromptOverwriteMemoryAll {
							t.Errorf("OverwriteAllMemory returned %v, want %v", yes, tt.wantPromptOverwriteMemoryAll)
						}
						if !tt.wantPromptOverwriteAll {
							overwrite, err := opts.Prompter.Overwrite("testfile")
							if err != nil {
								t.Errorf("Overwrite error: %v", err)
							}
							if overwrite != tt.wantPromptOverwrite {
								t.Errorf("Overwrite returned %v, want %v", overwrite, tt.wantPromptOverwrite)
							}
						}
						deleteAll, err := opts.Prompter.DeleteUnknownAll([]string{"unknown"})
						if err != nil {
							t.Errorf("DeleteUnknownAll error: %v", err)
						}
						if deleteAll != tt.wantPromptDeleteAll {
							t.Errorf("DeleteUnknownAll returned %v, want %v", deleteAll, tt.wantPromptDeleteAll)
						}
						if !deleteAll {
							deletePath, err := opts.Prompter.DeleteUnknown("unknown")
							if err != nil {
								t.Errorf("DeleteUnknown error: %v", err)
							}
							if deletePath != tt.wantPromptDelete {
								t.Errorf("DeleteUnknown returned %v, want %v", deletePath, tt.wantPromptDelete)
							}
						}
					}
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

			// If we expect install failure, installCalled is true.
			// If we expect resolve error, installCalled is false.
			if tt.name == "Resolve Root Error" || tt.name == "Resolve Pin Version Error" {
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
	if !strings.Contains(output, "al init --force") {
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

func TestInitCmd_OverwriteAllMemoryPromptError(t *testing.T) {
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
	installRun = func(root string, opts install.Options) error {
		// Call OverwriteAllMemory with paths to trigger the display code
		if opts.Prompter != nil {
			paths := []string{"docs/agent-layer/ISSUES.md"}
			_, _ = opts.Prompter.OverwriteAllMemory(paths)
		}
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	// Provide enough input for all prompts
	cmd.SetIn(&slowReader{r: strings.NewReader("n\nn\nn\nn\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

func TestInitCmd_DeleteUnknownAllPromptWithPaths(t *testing.T) {
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
	installRun = func(root string, opts install.Options) error {
		// Call DeleteUnknownAll with paths to trigger the display code
		if opts.Prompter != nil {
			paths := []string{"unknown1.txt", "unknown2.txt"}
			_, _ = opts.Prompter.DeleteUnknownAll(paths)
		}
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	// Provide enough input for all prompts
	cmd.SetIn(&slowReader{r: strings.NewReader("n\nn\nn\nn\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

func TestInitCmd_OverwritePromptFuncCallback(t *testing.T) {
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
	installRun = func(root string, opts install.Options) error {
		// Call Overwrite to trigger that code path
		if opts.Prompter != nil {
			_, _ = opts.Prompter.Overwrite("test-file.md")
		}
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	// Provide enough input
	cmd.SetIn(&slowReader{r: strings.NewReader("n\nn\nn\nn\nn\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

func TestInitCmd_DeleteUnknownFuncCallback(t *testing.T) {
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
	installRun = func(root string, opts install.Options) error {
		// Call DeleteUnknown to trigger that code path
		if opts.Prompter != nil {
			_, _ = opts.Prompter.DeleteUnknown("unknown-file.txt")
		}
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	// Provide enough input
	cmd.SetIn(&slowReader{r: strings.NewReader("n\nn\nn\nn\nn\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
}

func TestInitCmd_OverwriteAllManagedWithPaths(t *testing.T) {
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
	installRun = func(root string, opts install.Options) error {
		// Call OverwriteAll with paths to trigger the display code
		if opts.Prompter != nil {
			paths := []string{".agent-layer/config.toml", ".agent-layer/commands.allow"}
			_, _ = opts.Prompter.OverwriteAll(paths)
		}
		return nil
	}
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	// Provide input for prompts
	cmd.SetIn(&slowReader{r: strings.NewReader("y\nn\n")})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	// Just ensure no error - output verification is already done above
}

// errWriter returns an error on all write operations.
type errWriter struct {
	err error
}

func (e *errWriter) Write([]byte) (int, error) {
	return 0, e.err
}

func TestInitCmd_OverwriteAllFprintlnError(t *testing.T) {
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
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	writeErr := fmt.Errorf("write error")
	installRun = func(root string, opts install.Options) error {
		if opts.Prompter == nil {
			return fmt.Errorf("expected prompter")
		}
		// Call with paths to trigger fmt.Fprintln
		_, err := opts.Prompter.OverwriteAll([]string{"file1.txt"})
		if err == nil {
			t.Errorf("expected error from OverwriteAll")
		}
		if err != writeErr {
			t.Errorf("expected write error, got %v", err)
		}
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
	cmd.SetOut(&errWriter{err: writeErr})

	_ = cmd.Execute()
}

func TestInitCmd_OverwriteAllHeaderError(t *testing.T) {
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
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	// Writer that fails on second write (header line)
	callCount := 0
	writeErr := fmt.Errorf("header write error")
	installRun = func(root string, opts install.Options) error {
		if opts.Prompter == nil {
			return fmt.Errorf("expected prompter")
		}
		_, err := opts.Prompter.OverwriteAll([]string{"file1.txt"})
		if err == nil {
			t.Errorf("expected error from OverwriteAll")
		}
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
	cmd.SetOut(&limitedWriter{n: 1, err: writeErr, callCount: &callCount})

	_ = cmd.Execute()
}

func TestInitCmd_OverwriteAllPathError(t *testing.T) {
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
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	// Writer that fails on third write (path line)
	callCount := 0
	writeErr := fmt.Errorf("path write error")
	installRun = func(root string, opts install.Options) error {
		if opts.Prompter == nil {
			return fmt.Errorf("expected prompter")
		}
		_, err := opts.Prompter.OverwriteAll([]string{"file1.txt"})
		if err == nil {
			t.Errorf("expected error from OverwriteAll")
		}
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
	cmd.SetOut(&limitedWriter{n: 2, err: writeErr, callCount: &callCount})

	_ = cmd.Execute()
}

func TestInitCmd_OverwriteAllTrailingNewlineError(t *testing.T) {
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
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
	}

	// Writer that fails on fourth write (trailing newline)
	callCount := 0
	writeErr := fmt.Errorf("trailing newline error")
	installRun = func(root string, opts install.Options) error {
		if opts.Prompter == nil {
			return fmt.Errorf("expected prompter")
		}
		_, err := opts.Prompter.OverwriteAll([]string{"file1.txt"})
		if err == nil {
			t.Errorf("expected error from OverwriteAll")
		}
		return nil
	}

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--overwrite"})
	cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
	cmd.SetOut(&limitedWriter{n: 3, err: writeErr, callCount: &callCount})

	_ = cmd.Execute()
}

func TestInitCmd_OverwriteAllMemoryFprintlnErrors(t *testing.T) {
	tests := []struct {
		name       string
		failAfterN int
	}{
		{"first newline", 0},
		{"header", 1},
		{"path", 2},
		{"trailing newline", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
				return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
			}

			callCount := 0
			writeErr := fmt.Errorf("write error at %d", tt.failAfterN)
			installRun = func(root string, opts install.Options) error {
				if opts.Prompter == nil {
					return fmt.Errorf("expected prompter")
				}
				_, err := opts.Prompter.OverwriteAllMemory([]string{"docs/ISSUES.md"})
				if err == nil {
					t.Errorf("expected error from OverwriteAllMemory")
				}
				return nil
			}

			cmd := newInitCmd()
			cmd.SetArgs([]string{"--overwrite"})
			cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
			cmd.SetOut(&limitedWriter{n: tt.failAfterN, err: writeErr, callCount: &callCount})

			_ = cmd.Execute()
		})
	}
}

func TestInitCmd_DeleteUnknownAllFprintlnErrors(t *testing.T) {
	tests := []struct {
		name       string
		failAfterN int
	}{
		{"first newline", 0},
		{"header", 1},
		{"path", 2},
		{"trailing newline", 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
				return update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil
			}

			callCount := 0
			writeErr := fmt.Errorf("write error at %d", tt.failAfterN)
			installRun = func(root string, opts install.Options) error {
				if opts.Prompter == nil {
					return fmt.Errorf("expected prompter")
				}
				_, err := opts.Prompter.DeleteUnknownAll([]string{"unknown.txt"})
				if err == nil {
					t.Errorf("expected error from DeleteUnknownAll")
				}
				return nil
			}

			cmd := newInitCmd()
			cmd.SetArgs([]string{"--overwrite"})
			cmd.SetIn(&slowReader{r: strings.NewReader("y\n")})
			cmd.SetOut(&limitedWriter{n: tt.failAfterN, err: writeErr, callCount: &callCount})

			_ = cmd.Execute()
		})
	}
}

// limitedWriter writes successfully n times, then returns err.
type limitedWriter struct {
	n         int
	err       error
	callCount *int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	*l.callCount++
	if *l.callCount > l.n {
		return 0, l.err
	}
	return len(p), nil
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
