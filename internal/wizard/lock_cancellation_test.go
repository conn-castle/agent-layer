package wizard

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestRunProfileCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte(basicAgentConfig()), 0o600))
	profilePath := filepath.Join(root, "profile.toml")
	profile := strings.ReplaceAll(basicAgentConfig(), `mode = "none"`, `mode = "all"`)
	require.NoError(t, os.WriteFile(profilePath, []byte(profile), 0o600))
	var syncCalled atomic.Bool
	testutil.AssertCanceledLockWaitPreservesProject(t, root, func(ctx context.Context) error {
		return RunProfile(ctx, root, func(context.Context, string) (*alsync.Result, error) {
			syncCalled.Store(true)
			return &alsync.Result{}, nil
		}, "", profilePath, true, io.Discard)
	})
	require.False(t, syncCalled.Load())
}

func TestWizardCancellationStopsProjectLockWait(t *testing.T) {
	for _, mode := range []string{"answers", "interactive", "after init"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			setupRepo(t, root)
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte(basicAgentConfig()), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_EXAMPLE=before\n"), 0o600))
			var ui UI = &MockUI{}
			if mode == "answers" {
				label, _ := approvalModeLabelForValue(config.ApprovalModeAll)
				ui = &ScriptedUI{used: map[string]struct{}{}, answers: scriptedAnswers{
					Select: map[string]string{messages.WizardInstructionSetTitle: instructionSetLabels()[0], messages.WizardApprovalModeTitle: label},
					MultiSelect: map[string][]string{
						messages.WizardEnableAgentsTitle: {}, messages.WizardGitTrackingTitle: {},
						messages.WizardEnableCLISkillsTitle: {}, messages.WizardEnableDefaultMCPServersTitle: {},
					},
					Confirm: map[string]bool{messages.WizardEnableWarningsPrompt: false, messages.WizardApplyChangesPrompt: true},
				}}
			}
			run := RunWithWriter
			if mode == "after init" {
				run = RunAfterFreshInitWithWriter
			}
			var syncCalled atomic.Bool
			testutil.AssertCanceledLockWaitPreservesProject(t, root, func(ctx context.Context) error {
				return run(ctx, root, ui, func(context.Context, string) (*alsync.Result, error) {
					syncCalled.Store(true)
					return &alsync.Result{}, nil
				}, "", io.Discard)
			})
			require.False(t, syncCalled.Load())
		})
	}
}

func TestInitializeCatalogChoicesCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	choices := NewChoices()
	var err error
	choices.CLISkillsCatalog, err = templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	testutil.AssertCanceledLockWaitPreservesProject(t, root, func(ctx context.Context) error {
		return initializeCatalogChoices(ctx, root, choices)
	})
}

func TestApplyChangesCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	envPath := filepath.Join(root, ".agent-layer", ".env")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600))
	require.NoError(t, os.WriteFile(envPath, []byte("AL_EXAMPLE=before\n"), 0o600))
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.Secrets["AL_EXAMPLE"] = "after"
	var syncCalled atomic.Bool
	testutil.AssertCanceledLockWaitPreservesProject(t, root, func(ctx context.Context) error {
		return applyChanges(ctx, root, configPath, envPath, choices, func(context.Context, string) (*alsync.Result, error) {
			syncCalled.Store(true)
			return &alsync.Result{}, nil
		}, io.Discard)
	})
	require.False(t, syncCalled.Load())
}

func TestComputeSkillsChangeSetCancellationStopsProjectLockWait(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	choices := NewChoices()
	var err error
	choices.CLISkillsCatalog, err = templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	testutil.AssertCanceledLockWaitPreservesProject(t, root, func(ctx context.Context) error {
		_, err := computeSkillsChangeSet(ctx, root, choices)
		return err
	})
}
