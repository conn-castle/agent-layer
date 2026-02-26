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
		if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o644); err != nil {
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

func TestEnsureWizardConfig_StatErrorBranch(t *testing.T) {
	ui := &MockUI{}
	_, err := ensureWizardConfig(t.TempDir(), "bad\x00config.toml", ui, "", io.Discard)
	if err == nil {
		t.Fatal("expected stat error for invalid config path")
	}
}

func TestInitializeChoices_AdditionalBranches(t *testing.T) {
	claudeLocal := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					LocalConfigDir: &claudeLocal,
				},
			},
		},
		Root: t.TempDir(),
	}
	choices, err := initializeChoices(cfg)
	if err != nil {
		t.Fatalf("initializeChoices: %v", err)
	}
	if choices.ApprovalMode != ApprovalAll {
		t.Fatalf("expected default approval mode %q, got %q", ApprovalAll, choices.ApprovalMode)
	}
	if !choices.ClaudeLocalConfigDir {
		t.Fatal("expected claude local config dir to be loaded from config")
	}
}

func TestPromptWizardAndHelpers_ErrorBranches(t *testing.T) {
	t.Run("promptWizardFlow first-step confirm error", func(t *testing.T) {
		cfg := &config.ProjectConfig{Config: config.Config{}}
		choices := NewChoices()
		choices.ApprovalMode = ApprovalAll
		ui := &MockUI{
			SelectFunc: func(string, []string, *string) error {
				return errWizardBack
			},
			ConfirmFunc: func(string, *bool) error {
				return errors.New("confirm failed")
			},
		}
		err := promptWizardFlow(t.TempDir(), ui, cfg, choices)
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
		if err := os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644); err != nil {
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
		if err := os.WriteFile(configPath, []byte("[approvals"), 0o644); err != nil {
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
		if err := os.WriteFile(configPath, []byte(basicAgentConfig()), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.MkdirAll(envPath, 0o755); err != nil {
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
		if err := os.WriteFile(configPath, templateConfig, 0o644); err != nil {
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
		if err := os.WriteFile(configPath, []byte(canonicalConfig), 0o644); err != nil {
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
}
