package wizard

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillimport"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestApplySkillsChangesUsesContextForEveryImportOperation(t *testing.T) {
	for _, operation := range []string{"instructions", "install", "remove"} {
		t.Run(operation, func(t *testing.T) {
			testutil.CatalogGitFixture(t)
			root := t.TempDir()
			setupRemoteRepo(t, root)
			changes := skillsChangeSet{}
			switch operation {
			case "instructions":
				require.NoError(t, os.RemoveAll(filepath.Join(root, ".agent-layer", "instructions")))
				changes.instructionImports = []string{".agent-layer/instructions/00_rules.md"}
			case "install":
				changes.importSelectors = []string{"skills/tools/playwright"}
			case "remove":
				// Keep one selector so removal must resolve the remaining source.
				selectors := []string{"skills/tools/playwright", "skills/tools/tavily-web"}
				_, err := skillimport.NewSourceOnly(root).InstallCatalog(t.Context(), selectors, nil)
				require.NoError(t, err)
				changes.removeSelectors = selectors[:1]
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			err := applySkillsChanges(ctx, root, changes)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestWizardCancellationStopsRunningCatalogImport(t *testing.T) {
	for _, freshInit := range []bool{false, true} {
		name := "wizard"
		if freshInit {
			name = "after init"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			setupRemoteRepo(t, root)
			bin := t.TempDir()
			ready := filepath.Join(bin, "ready")
			// The first Git invocation blocks long enough to distinguish cancellation
			// from natural exit. exec keeps the fixture bounded even on regression.
			require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n: > \"$WIZARD_GIT_READY\"\nexec /bin/sleep 3\n"), 0o700)) // #nosec G306 -- owner-executable test fixture.
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("WIZARD_GIT_READY", ready)
			ui := &MockUI{
				MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
					title, _, _ = strings.Cut(title, cliSkillsStatusHeading)
					*selected = nil
					if title == messages.WizardEnableCLISkillsTitle {
						*selected = []string{"Playwright browser automation"}
					}
					return nil
				},
				ConfirmFunc: func(title string, value *bool) error {
					if title == messages.WizardApplyChangesPrompt {
						*value = true
					}
					return nil
				},
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cancelled := make(chan bool, 1)
			go func() {
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					if _, err := os.Stat(ready); err == nil {
						cancel()
						cancelled <- true
						return
					}
					select {
					case <-ctx.Done():
						cancelled <- false
						return
					case <-ticker.C:
					}
				}
			}()
			run := RunWithWriter
			if freshInit {
				run = RunAfterFreshInitWithWriter
			}
			syncCalled := false
			start := time.Now()
			err := run(ctx, root, ui, func(context.Context, string) (*alsync.Result, error) {
				syncCalled = true
				return &alsync.Result{}, nil
			}, "", io.Discard)
			elapsed := time.Since(start)
			cancel()
			require.True(t, <-cancelled, "Git must start before cancellation: %v", err)
			require.ErrorIs(t, err, context.Canceled)
			require.Less(t, elapsed, 2*time.Second, "wizard waited for natural Git exit")
			require.False(t, syncCalled)
			require.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills.lock.json"))
			require.NoDirExists(t, filepath.Join(root, ".agent-layer", "skills-imported", "playwright"))
		})
	}
}
