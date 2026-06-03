package wizard

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestRunWithWriter_AdditionalBranches(t *testing.T) {
	t.Run("nil writer falls back and exits on wizard back", func(t *testing.T) {
		root := t.TempDir()
		ui := &MockUI{
			ConfirmFunc: func(string, *bool) error {
				return errWizardBack
			},
		}
		err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) {
			return &alsync.Result{}, nil
		}, "", nil)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("confirmAndApply wizard-back exits without changes", func(t *testing.T) {
		root := t.TempDir()
		setupRepo(t, root)
		configDir := filepath.Join(root, ".agent-layer")
		if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}

		ui := &MockUI{
			SelectFunc:      func(string, []string, *string) error { return nil },
			MultiSelectFunc: func(string, []string, *[]string) error { return nil },
			ConfirmFunc:     func(string, *bool) error { return nil },
			NoteFunc: func(title, body string) error {
				if title == messages.WizardSummaryTitle {
					return errWizardBack
				}
				return nil
			},
		}
		err := RunWithWriter(root, ui, func(string) (*alsync.Result, error) {
			return &alsync.Result{}, nil
		}, "", io.Discard)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}

func TestRunAfterFreshInitWithWriter_UsesFreshStatuslineDefaults(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	configText := strings.ReplaceAll(basicAgentConfig(), "[agents.claude]\nenabled = false", "[agents.claude]\nenabled = true")
	configText = strings.ReplaceAll(configText, "[agents.codex]\nenabled = false", "[agents.codex]\nenabled = true")
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configText), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), nil, 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	sawClaudeStatuslineDefault := false
	sawCodexStatuslineDefault := false
	ui := &MockUI{
		SelectFunc: func(string, []string, *string) error { return nil },
		MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
			for _, label := range *selected {
				if title == messages.WizardClaudeFeaturesTitle && label == messages.WizardClaudeFeatureStatuslineLabel {
					sawClaudeStatuslineDefault = true
				}
				if title == messages.WizardCodexFeaturesTitle && label == messages.WizardCodexFeatureStatuslineLabel {
					sawCodexStatuslineDefault = true
				}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = false
			}
			return nil
		},
		NoteFunc: func(string, string) error { return nil },
	}

	err := RunAfterFreshInitWithWriter(root, ui, func(string) (*alsync.Result, error) {
		t.Fatal("sync should not run when apply is declined")
		return nil, nil
	}, "", io.Discard)
	if err != nil {
		t.Fatalf("RunAfterFreshInitWithWriter: %v", err)
	}
	// Status lines are opt-in: the fresh-init wizard must NOT preselect them, so
	// accepting the defaults leaves both disabled until the user explicitly checks
	// the toggle. Mirrors the non-interactive upgrade default (migration value: false).
	if sawClaudeStatuslineDefault {
		t.Fatal("fresh init wrapper should not preselect Claude statusline (opt-in)")
	}
	if sawCodexStatuslineDefault {
		t.Fatal("fresh init wrapper should not preselect Codex statusline (opt-in)")
	}
}

func TestEnsureWizardConfig_StatErrorBranch(t *testing.T) {
	ui := &MockUI{}
	_, _, err := ensureWizardConfig(t.TempDir(), "bad\x00config.toml", ui, "", io.Discard)
	if err == nil {
		t.Fatal("expected stat error for invalid config path")
	}
}

func TestInitializeChoices_AdditionalBranches(t *testing.T) {
	claudeLocal := true
	disableQuestion := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					LocalConfigDir:      &claudeLocal,
					DisableQuestionTool: &disableQuestion,
				},
			},
		},
		Root: t.TempDir(),
	}
	choices, err := initializeChoices(cfg)
	if err != nil {
		t.Fatalf("initializeChoices: %v", err)
	}
	if choices.ApprovalMode != config.ApprovalModeAll {
		t.Fatalf("expected default approval mode %q, got %q", config.ApprovalModeAll, choices.ApprovalMode)
	}
	if !choices.ClaudeLocalConfigDir {
		t.Fatal("expected claude local config dir to be loaded from config")
	}
	// The typed disable_question_tool flag must read back so re-running the
	// wizard defaults the prompt to Yes.
	if !choices.ClaudeDisableQuestionTool {
		t.Fatal("expected disable_question_tool to be loaded from config")
	}
}

func TestPromptWizardAndHelpers_ErrorBranches(t *testing.T) {
	t.Run("promptWizardFlow first-step confirm error", func(t *testing.T) {
		choices := NewChoices()
		choices.ApprovalMode = config.ApprovalModeAll
		ui := &MockUI{
			SelectFunc: func(string, []string, *string) error {
				return errWizardBack
			},
			ConfirmFunc: func(string, *bool) error {
				return errors.New("confirm failed")
			},
		}
		err := promptWizardFlow(t.TempDir(), ui, choices)
		if err == nil || !strings.Contains(err.Error(), "confirm failed") {
			t.Fatalf("expected confirm error, got %v", err)
		}
	})

	t.Run("promptApprovalMode unknown mode", func(t *testing.T) {
		err := promptApprovalMode(&MockUI{}, &Choices{ApprovalMode: "bogus"})
		if err == nil || !strings.Contains(err.Error(), "unknown approval mode") {
			t.Fatalf("expected unknown mode error, got %v", err)
		}
	})

	t.Run("confirmWizardExitOnFirstStepEscape back returns false,nil", func(t *testing.T) {
		exit, err := confirmWizardExitOnFirstStepEscape(&MockUI{
			ConfirmFunc: func(string, *bool) error { return errWizardBack },
		})
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if exit {
			t.Fatal("expected exit=false on wizard back")
		}
	})

	t.Run("confirmWizardExitOnFirstStepEscape other error", func(t *testing.T) {
		exit, err := confirmWizardExitOnFirstStepEscape(&MockUI{
			ConfirmFunc: func(string, *bool) error { return errors.New("confirm failed") },
		})
		if err == nil || !strings.Contains(err.Error(), "confirm failed") {
			t.Fatalf("expected confirm error, got %v", err)
		}
		if exit {
			t.Fatal("expected exit=false on error")
		}
	})

	t.Run("promptModels claude local config confirm error", func(t *testing.T) {
		choices := NewChoices()
		choices.EnabledAgents[AgentClaude] = true
		err := promptModels(&MockUI{
			SelectFunc: func(string, []string, *string) error { return nil },
			ConfirmFunc: func(string, *bool) error {
				return errors.New("confirm failed")
			},
		}, choices)
		if err == nil || !strings.Contains(err.Error(), "confirm failed") {
			t.Fatalf("expected confirm error, got %v", err)
		}
	})

	t.Run("promptModels codex apps multi-select inverts enabled state", func(t *testing.T) {
		choices := NewChoices()
		choices.EnabledAgents[AgentCodex] = true
		choices.CodexApps = true // apps currently enabled
		var appsPreChecked bool
		err := promptModels(&MockUI{
			SelectFunc: func(string, []string, *string) error { return nil },
			MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
				if title == messages.WizardCodexFeaturesTitle {
					// Enabled apps must arrive pre-checked.
					for _, label := range *selected {
						if label == messages.WizardCodexFeatureAppsLabel {
							appsPreChecked = true
						}
					}
					// User unchecks apps (drops the label) to disable them.
					*selected = []string{messages.WizardCodexFeatureBrowserLabel}
				}
				return nil
			},
		}, choices)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Apps enabled => the checkbox is pre-selected.
		if !appsPreChecked {
			t.Fatal("expected the apps checkbox to be pre-selected when apps are enabled")
		}
		// Removing the apps label flips CodexApps to enabled-state false.
		if choices.CodexApps {
			t.Fatal("expected CodexApps to be disabled after unchecking the apps label")
		}
		if !choices.CodexAppsTouched {
			t.Fatal("expected CodexAppsTouched to be set")
		}
	})

	t.Run("promptModels codex features multi-select error", func(t *testing.T) {
		choices := NewChoices()
		choices.EnabledAgents[AgentCodex] = true
		err := promptModels(&MockUI{
			SelectFunc: func(string, []string, *string) error { return nil },
			MultiSelectFunc: func(title string, _ []string, _ *[]string) error {
				if title == messages.WizardCodexFeaturesTitle {
					return errors.New("codex features failed")
				}
				return nil
			},
		}, choices)
		if err == nil || !strings.Contains(err.Error(), "codex features failed") {
			t.Fatalf("expected codex features error, got %v", err)
		}
	})
}

func TestConfirmAndApply_ErrorBranches(t *testing.T) {
	t.Run("rewrite preview error", func(t *testing.T) {
		err := confirmAndApply(
			t.TempDir(),
			filepath.Join(t.TempDir(), "missing-config.toml"),
			filepath.Join(t.TempDir(), ".env"),
			&MockUI{NoteFunc: func(string, string) error { return nil }},
			NewChoices(),
			func(string) (*alsync.Result, error) { return &alsync.Result{}, nil },
			io.Discard,
			true,
		)
		if err == nil {
			t.Fatal("expected rewrite preview error")
		}
	})

	t.Run("rewrite preview note error", func(t *testing.T) {
		root := t.TempDir()
		setupRepo(t, root)
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}
		err := confirmAndApply(
			root,
			configPath,
			envPath,
			&MockUI{
				NoteFunc: func(title, body string) error {
					if title == messages.WizardRewritePreviewTitle {
						return errors.New("note failed")
					}
					return nil
				},
			},
			NewChoices(),
			func(string) (*alsync.Result, error) { return &alsync.Result{}, nil },
			io.Discard,
			true,
		)
		if err == nil || !strings.Contains(err.Error(), "note failed") {
			t.Fatalf("expected rewrite-note error, got %v", err)
		}
	})
}

func TestPromptSecrets_ExistingSecretBranch(t *testing.T) {
	root := t.TempDir()
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{
		{ID: "server", RequiredEnv: []string{"AL_TOKEN"}},
	}
	choices.EnabledMCPServers["server"] = true
	choices.Secrets["AL_TOKEN"] = "already-set"

	err := promptSecrets(root, &MockUI{
		SecretInputFunc: func(string, *string) error {
			return errors.New("should not prompt for existing secret")
		},
	}, choices)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestBuildRewritePreview_ErrorAndNoDiffBranches(t *testing.T) {
	t.Run("config read error", func(t *testing.T) {
		_, err := buildRewritePreview(filepath.Join(t.TempDir(), "missing.toml"), filepath.Join(t.TempDir(), ".env"), NewChoices())
		if err == nil {
			t.Fatal("expected config read error")
		}
	})

	t.Run("patch config error", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.toml")
		envPath := filepath.Join(root, ".env")
		if err := os.WriteFile(configPath, []byte("[approvals"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}
		_, err := buildRewritePreview(configPath, envPath, NewChoices())
		if err == nil || !strings.Contains(err.Error(), "failed to patch config") {
			t.Fatalf("expected patch-config error, got %v", err)
		}
	})

	t.Run("env read non-notexist error", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, "config.toml")
		envPath := filepath.Join(root, ".env")
		if err := os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.MkdirAll(envPath, 0o700); err != nil {
			t.Fatalf("mkdir env path: %v", err)
		}
		_, err := buildRewritePreview(configPath, envPath, NewChoices())
		if err == nil {
			t.Fatal("expected env read error")
		}
	})

	t.Run("no rewrites needed", func(t *testing.T) {
		root := t.TempDir()
		setupRepo(t, root)
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		envPath := filepath.Join(root, ".agent-layer", ".env")
		templateConfig, err := templates.Read("config.toml")
		if err != nil {
			t.Fatalf("read config template: %v", err)
		}
		if err := os.WriteFile(configPath, templateConfig, 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(envPath, []byte(""), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}

		cfg, err := loadProjectConfigFunc(root)
		if err != nil {
			t.Fatalf("loadProjectConfig: %v", err)
		}
		choices, err := initializeChoices(cfg)
		if err != nil {
			t.Fatalf("initializeChoices: %v", err)
		}
		canonicalConfig, err := PatchConfig(string(templateConfig), choices)
		if err != nil {
			t.Fatalf("PatchConfig: %v", err)
		}
		if err := os.WriteFile(configPath, []byte(canonicalConfig), 0o600); err != nil {
			t.Fatalf("rewrite canonical config: %v", err)
		}

		preview, err := buildRewritePreview(configPath, envPath, choices)
		if err != nil {
			t.Fatalf("buildRewritePreview: %v", err)
		}
		if !strings.Contains(preview, "No rewrites needed") {
			t.Fatalf("expected no-diff message, got %q", preview)
		}
	})

	t.Run("env preview redacts secret values", func(t *testing.T) {
		root := t.TempDir()
		setupRepo(t, root)
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("AL_TOKEN=old-secret\nUNCHANGED=same-secret\n"), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}
		choices := NewChoices()
		choices.Secrets["AL_TOKEN"] = "new-secret"

		preview, err := buildRewritePreview(configPath, envPath, choices)
		if err != nil {
			t.Fatalf("buildRewritePreview: %v", err)
		}
		for _, forbidden := range []string{"old-secret", "new-secret", "same-secret"} {
			if strings.Contains(preview, forbidden) {
				t.Fatalf("preview leaked secret %q:\n%s", forbidden, preview)
			}
		}
		for _, expected := range []string{"<redacted current>", "<redacted proposed>", "Secret values are redacted"} {
			if !strings.Contains(preview, expected) {
				t.Fatalf("expected %q in redacted preview:\n%s", expected, preview)
			}
		}
	})

	t.Run("env preview redacts parser-missing assignments and keeps quoted comments", func(t *testing.T) {
		content := "MISSING=super-secret\nQUOTED=\"old-secret\" # keep me\nUNQUOTED=old-secret # not a comment\n"
		values := map[string]string{"QUOTED": "old-secret", "UNQUOTED": "old-secret # not a comment"}
		redacted := redactEnvPreviewSide(content, values, nil, true)

		for _, forbidden := range []string{"super-secret", "old-secret"} {
			if strings.Contains(redacted, forbidden) {
				t.Fatalf("redacted preview leaked %q:\n%s", forbidden, redacted)
			}
		}
		for _, expected := range []string{`MISSING=""`, `QUOTED="<redacted current>" # keep me`, `UNQUOTED="<redacted current>"`} {
			if !strings.Contains(redacted, expected) {
				t.Fatalf("expected %q in redacted preview:\n%s", expected, redacted)
			}
		}
	})
}
