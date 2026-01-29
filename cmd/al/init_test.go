package main

// NOTE: Tests in this file mutate package-level globals (getwd, isTerminal,
// installRun, runWizard, checkForUpdate). Do not use t.Parallel() at the
// top level. Each test must restore globals via t.Cleanup().

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
					if opts.PromptOverwriteAll == nil {
						t.Error("Expected PromptOverwriteAll to be set")
					} else {
						yes, err := opts.PromptOverwriteAll([]string{"managed"})
						if err != nil {
							t.Errorf("PromptOverwriteAll error: %v", err)
						}
						if yes != tt.wantPromptOverwriteAll {
							t.Errorf("PromptOverwriteAll returned %v, want %v", yes, tt.wantPromptOverwriteAll)
						}
					}
					if opts.PromptOverwriteMemoryAll == nil {
						t.Error("Expected PromptOverwriteMemoryAll to be set")
					} else {
						yes, err := opts.PromptOverwriteMemoryAll([]string{"docs/agent-layer/ISSUES.md"})
						if err != nil {
							t.Errorf("PromptOverwriteMemoryAll error: %v", err)
						}
						if yes != tt.wantPromptOverwriteMemoryAll {
							t.Errorf("PromptOverwriteMemoryAll returned %v, want %v", yes, tt.wantPromptOverwriteMemoryAll)
						}
					}
					if !tt.wantPromptOverwriteAll {
						if opts.PromptOverwrite == nil {
							t.Error("Expected PromptOverwrite to be set")
						} else {
							overwrite, err := opts.PromptOverwrite("testfile")
							if err != nil {
								t.Errorf("PromptOverwrite error: %v", err)
							}
							if overwrite != tt.wantPromptOverwrite {
								t.Errorf("PromptOverwrite returned %v, want %v", overwrite, tt.wantPromptOverwrite)
							}
						}
					}
					if opts.PromptDeleteUnknownAll == nil {
						t.Error("Expected PromptDeleteUnknownAll to be set")
					} else {
						deleteAll, err := opts.PromptDeleteUnknownAll([]string{"unknown"})
						if err != nil {
							t.Errorf("PromptDeleteUnknownAll error: %v", err)
						}
						if deleteAll != tt.wantPromptDeleteAll {
							t.Errorf("PromptDeleteUnknownAll returned %v, want %v", deleteAll, tt.wantPromptDeleteAll)
						}
						if !deleteAll {
							if opts.PromptDeleteUnknown == nil {
								t.Error("Expected PromptDeleteUnknown to be set")
							} else {
								deletePath, err := opts.PromptDeleteUnknown("unknown")
								if err != nil {
									t.Errorf("PromptDeleteUnknown error: %v", err)
								}
								if deletePath != tt.wantPromptDelete {
									t.Errorf("PromptDeleteUnknown returned %v, want %v", deletePath, tt.wantPromptDelete)
								}
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
