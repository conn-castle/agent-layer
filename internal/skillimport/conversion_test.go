package skillimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

var conversionLocalPaths = []string{".agent-layer/skills/implement", ".agent-layer/skills/ship-pr", ".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/01_memory.md"}

// pristineConversionProject seeds a v1.1.0 project created before v1.0.0 that
// never adopted: unmodified shipped skill trees and configured instructions.
func pristineConversionProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
	testutil.WriteTransitionSkill(t, root, "implement")
	testutil.WriteTransitionSkill(t, root, "ship-pr")
	paths := config.DefaultPaths(root)
	raw, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	for i, name := range []string{"00_rules.md", "01_memory.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, name), testutil.TransitionFixture(t, name), 0o600))
		raw = append(raw, []byte(fmt.Sprintf("\n[[instructions.local]]\nselectors = [%q]\norder = %d\n", name, i*10))...)
	}
	require.NoError(t, os.WriteFile(paths.ConfigPath, raw, 0o600)) // #nosec G703 -- test-owned project config.
	return root
}

func upgradeWith(root string, converter install.PristineConverter) error {
	return install.Run(root, install.Options{
		System: install.RealSystem{}, Overwrite: true, PinVersion: "1.1.0", Converter: converter, WarnWriter: io.Discard,
		Prompter: &install.PromptFuncs{
			OverwriteAllUnifiedPreviewFunc: func([]install.DiffPreview, []install.DiffPreview) (bool, bool, error) { return true, true, nil },
			OverwritePreviewFunc:           func(install.DiffPreview) (bool, error) { return true, nil },
			DeleteUnknownAllFunc:           func([]string) (bool, error) { return true, nil },
			DeleteUnknownFunc:              func(string) (bool, error) { return true, nil },
		},
	})
}

func requireConverted(t *testing.T, root string) {
	t.Helper()
	for _, rel := range conversionLocalPaths {
		_, err := os.Lstat(filepath.Join(root, rel))
		require.ErrorIs(t, err, os.ErrNotExist, rel)
	}
	paths := config.DefaultPaths(root)
	for _, name := range []string{"implement", "ship-pr"} {
		require.FileExists(t, filepath.Join(paths.ImportedSkillsDir, name, "SKILL.md"))
	}
	for _, name := range []string{"rules.md", "memory.md"} {
		require.FileExists(t, filepath.Join(paths.ImportedInstructionsDir, name))
	}
	cfg, err := config.LoadConfigFS(os.DirFS(root), root, paths.ConfigPath)
	require.NoError(t, err)
	require.Empty(t, cfg.Instructions.Local)
	items, err := install.PlanPristineConversions(root, install.RealSystem{})
	require.NoError(t, err)
	require.Empty(t, items)
	_, err = config.LoadOrderedInstructionsFS(os.DirFS(root), root, cfg.Instructions)
	require.NoError(t, err)
}

// TestUpgradeConversionMatchesFreshSetupImports proves conversion produces the
// imports a fresh v1.2 setup creates, with instruction orders reused.
func TestUpgradeConversionMatchesFreshSetupImports(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := pristineConversionProject(t)
	require.NoError(t, upgradeWith(root, UpgradeConverter{}))
	requireConverted(t, root)

	fresh := t.TempDir()
	require.NoError(t, install.Run(fresh, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
	_, err := NewSourceOnly(fresh).InstallCatalog(t.Context(), []string{"skills/development/implement", "skills/development/ship-pr"})
	require.NoError(t, err)
	for i, name := range []string{"rules.md", "memory.md"} {
		order := i * 10
		_, err := NewInstructionsSourceOnly(fresh).Add(t.Context(), AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: []string{"instructions/" + name}, Order: &order})
		require.NoError(t, err)
	}
	converted, err := config.LoadConfigFS(os.DirFS(root), root, config.DefaultPaths(root).ConfigPath)
	require.NoError(t, err)
	want, err := config.LoadConfigFS(os.DirFS(fresh), fresh, config.DefaultPaths(fresh).ConfigPath)
	require.NoError(t, err)
	require.Equal(t, want.Skills.Imports, converted.Skills.Imports)
	require.Equal(t, want.Instructions, converted.Instructions)
	for _, lock := range []func(config.Paths) string{
		func(p config.Paths) string { return p.SkillsLockPath },
		func(p config.Paths) string { return p.InstructionsLockPath },
	} {
		got, err := os.ReadFile(lock(config.DefaultPaths(root)))
		require.NoError(t, err)
		expected, err := os.ReadFile(lock(config.DefaultPaths(fresh)))
		require.NoError(t, err)
		require.Equal(t, string(expected), string(got))
	}

	// The public manual rollback restores the local copies and removes the imports.
	snapshots, err := install.ListUpgradeSnapshots(root, install.RealSystem{})
	require.NoError(t, err)
	require.NoError(t, install.RollbackUpgradeSnapshot(root, snapshots[0].ID, install.RollbackUpgradeSnapshotOptions{System: install.RealSystem{}}))
	items, err := install.PlanPristineConversions(root, install.RealSystem{})
	require.NoError(t, err)
	require.Len(t, items, 4)
	require.NoDirExists(t, config.DefaultPaths(root).ImportedSkillsDir)
	require.NoFileExists(t, config.DefaultPaths(root).SkillsLockPath)
}

func TestUpgradeConversionPreflightFailsOfflineWithoutWrites(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.git")
	count, _ := strconv.Atoi(os.Getenv("GIT_CONFIG_COUNT"))
	t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", count), "url."+missing+".insteadOf")
	t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", count), templates.GeneralSkillsRepository)
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(count+1))
	root := pristineConversionProject(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/sync.lock"), nil, 0o600))
	before := testutil.SnapshotEvidence(t, root)
	err := upgradeWith(root, UpgradeConverter{})
	require.ErrorContains(t, err, "could not prepare github.com/nicholasjconn/skills imports to convert 4 unmodified shipped copies: ")
	require.ErrorContains(t, err, "running al upgrade in a terminal without --yes and declining the conversion")
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

// Project-state problems the imports would refuse are found by the preflight,
// before the snapshot or any other project write.
func TestUpgradeConversionPreflightChecksProjectStateWithoutWrites(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := pristineConversionProject(t)
	orphan := filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "orphan")
	require.NoError(t, os.MkdirAll(orphan, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "SKILL.md"), []byte("---\nname: orphan\ndescription: Unowned.\n---\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/sync.lock"), nil, 0o600))
	before := testutil.SnapshotEvidence(t, root)
	err := upgradeWith(root, UpgradeConverter{})
	require.ErrorContains(t, err, "could not prepare github.com/nicholasjconn/skills imports to convert 4 unmodified shipped copies: ")
	require.ErrorContains(t, err, "imported skill directories have no entry in")
	require.ErrorContains(t, err, "This upgrade changed nothing")
	require.Equal(t, before, testutil.SnapshotEvidence(t, root), "no snapshot or other write precedes the refusal")
}

// Without config.toml the preflight plans against the template configuration
// the step seeds, so state refusals still precede every write and the
// conversion itself succeeds.
func TestUpgradeConversionPreflightPlansSeededConfigWhenMissing(t *testing.T) {
	testutil.CatalogGitFixture(t)
	missingConfig := func(t *testing.T) string {
		t.Helper()
		root := pristineConversionProject(t)
		paths := config.DefaultPaths(root)
		for _, path := range []string{paths.ConfigPath, filepath.Join(paths.InstructionsDir, "00_rules.md"), filepath.Join(paths.InstructionsDir, "01_memory.md")} {
			require.NoError(t, os.Remove(path))
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/sync.lock"), nil, 0o600))
		return root
	}
	t.Run("orphan refused before writes", func(t *testing.T) {
		root := missingConfig(t)
		orphan := filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "orphan")
		require.NoError(t, os.MkdirAll(orphan, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(orphan, "SKILL.md"), []byte("---\nname: orphan\ndescription: Unowned.\n---\n"), 0o600))
		before := testutil.SnapshotEvidence(t, root)
		err := upgradeWith(root, UpgradeConverter{})
		require.ErrorContains(t, err, "could not prepare github.com/nicholasjconn/skills imports to convert 2 unmodified shipped copies: ")
		require.ErrorContains(t, err, "imported skill directories have no entry in")
		require.Equal(t, before, testutil.SnapshotEvidence(t, root), "no snapshot or seeded config precedes the refusal")
	})
	t.Run("converts", func(t *testing.T) {
		root := missingConfig(t)
		require.NoError(t, upgradeWith(root, UpgradeConverter{}))
		paths := config.DefaultPaths(root)
		for _, name := range []string{"implement", "ship-pr"} {
			require.NoDirExists(t, filepath.Join(paths.SkillsDir, name))
			require.FileExists(t, filepath.Join(paths.ImportedSkillsDir, name, "SKILL.md"))
		}
	})
	t.Run("converts beside shipped local instructions", func(t *testing.T) {
		// Without configuration the shipped instruction files are not
		// offered, and skill planning reads skills only, so they neither
		// refuse the preflight nor change.
		root := pristineConversionProject(t)
		paths := config.DefaultPaths(root)
		require.NoError(t, os.Remove(paths.ConfigPath))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/sync.lock"), nil, 0o600))
		instructions := testutil.SnapshotEvidence(t, paths.InstructionsDir)
		require.NoError(t, upgradeWith(root, UpgradeConverter{}))
		for _, name := range []string{"implement", "ship-pr"} {
			require.NoDirExists(t, filepath.Join(paths.SkillsDir, name))
			require.FileExists(t, filepath.Join(paths.ImportedSkillsDir, name, "SKILL.md"))
		}
		require.Equal(t, instructions, testutil.SnapshotEvidence(t, paths.InstructionsDir))
	})
}

// seamConverter injects transaction behavior into the real conversion.
type seamConverter struct {
	newTransaction func(pathSet, *skilllock.File) *transaction
}

func (c seamConverter) Prepare(ctx context.Context, root string, items []install.PristineConversion) (install.PreparedConversion, error) {
	prepared, err := UpgradeConverter{}.Prepare(ctx, root, items)
	if err != nil {
		return nil, err
	}
	prepared.(*ConversionSource).newTransaction = c.newTransaction
	return prepared, nil
}

// TestInterruptedConversionIsCompletedByTheNextUpgrade copies the project at
// durable points inside the conversion, which is exactly what a killed process
// leaves, then proves recovery plus the next al upgrade completes it.
func TestInterruptedConversionIsCompletedByTheNextUpgrade(t *testing.T) {
	testutil.CatalogGitFixture(t)
	for _, scenario := range []struct {
		name, tier, step string
		// localAfterRecovery lists local copies the crash image still holds once recovered.
		localAfterRecovery int
	}{
		{"skills before commit", "skills", "config", 4},
		{"skills after commit before local removal", "skills", "committed", 4},
		{"between conversions", "instructions", "before-journal", 2},
		{"instruction before commit", "instructions", "config", 2},
		{"instruction after commit before local removal", "instructions", "committed", 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := pristineConversionProject(t)
			original, err := os.ReadFile(config.DefaultPaths(root).ConfigPath)
			require.NoError(t, err)
			crash := filepath.Join(t.TempDir(), "crash")
			captured := false
			converter := seamConverter{newTransaction: func(paths pathSet, lock *skilllock.File) *transaction {
				txn := newTransaction(paths, lock)
				instructions := paths.ExactFiles
				txn.checkpoint = func(step string) {
					if captured || step != scenario.step || instructions != (scenario.tier == "instructions") {
						return
					}
					captured = true
					copyProject(t, root, crash)
				}
				return txn
			}}
			require.NoError(t, upgradeWith(root, converter))
			requireConverted(t, root)
			require.True(t, captured)

			// The next al upgrade must recover the crash image by itself.
			unrecovered := filepath.Join(t.TempDir(), "unrecovered")
			copyProject(t, crash, unrecovered)
			require.NoError(t, upgradeWith(unrecovered, UpgradeConverter{}))
			requireConverted(t, unrecovered)

			require.NoError(t, skilljournal.RecoverBoth(crash))
			local := 0
			for _, rel := range conversionLocalPaths {
				if _, err := os.Lstat(filepath.Join(crash, rel)); err == nil {
					local++
				}
			}
			require.Equal(t, scenario.localAfterRecovery, local, "no content is ever neither local nor imported")
			if scenario.name == "skills before commit" {
				recovered, err := os.ReadFile(config.DefaultPaths(crash).ConfigPath)
				require.NoError(t, err)
				require.Equal(t, string(original), string(recovered))
			}
			require.NoError(t, install.CheckUpgradeSource(crash, install.RealSystem{}))
			require.NoError(t, upgradeWith(crash, UpgradeConverter{}))
			requireConverted(t, crash)
		})
	}
}

// TestFailedInnerRollbackCannotReplayOverRestoredSnapshot fails an import and
// its own rollback, leaving a journal; the outer upgrade rollback must clear it.
func TestFailedInnerRollbackCannotReplayOverRestoredSnapshot(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := pristineConversionProject(t)
	before := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills"), filepath.Join(root, ".agent-layer/instructions"), filepath.Join(root, ".agent-layer/config.toml"))
	converter := seamConverter{newTransaction: func(paths pathSet, lock *skilllock.File) *transaction {
		txn := newTransaction(paths, lock)
		write := txn.writeFile
		configWrites := 0
		txn.writeFile = func(path string, data []byte, perm os.FileMode) error {
			if path == paths.ConfigPath {
				configWrites++
				if configWrites > 1 {
					return errors.New("injected rollback failure")
				}
			}
			if path == paths.SkillsLockPath {
				return errors.New("injected lock failure")
			}
			return write(path, data, perm)
		}
		return txn
	}}
	err := upgradeWith(root, converter)
	require.ErrorContains(t, err, "injected lock failure")
	require.ErrorContains(t, err, "rolled back to pre-upgrade state")
	for _, tier := range []string{config.DefaultPaths(root).ImportedSkillsDir, config.DefaultPaths(root).ImportedInstructionsDir} {
		require.NoDirExists(t, skilljournal.StagingRoot(tier))
	}
	require.NoError(t, skilljournal.RecoverBoth(root))
	require.Equal(t, before, testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills"), filepath.Join(root, ".agent-layer/instructions"), filepath.Join(root, ".agent-layer/config.toml")))
	require.NoFileExists(t, config.DefaultPaths(root).SkillsLockPath)
}

func copyProject(t *testing.T, source, dest string) {
	t.Helper()
	require.NoError(t, filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		if strings.HasSuffix(path, "sync.lock") {
			return nil // The process lock does not survive a crash.
		}
		raw, err := os.ReadFile(path) // #nosec G304 G122 -- copies a test-owned project.
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, info.Mode().Perm()) // #nosec G703 -- destination is rooted in t.TempDir.
	}))
}

// failAfterImportConverter commits the real conversion, then fails the step.
type failAfterImportConverter struct{}

func (failAfterImportConverter) Prepare(ctx context.Context, root string, items []install.PristineConversion) (install.PreparedConversion, error) {
	prepared, err := UpgradeConverter{}.Prepare(ctx, root, items)
	if err != nil {
		return nil, err
	}
	return failAfterImport{prepared}, nil
}

type failAfterImport struct{ install.PreparedConversion }

func (f failAfterImport) Import(ctx context.Context, items []install.PristineConversion, removeLocal func(install.PristineConversion) error) error {
	if err := f.PreparedConversion.Import(ctx, items, removeLocal); err != nil {
		return err
	}
	return errors.New("injected failure after imports")
}

// A catalog block that already lists another exact selector without a lock
// entry makes the import materialize it too. Both automatic and manual rollback
// must remove it with the converted imports, or later syncs see an orphan.
func TestConversionRollbackRemovesEveryImportItMaterialized(t *testing.T) {
	testutil.CatalogGitFixture(t)
	setup := func(t *testing.T) string {
		root := pristineConversionProject(t)
		paths := config.DefaultPaths(root)
		raw, err := os.ReadFile(paths.ConfigPath)
		require.NoError(t, err)
		content, err := config.SetSkillImportSelectors(string(raw), config.SkillImport{Repository: templates.GeneralSkillsRepository}.Identity(), []string{"skills/tools/find-docs"})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(paths.ConfigPath, []byte(content), 0o600)) // #nosec G703 -- test-owned project config.
		require.NoError(t, os.MkdirAll(paths.ImportedSkillsDir, 0o750))            // The tier exists before conversion.
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/sync.lock"), nil, 0o600))
		return root
	}
	evidence := func(t *testing.T, root string) map[string]string {
		state := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer"))
		for path := range state {
			if strings.Contains(path, "upgrade-snapshots") || strings.Contains(path, filepath.Join(".agent-layer", "tmp")) || strings.Contains(path, filepath.Join(".agent-layer", "state")) {
				delete(state, path)
			}
		}
		return state
	}

	t.Run("automatic", func(t *testing.T) {
		root := setup(t)
		before := evidence(t, root)
		err := upgradeWith(root, failAfterImportConverter{})
		require.ErrorContains(t, err, "injected failure after imports")
		require.NoDirExists(t, filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "find-docs"))
		require.Equal(t, before, evidence(t, root))
	})
	t.Run("manual", func(t *testing.T) {
		root := setup(t)
		before := evidence(t, root)
		require.NoError(t, upgradeWith(root, UpgradeConverter{}))
		require.DirExists(t, filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "find-docs"), "the import materialized the configured selector")
		snapshots, err := install.ListUpgradeSnapshots(root, install.RealSystem{})
		require.NoError(t, err)
		require.NoError(t, install.RollbackUpgradeSnapshot(root, snapshots[0].ID, install.RollbackUpgradeSnapshotOptions{System: install.RealSystem{}}))
		require.NoDirExists(t, filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "find-docs"))
		after := evidence(t, root)
		for path := range after {
			// Manual rollback restores the pin; post-upgrade sync output outside
			// .agent-layer is not compared, and the version file is restored.
			if _, ok := before[path]; !ok {
				t.Errorf("rollback left %s", path)
			}
		}
		items, err := install.PlanPristineConversions(root, install.RealSystem{})
		require.NoError(t, err)
		require.Len(t, items, 4)
	})
}
