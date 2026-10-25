package wizard

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestWizardAndProfileRefuseBeforeAnyMutationOrPrompt(t *testing.T) {
	for _, scenario := range []string{"pristine skill", "retired model without enabled"} {
		for _, kind := range []string{"wizard", "profile preview", "profile apply", "locked apply"} {
			t.Run(scenario+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
				if scenario == "pristine skill" {
					skill := filepath.Join(root, ".agent-layer/skills/ship-pr")
					require.NoError(t, os.MkdirAll(skill, 0o750))
					require.NoError(t, os.WriteFile(filepath.Join(skill, "SKILL.md"), testutil.TransitionFixture(t, "ship-pr.md"), 0o600))
					// Missing config proves the gate precedes ensureWizardConfig and profile install.
					require.NoError(t, os.Remove(config.DefaultPaths(root).ConfigPath))
				} else {
					raw := []byte("[approvals]\nmode=\"all\"\n[agents.antigravity.agent_specific]\nmodel=\"Gemini 3.1 Pro (High)\"\n")
					require.NoError(t, os.WriteFile(config.DefaultPaths(root).ConfigPath, raw, 0o600))
				}
				stage := filepath.Join(root, ".agent-layer/instructions-imported/.staging")
				require.NoError(t, os.MkdirAll(stage, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(stage, "journal.json"), []byte(`{"version":1,"committed":true}`), 0o600))
				before := testutil.SnapshotEvidence(t, root)
				runSync := func(context.Context, string) (*alsync.Result, error) {
					t.Fatal("refusal reached sync")
					return nil, nil
				}
				var err error
				switch kind {
				case "wizard":
					err = RunWithWriter(t.Context(), root, &ScriptedUI{}, runSync, "1.1.0", io.Discard)
				case "locked apply":
					err = applyChanges(t.Context(), root, config.DefaultPaths(root).ConfigPath, filepath.Join(root, ".agent-layer/.env"), &Choices{}, runSync, io.Discard)
				default:
					err = RunProfile(t.Context(), root, runSync, "1.1.0", filepath.Join(root, "missing-profile.toml"), kind == "profile apply", io.Discard)
				}
				require.ErrorIs(t, err, install.ErrUnfinishedTransition)
				require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			})
		}
	}
}

func TestFreshWizardAndCompletedProfileKeepInstructionOrdering(t *testing.T) {
	for _, kind := range []string{"fresh wizard", "completed profile"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
			paths := config.DefaultPaths(root)
			for _, name := range []string{"00_rules.md", "01_memory.md"} {
				require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, name), []byte("custom "+name), 0o600))
			}
			raw, err := os.ReadFile(paths.ConfigPath)
			require.NoError(t, err)
			before := testutil.SnapshotEvidence(t, paths.InstructionsDir)
			runSync := func(context.Context, string) (*alsync.Result, error) { return &alsync.Result{}, nil }
			if kind == "fresh wizard" {
				ui := &MockUI{
					SelectFunc: func(title string, _ []string, selected *string) error {
						if title == messages.WizardInstructionSetTitle {
							*selected = instructionSetLabels()[0]
						}
						return nil
					},
					MultiSelectFunc: func(_ string, _ []string, selected *[]string) error { *selected = nil; return nil },
					ConfirmFunc:     func(title string, value *bool) error { *value = title == messages.WizardApplyChangesPrompt; return nil },
				}
				require.NoError(t, RunAfterFreshInitWithWriter(t.Context(), root, ui, runSync, "1.1.0", io.Discard))
			} else {
				profile := filepath.Join(t.TempDir(), "profile.toml")
				require.NoError(t, os.WriteFile(profile, raw, 0o600)) // #nosec G703 -- fixture is rooted in t.TempDir.
				require.NoError(t, RunProfile(t.Context(), root, runSync, "1.1.0", profile, true, io.Discard))
			}
			cfg, err := config.LoadConfigLenient(paths.ConfigPath)
			require.NoError(t, err)
			require.Len(t, cfg.Instructions.Local, 2)
			require.Equal(t, 0, *cfg.Instructions.Local[0].Order)
			require.Equal(t, 10, *cfg.Instructions.Local[1].Order)
			require.Equal(t, before, testutil.SnapshotEvidence(t, paths.InstructionsDir))
		})
	}
}
