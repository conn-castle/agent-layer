package install

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// pristineProject seeds a v1.0.0-or-later project holding unmodified shipped
// copies: the implement and ship-pr trees and both configured instructions.
func pristineProject(t *testing.T, pin string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: pin}))
	testutil.WriteTransitionSkill(t, root, "implement")
	testutil.WriteTransitionSkill(t, root, "ship-pr")
	for _, name := range []string{"00_rules.md", "01_memory.md"} {
		writeTransitionFile(t, root, ".agent-layer/instructions/"+name, testutil.TransitionFixture(t, name))
	}
	configureLocalInstructions(t, root, "00_rules.md", "01_memory.md")
	writeTransitionFile(t, root, ".agent-layer/sync.lock", nil) // The project lock file persists by design.
	return root
}

func conversionPaths(items []PristineConversion) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}

func TestPlanPristineConversionsClassifiesCompleteShippedCopies(t *testing.T) {
	root := pristineProject(t, "1.1.0")
	before := testutil.SnapshotEvidence(t, root)
	items, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, []PristineConversion{
		{Kind: PristineConversionSkill, Path: ".agent-layer/skills/implement", Name: "implement", Selector: "skills/development/implement"},
		{Kind: PristineConversionSkill, Path: ".agent-layer/skills/ship-pr", Name: "ship-pr", Selector: "skills/development/ship-pr"},
		{Kind: PristineConversionInstruction, Path: ".agent-layer/instructions/00_rules.md", Name: "rules.md", Selector: "instructions/rules.md", LocalName: "00_rules.md"},
		{Kind: PristineConversionInstruction, Path: ".agent-layer/instructions/01_memory.md", Name: "memory.md", Selector: "instructions/memory.md", LocalName: "01_memory.md"},
	}, items)
	// Convertible copies never block upgrade; only the wizard asks for al upgrade first.
	require.NoError(t, CheckTransitionState(root, RealSystem{}))
	require.NoError(t, CheckUpgradeSource(root, RealSystem{}))
	err = CheckWizardSource(root, RealSystem{})
	require.ErrorIs(t, err, ErrUnfinishedTransition)
	require.ErrorContains(t, err, "4 unmodified shipped local copies")
	require.ErrorContains(t, err, "Run al upgrade to convert them")
	require.NotContains(t, err.Error(), "v1.0.0 executable")
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestPlanPristineConversionsLeavesUserManagedContent(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		setup func(t *testing.T, root string)
		want  []string
	}{
		{"extra skill file", func(t *testing.T, root string) {
			writeTransitionFile(t, root, ".agent-layer/skills/ship-pr/notes.md", []byte("mine\n"))
		}, []string{".agent-layer/skills/implement"}},
		{"edited support file", func(t *testing.T, root string) {
			writeTransitionFile(t, root, ".agent-layer/skills/ship-pr/references/fix-ci.md", []byte("edited\n"))
		}, []string{".agent-layer/skills/implement"}},
		{"incomplete tree with pristine SKILL.md", func(t *testing.T, root string) {
			require.NoError(t, os.RemoveAll(filepath.Join(root, ".agent-layer/skills/ship-pr/scripts")))
		}, []string{".agent-layer/skills/implement"}},
		{"linked skill file", func(t *testing.T, root string) {
			target := filepath.Join(root, ".agent-layer/skills/implement/SKILL.md")
			raw, err := os.ReadFile(target) // #nosec G304 -- test-owned fixture.
			require.NoError(t, err)
			outside := filepath.Join(t.TempDir(), "SKILL.md")
			require.NoError(t, os.WriteFile(outside, raw, 0o600)) // #nosec G703 -- test-owned temporary path.
			require.NoError(t, os.Remove(target))
			require.NoError(t, os.Symlink(outside, target))
		}, []string{".agent-layer/skills/ship-pr"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
			testutil.WriteTransitionSkill(t, root, "implement")
			testutil.WriteTransitionSkill(t, root, "ship-pr")
			scenario.setup(t, root)
			items, err := PlanPristineConversions(root, RealSystem{})
			require.NoError(t, err)
			require.Equal(t, scenario.want, conversionPaths(items))
		})
	}
}

// An unreferenced shipped instruction is not converted, and sync would refuse
// it after the pin advanced, so upgrade refuses it first with the loader's
// guidance (never the v1.0.0 guidance).
func TestUnreferencedPristineInstructionRefusesUpgradeBeforeMutation(t *testing.T) {
	for _, withOtherBlocks := range []bool{false, true} {
		root := t.TempDir()
		require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
		writeTransitionFile(t, root, ".agent-layer/instructions/00_rules.md", testutil.TransitionFixture(t, "00_rules.md"))
		if withOtherBlocks {
			writeTransitionFile(t, root, ".agent-layer/instructions/project.md", []byte("Project.\n"))
			configureLocalInstructions(t, root, "project.md")
		}
		writeTransitionFile(t, root, ".agent-layer/sync.lock", nil)
		items, err := PlanPristineConversions(root, RealSystem{})
		require.NoError(t, err)
		require.Empty(t, items)
		want := config.UnconfiguredLocalInstruction(filepath.Join(root, ".agent-layer/instructions/00_rules.md")).Error()
		require.Contains(t, want, "declare it in .agent-layer/config.toml with [[instructions.local]]")
		require.Contains(t, want, "or remove the file")
		require.NotContains(t, want, "v1.0.0")
		before := testutil.SnapshotEvidence(t, root)
		require.EqualError(t, CheckUpgradeSource(root, RealSystem{}), want)
		_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.2.0"})
		require.EqualError(t, err, want)
		err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.2.0", Converter: &fakeConverter{t: t, root: root}})
		require.EqualError(t, err, want)
		require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		// The loader refuses the same file with the same guidance.
		cfg, err := config.LoadConfigFS(os.DirFS(root), root, filepath.Join(root, upgradeMigrationConfigPath))
		require.NoError(t, err)
		_, err = config.LoadOrderedInstructionsFS(os.DirFS(root), root, cfg.Instructions)
		require.EqualError(t, err, want)
	}
}

// fakeConverter imports by writing the same config, lock, and tier evidence a
// real conversion commits, without Git.
type fakeConverter struct {
	t          *testing.T
	root       string
	prepareErr error
	// importErr fails after the first import committed and its local copy was removed.
	importErr  error
	prepared   [][]PristineConversion
	atPrepare  map[string]string
	pinAtWrite string
}

func (f *fakeConverter) Prepare(_ context.Context, root string, items []PristineConversion) (PreparedConversion, error) {
	f.prepared = append(f.prepared, items)
	f.atPrepare = testutil.SnapshotEvidence(f.t, root)
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	return f, nil
}

func (f *fakeConverter) Import(_ context.Context, items []PristineConversion, removeLocal func(PristineConversion) error) error {
	pin, err := os.ReadFile(filepath.Join(f.root, pinVersionRelPath)) // #nosec G304 -- test-owned pin.
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(f.t, err)
	}
	f.pinAtWrite = strings.TrimSpace(string(pin))
	for _, item := range items {
		writeFakeImport(f.t, f.root, item)
		if err := removeLocal(item); err != nil {
			return err
		}
		if f.importErr != nil {
			return f.importErr
		}
	}
	return nil
}

func (f *fakeConverter) Close() {}

func writeFakeImport(t *testing.T, root string, item PristineConversion) {
	t.Helper()
	paths := config.DefaultPaths(root)
	raw, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	content := string(raw)
	entry := skilllock.Entry{Name: item.Name, Repository: templates.GeneralSkillsRepository, Selector: item.Selector, SelectedPath: item.Selector,
		ResolvedRef: "main", RefKind: skilllock.RefKindBranch, Tracking: skilllock.TrackingTracked, Commit: strings.Repeat("b", 40)}
	lockPath, dest, load := paths.SkillsLockPath, filepath.Join(paths.ImportedSkillsDir, item.Name, "SKILL.md"), skilllock.Load
	newLock := skilllock.New
	block := config.SkillImport{Repository: templates.GeneralSkillsRepository, Selectors: []string{item.Selector}}
	if item.Kind == PristineConversionInstruction {
		lockPath, dest, load, newLock = paths.InstructionsLockPath, filepath.Join(paths.ImportedInstructionsDir, item.Name), skilllock.LoadInstructions, skilllock.NewInstructions
		var order *int
		content, order, err = config.RemoveLocalInstruction(content, item.LocalName)
		require.NoError(t, err)
		content, err = config.AddInstructionImport(content, block, order)
	} else {
		// Like a real import, add the selector to the general block's existing ones.
		selectors := []string{item.Selector}
		parsed, parseErr := config.ParseConfigLenient(raw, paths.ConfigPath)
		require.NoError(t, parseErr)
		for _, imp := range parsed.Skills.Imports {
			if config.NormalizeSkillRepository(imp.Repository) == templates.GeneralSkillsRepository {
				selectors = append(append([]string{}, imp.Selectors...), item.Selector)
			}
		}
		content, err = config.SetSkillImportSelectors(content, block.Identity(), selectors)
	}
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.ConfigPath, []byte(content), 0o600)) // #nosec G703 -- test-owned project config.
	writeTransitionFile(t, root, mustRel(t, root, dest), []byte("---\nname: "+item.Name+"\ndescription: Imported.\n---\n"))
	entry.TreeHash = fakeImportHash(t, dest, item)
	lock, err := load(lockPath)
	if errors.Is(err, skilllock.ErrMissing) {
		lock, err = newLock(), nil
	}
	require.NoError(t, err)
	lock.Upsert(entry)
	require.NoError(t, lock.Save(lockPath))
}

// fakeImportHash is the locked tree hash of the imported content, as the
// real import machinery records it.
func fakeImportHash(t *testing.T, dest string, item PristineConversion) string {
	t.Helper()
	var tree skilltree.Tree
	var err error
	if item.Kind == PristineConversionInstruction {
		tree, err = skilltree.ReadFileNode(skilltree.OSFS{}, dest, item.Name)
	} else {
		tree, err = skilltree.Read(skilltree.OSFS{}, filepath.Dir(dest))
	}
	require.NoError(t, err)
	return tree.Hash()
}

func mustRel(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	require.NoError(t, err)
	return filepath.ToSlash(rel)
}

func conversionUpgrade(root string, converter PristineConverter, prompter *PromptFuncs, warn *bytes.Buffer) error {
	return Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: prompter, PinVersion: "1.1.0", Converter: converter, WarnWriter: warn})
}

func TestUpgradeConvertsPristineCopiesBeforeAdvancingPin(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	before := testutil.SnapshotEvidence(t, root)
	converter := &fakeConverter{t: t, root: root}
	require.NoError(t, conversionUpgrade(root, converter, autoApprovePrompter(), &bytes.Buffer{}))

	require.Len(t, converter.prepared, 1)
	require.Len(t, converter.prepared[0], 4)
	require.Equal(t, before, converter.atPrepare, "preflight must precede every project write")
	require.Equal(t, "1.0.0", converter.pinAtWrite, "the pin advances only after conversion")
	pin, err := readCurrentPinVersion(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, "1.1.0", pin)
	for _, rel := range []string{".agent-layer/skills/implement", ".agent-layer/skills/ship-pr", ".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/01_memory.md"} {
		require.NoFileExists(t, filepath.Join(root, rel))
		require.NoDirExists(t, filepath.Join(root, rel))
	}
	cfg, err := config.LoadConfigLenient(config.DefaultPaths(root).ConfigPath)
	require.NoError(t, err)
	require.Empty(t, cfg.Instructions.Local)
	require.Len(t, cfg.Instructions.Imports, 2)
	require.Equal(t, 0, *cfg.Instructions.Imports[0].Order, "imports reuse the removed local orders")
	require.Equal(t, 10, *cfg.Instructions.Imports[1].Order)
	items, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	require.Empty(t, items)
}

func TestUpgradeConvertsAtCurrentPinAndInUnpinnedProjects(t *testing.T) {
	for _, pin := range []string{"1.1.0", ""} {
		root := pristineProject(t, pin)
		converter := &fakeConverter{t: t, root: root}
		require.NoError(t, conversionUpgrade(root, converter, autoApprovePrompter(), &bytes.Buffer{}))
		require.Equal(t, pin, converter.pinAtWrite)
		require.NoDirExists(t, filepath.Join(root, ".agent-layer/skills/implement"))
	}
}

func TestUpgradeConversionPreflightFailureLeavesProjectUntouched(t *testing.T) {
	root := pristineProject(t, "1.1.0")
	require.NoFileExists(t, config.DefaultPaths(root).SkillsLockPath)
	before := testutil.SnapshotEvidence(t, root)
	converter := &fakeConverter{t: t, root: root, prepareErr: errors.New("network is unreachable")}
	err := conversionUpgrade(root, converter, autoApprovePrompter(), &bytes.Buffer{})
	require.EqualError(t, err, "could not prepare github.com/nicholasjconn/skills imports to convert 4 unmodified shipped copies: network is unreachable. This upgrade changed nothing. Retry when online or after fixing the reported problem, or keep them as local copies by running al upgrade in a terminal without --yes and declining the conversion")
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestUpgradeDeclinedConversionKeepsLocalCopies(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	local := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/ship-pr"), filepath.Join(root, ".agent-layer/instructions"))
	converter := &fakeConverter{t: t, root: root}
	prompter := autoApprovePrompter()
	var offered []PristineConversion
	prompter.ConvertPristineCopiesFunc = func(items []PristineConversion) (bool, error) { offered = items; return false, nil }
	warn := &bytes.Buffer{}
	require.NoError(t, conversionUpgrade(root, converter, prompter, warn))
	require.Len(t, offered, 4)
	require.Empty(t, converter.prepared)
	require.Contains(t, warn.String(), "Kept the unmodified shipped copies as user-managed local content")
	require.Contains(t, warn.String(), "al wizard and profile commands refuse until they are converted", "the decline names what still refuses")
	require.ErrorIs(t, CheckWizardSource(root, RealSystem{}), ErrUnfinishedTransition)
	require.Equal(t, local, testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/ship-pr"), filepath.Join(root, ".agent-layer/instructions")))
	pin, err := readCurrentPinVersion(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, "1.1.0", pin)
}

func TestUpgradeConversionFailureRollsBackEveryTarget(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	before := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer"))
	converter := &fakeConverter{t: t, root: root, importErr: errors.New("injected import failure")}
	err := conversionUpgrade(root, converter, autoApprovePrompter(), &bytes.Buffer{})
	require.ErrorContains(t, err, "upgrade step convertPristineCopies failed (rolled back to pre-upgrade state)")
	require.ErrorContains(t, err, "injected import failure")
	after := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer"))
	for path := range after {
		if strings.Contains(path, "upgrade-snapshots") {
			delete(after, path)
		}
	}
	for path := range before {
		if strings.Contains(path, "upgrade-snapshots") {
			delete(before, path)
		}
	}
	require.Equal(t, before, after, "config, local copies, imported tiers, and lock files are restored")
}

func TestUpgradeRefusesToDeleteLocalCopyWithoutActiveImport(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	converter := &fakeConverter{t: t, root: root}
	err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0", WarnWriter: &bytes.Buffer{},
		Converter: noImportConverter{converter}})
	require.ErrorContains(t, err, ".agent-layer/skills/implement was not imported as expected")
	require.DirExists(t, filepath.Join(root, ".agent-layer/skills/implement"))
}

// noImportConverter reports success without committing any import.
type noImportConverter struct{ *fakeConverter }

func (n noImportConverter) Prepare(ctx context.Context, root string, items []PristineConversion) (PreparedConversion, error) {
	_, err := n.fakeConverter.Prepare(ctx, root, items)
	return n, err
}

func (noImportConverter) Import(_ context.Context, items []PristineConversion, removeLocal func(PristineConversion) error) error {
	return removeLocal(items[0])
}

func TestUpgradeCompletesInterruptedConversionAsDeleteOnly(t *testing.T) {
	root := pristineProject(t, "1.1.0")
	items, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	// A crash after each import committed leaves only the local copies behind.
	for _, item := range items {
		writeFakeImport(t, root, item)
	}
	items, err = PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	require.Len(t, items, 4)
	for _, item := range items {
		require.True(t, item.DeleteOnly, item.Path)
	}
	require.NoError(t, CheckUpgradeSource(root, RealSystem{}))
	// Delete-only completion needs no converter and no network, and it is
	// announced even though nothing is offered for consent.
	prompter := autoApprovePrompter()
	prompter.ConvertPristineCopiesFunc = func([]PristineConversion) (bool, error) {
		t.Fatal("leftover copies are not a choice")
		return false, nil
	}
	warn := &bytes.Buffer{}
	require.NoError(t, conversionUpgrade(root, nil, prompter, warn))
	for _, item := range items {
		require.Contains(t, warn.String(), "Removing leftover unmodified shipped copy "+item.Path+": its import of "+item.Selector+" is already active")
	}
	require.NoDirExists(t, filepath.Join(root, ".agent-layer/skills/ship-pr"))
	require.NoFileExists(t, filepath.Join(root, ".agent-layer/instructions/00_rules.md"))

	// Manual rollback restores the rest of the snapshot without recreating the
	// leftovers beside their still-active imports.
	snapshots, err := ListUpgradeSnapshots(root, RealSystem{})
	require.NoError(t, err)
	writeTransitionFile(t, root, ".agent-layer/al.version", []byte("1.2.0\n"))
	require.NoError(t, RollbackUpgradeSnapshot(root, snapshots[0].ID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}))
	pin, err := readCurrentPinVersion(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, "1.1.0", pin, "unrelated paths are restored")
	for _, item := range items {
		_, err := os.Lstat(filepath.Join(root, item.Path))
		require.ErrorIs(t, err, os.ErrNotExist, item.Path)
	}
	plan, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	require.Empty(t, plan)
}

// A leftover local copy is deleted only when configuration owns its import and
// the imported content equals the locked tree; otherwise upgrade refuses before
// any write, so sync never loses the only usable copy.
func TestUpgradeRefusesDeleteOnlyWithBrokenImport(t *testing.T) {
	item := func(t *testing.T, root, rel string) PristineConversion {
		t.Helper()
		items, err := PlanPristineConversions(root, RealSystem{})
		require.NoError(t, err)
		for _, candidate := range items {
			if candidate.Path == rel {
				return candidate
			}
		}
		t.Fatalf("%s not planned", rel)
		return PristineConversion{}
	}
	paths := config.DefaultPaths
	for _, scenario := range []struct {
		name, local string
		breakIt     func(t *testing.T, root string)
	}{
		{"empty imported skill directory", ".agent-layer/skills/implement", func(t *testing.T, root string) {
			dir := filepath.Join(paths(root).ImportedSkillsDir, "implement")
			require.NoError(t, os.RemoveAll(dir))
			require.NoError(t, os.MkdirAll(dir, 0o700))
		}},
		{"skill import not owned by config", ".agent-layer/skills/implement", func(t *testing.T, root string) {
			raw, err := os.ReadFile(paths(root).ConfigPath)
			require.NoError(t, err)
			content, err := config.SetSkillImportSelectors(string(raw), config.SkillImport{Repository: templates.GeneralSkillsRepository}.Identity(), nil)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(paths(root).ConfigPath, []byte(content), 0o600)) // #nosec G703 -- test-owned project config.
		}},
		{"modified imported skill", ".agent-layer/skills/implement", func(t *testing.T, root string) {
			writeTransitionFile(t, root, ".agent-layer/skills-imported/implement/SKILL.md", []byte("---\nname: implement\ndescription: Edited.\n---\n"))
		}},
		{"instruction import not owned by config", ".agent-layer/instructions/00_rules.md", func(t *testing.T, root string) {
			raw, err := os.ReadFile(paths(root).ConfigPath)
			require.NoError(t, err)
			content := strings.Replace(string(raw), "  'instructions/rules.md',", "  'instructions/other.md',", 1)
			require.NotEqual(t, string(raw), content)
			require.NoError(t, os.WriteFile(paths(root).ConfigPath, []byte(content), 0o600)) // #nosec G703 -- test-owned project config.
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := pristineProject(t, "1.1.0")
			writeFakeImport(t, root, item(t, root, scenario.local))
			planned := item(t, root, scenario.local)
			require.True(t, planned.DeleteOnly, "an intact import makes the leftover copy delete-only")
			scenario.breakIt(t, root)
			before := testutil.SnapshotEvidence(t, root)
			got, err := PlanPristineConversions(root, RealSystem{})
			require.Error(t, err, "%+v", got)
			require.ErrorContains(t, err, scenario.local+" is an unmodified shipped copy, but its import of "+planned.Selector+" is not intact")
			require.ErrorContains(t, err, "this upgrade changed nothing")
			require.NotContains(t, err.Error(), "v1.0.0")
			err = conversionUpgrade(root, &fakeConverter{t: t, root: root}, autoApprovePrompter(), &bytes.Buffer{})
			require.ErrorContains(t, err, "is not intact")
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		})
	}
}

func TestManualRollbackRestoresConversionSnapshot(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	before := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills"), filepath.Join(root, ".agent-layer/instructions"), config.DefaultPaths(root).ConfigPath)
	require.NoError(t, conversionUpgrade(root, &fakeConverter{t: t, root: root}, autoApprovePrompter(), &bytes.Buffer{}))
	snapshots, err := ListUpgradeSnapshots(root, RealSystem{})
	require.NoError(t, err)
	id := snapshots[0].ID

	// A snapshot that recreates local slots without restoring both lock files stays refused.
	path := filepath.Join(root, upgradeSnapshotDirRelPath, id+".json")
	snapshot, err := readUpgradeSnapshot(path, RealSystem{})
	require.NoError(t, err)
	partial := snapshot
	partial.SnapshotID = "partial"
	partial.Entries = nil
	for _, entry := range snapshot.Entries {
		if entry.Path != ".agent-layer/instructions.lock.json" {
			partial.Entries = append(partial.Entries, entry)
		}
	}
	raw, err := json.Marshal(partial)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, upgradeSnapshotDirRelPath, "partial.json"), raw, 0o600))
	err = RollbackUpgradeSnapshot(root, "partial", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
	require.Regexp(t, "would (discard ownership of converted import|recreate converted local source)", err.Error())

	require.NoError(t, RollbackUpgradeSnapshot(root, id, RollbackUpgradeSnapshotOptions{System: RealSystem{}}))
	require.Equal(t, before, testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills"), filepath.Join(root, ".agent-layer/instructions"), config.DefaultPaths(root).ConfigPath))
	for _, path := range []string{config.DefaultPaths(root).SkillsLockPath, config.DefaultPaths(root).InstructionsLockPath} {
		require.NoFileExists(t, path)
	}
	require.NoDirExists(t, config.DefaultPaths(root).ImportedSkillsDir)
	require.NoDirExists(t, config.DefaultPaths(root).ImportedInstructionsDir)
	pin, err := readCurrentPinVersion(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", pin)
}

// editingConverter commits each import, then edits the local copy before
// conversion deletes it, as a user could while imports commit.
type editingConverter struct {
	*fakeConverter
	edit func(item PristineConversion)
}

func (e editingConverter) Prepare(ctx context.Context, root string, items []PristineConversion) (PreparedConversion, error) {
	_, err := e.fakeConverter.Prepare(ctx, root, items)
	return e, err
}

func (e editingConverter) Import(_ context.Context, items []PristineConversion, removeLocal func(PristineConversion) error) error {
	for _, item := range items {
		writeFakeImport(e.t, e.root, item)
		e.edit(item)
		if err := removeLocal(item); err != nil {
			return err
		}
	}
	return nil
}

// preservedCopy returns the one edited copy kept under .agent-layer/tmp.
func preservedCopy(t *testing.T, root, rel string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, ".agent-layer", "tmp", "upgrade-conversion-*", filepath.FromSlash(rel)))
	require.NoError(t, err)
	require.Len(t, matches, 1, "edited copy is preserved outside the rollback's reach")
	return matches[0]
}

func TestUpgradeConversionNeverDeletesCopyEditedDuringImport(t *testing.T) {
	for _, scenario := range []struct {
		name, rel, edited string
	}{
		{"added skill file", ".agent-layer/skills/implement", ".agent-layer/skills/implement/notes.md"},
		{"edited instruction", ".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/00_rules.md"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := pristineProject(t, "1.0.0")
			shipped := testutil.SnapshotEvidence(t, filepath.Join(root, filepath.FromSlash(scenario.rel)))
			converter := editingConverter{&fakeConverter{t: t, root: root}, func(item PristineConversion) {
				if item.Path == scenario.rel {
					writeTransitionFile(t, root, scenario.edited, []byte("my edit\n"))
				}
			}}
			err := conversionUpgrade(root, converter, autoApprovePrompter(), &bytes.Buffer{})
			require.ErrorContains(t, err, scenario.rel+" changed during the upgrade, so it was not deleted")
			require.ErrorContains(t, err, "rolled back")
			// Rollback restores the shipped copy; the edit survives outside it.
			require.Equal(t, shipped, testutil.SnapshotEvidence(t, filepath.Join(root, filepath.FromSlash(scenario.rel))))
			preserved := preservedCopy(t, root, scenario.rel)
			editedRel := strings.TrimPrefix(scenario.edited, scenario.rel)
			data, err := os.ReadFile(preserved + filepath.FromSlash(editedRel)) // #nosec G304 -- test-owned preserved copy.
			require.NoError(t, err)
			require.Equal(t, "my edit\n", string(data))
			pin, err := readCurrentPinVersion(root, RealSystem{})
			require.NoError(t, err)
			require.Equal(t, "1.0.0", pin)
		})
	}
}

// snapshotHookWriter edits the project once the upgrade snapshot exists,
// before the conversion step starts.
type snapshotHookWriter struct {
	bytes.Buffer
	hook func()
	done bool
}

func (w *snapshotHookWriter) Write(p []byte) (int, error) {
	if !w.done && strings.Contains(string(p), "Created upgrade snapshot") {
		w.done = true
		w.hook()
	}
	return w.Buffer.Write(p)
}

func TestUpgradeConversionPreservesEveryCopyEditedAfterSnapshot(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	edits := map[string]string{
		".agent-layer/skills/ship-pr":            "SKILL.md",
		".agent-layer/skills/implement":          "SKILL.md",
		".agent-layer/instructions/01_memory.md": "",
	}
	warn := &snapshotHookWriter{hook: func() {
		for rel, file := range edits {
			writeTransitionFile(t, root, strings.TrimSuffix(rel+"/"+file, "/"), []byte("edited after the snapshot\n"))
		}
	}}
	converter := &fakeConverter{t: t, root: root}
	err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0", Converter: converter, WarnWriter: warn})
	require.True(t, warn.done)
	for rel, file := range edits {
		require.ErrorContains(t, err, rel+" changed during the upgrade, so it was not deleted")
		data, err := os.ReadFile(filepath.Join(preservedCopy(t, root, rel), file)) // #nosec G304 -- test-owned preserved copy.
		require.NoError(t, err)
		require.Equal(t, "edited after the snapshot\n", string(data), "every edit survives the rollback")
	}
	require.NoFileExists(t, config.DefaultPaths(root).SkillsLockPath, "nothing was imported")
}

// Declining new conversions never keeps a leftover copy of an intact import:
// sync cannot load it beside its import, so it is removed regardless.
func TestUpgradeDeclineStillCompletesLeftoverCopies(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	items, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	for _, item := range items {
		if item.Path == ".agent-layer/skills/ship-pr" {
			writeFakeImport(t, root, item)
		}
	}
	kept := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/implement"), filepath.Join(root, ".agent-layer/instructions"))
	prompter := autoApprovePrompter()
	var offered []PristineConversion
	prompter.ConvertPristineCopiesFunc = func(items []PristineConversion) (bool, error) { offered = items; return false, nil }
	converter := &fakeConverter{t: t, root: root}
	require.NoError(t, conversionUpgrade(root, converter, prompter, &bytes.Buffer{}))
	require.Len(t, offered, 3, "only new imports are offered")
	for _, item := range offered {
		require.False(t, item.DeleteOnly, item.Path)
	}
	require.Empty(t, converter.prepared)
	require.NoDirExists(t, filepath.Join(root, ".agent-layer/skills/ship-pr"), "the leftover copy is removed")
	require.DirExists(t, filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "ship-pr"))
	require.Equal(t, kept, testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/implement"), filepath.Join(root, ".agent-layer/instructions")))
}

// removalRecordingSystem records renames and deletions of local copies.
type removalRecordingSystem struct {
	RealSystem
	root       string
	failRename bool
	ops        []string
}

func (s *removalRecordingSystem) Rename(oldpath, newpath string) error {
	if strings.Contains(oldpath, filepath.FromSlash(".agent-layer/skills/")) || strings.Contains(oldpath, filepath.FromSlash(".agent-layer/instructions/")) {
		s.ops = append(s.ops, "rename "+mustRelNoT(s.root, oldpath)+" -> "+mustRelNoT(s.root, newpath))
		if s.failRename && strings.Contains(newpath, "upgrade-conversion-") {
			return errors.New("cross-device link")
		}
	}
	return s.RealSystem.Rename(oldpath, newpath)
}

func (s *removalRecordingSystem) RemoveAll(path string) error {
	s.ops = append(s.ops, "remove "+mustRelNoT(s.root, path))
	return s.RealSystem.RemoveAll(path)
}

func mustRelNoT(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func TestUpgradeConversionMovesLocalCopiesAsideBeforeDeleting(t *testing.T) {
	for _, failRename := range []bool{false, true} {
		root := pristineProject(t, "1.0.0")
		sys := &removalRecordingSystem{root: root, failRename: failRename}
		err := Run(root, Options{System: sys, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0", Converter: &fakeConverter{t: t, root: root}, WarnWriter: &bytes.Buffer{}})
		locals := []string{".agent-layer/skills/implement", ".agent-layer/skills/ship-pr", ".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/01_memory.md"}
		if failRename {
			// Without an atomic rename nothing is deleted in place: a kill
			// mid-delete would leave a partial tree. The upgrade rolls back.
			require.ErrorContains(t, err, "to remove it atomically")
			require.ErrorContains(t, err, "rolled back to pre-upgrade state")
			for _, rel := range locals {
				_, statErr := os.Lstat(filepath.Join(root, rel))
				require.NoError(t, statErr, "%s is intact after the rollback", rel)
			}
			continue
		}
		require.NoError(t, err)
		for _, rel := range locals {
			require.NoFileExists(t, filepath.Join(root, rel))
			require.NoDirExists(t, filepath.Join(root, rel))
			renamed := -1
			for i, op := range sys.ops {
				if strings.HasPrefix(op, "rename "+rel+" -> .agent-layer/tmp/upgrade-conversion-") {
					renamed = i
				}
			}
			require.GreaterOrEqual(t, renamed, 0, "%s is moved into .agent-layer/tmp first: %v", rel, sys.ops)
			require.NotContains(t, sys.ops, "remove "+rel, "a kill mid-delete must not leave a partial tree at %s", rel)
		}
		matches, err := filepath.Glob(filepath.Join(root, ".agent-layer", "tmp", "upgrade-conversion-*"))
		require.NoError(t, err)
		require.Empty(t, matches, "the removal directory is cleaned up")
	}
}

// A copy replaced after the snapshot by a link fails conversion planning; the
// link is still moved out of the rollback's reach instead of being overwritten.
func TestUpgradeConversionPreservesCopyReplacedAfterSnapshot(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	rel := ".agent-layer/instructions/00_rules.md"
	outside := filepath.Join(t.TempDir(), "rules.md")
	require.NoError(t, os.WriteFile(outside, []byte("my rules\n"), 0o600))
	warn := &snapshotHookWriter{hook: func() {
		require.NoError(t, os.Remove(filepath.Join(root, rel)))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, rel)))
	}}
	err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0", Converter: &fakeConverter{t: t, root: root}, WarnWriter: warn})
	require.True(t, warn.done)
	require.ErrorContains(t, err, rel+" changed during the upgrade, so it was not deleted")
	require.ErrorContains(t, err, "rolled back")
	preserved := preservedCopy(t, root, rel)
	target, err := os.Readlink(preserved)
	require.NoError(t, err, "the replacing link itself is preserved")
	require.Equal(t, outside, target)
}

// failingRemovalDirSystem cannot create the removal directory, so edited
// copies cannot be moved out of the rollback's reach.
type failingRemovalDirSystem struct{ RealSystem }

func (failingRemovalDirSystem) MkdirAll(path string, perm os.FileMode) error {
	if strings.Contains(path, "upgrade-conversion-") {
		return errors.New("read-only file system")
	}
	return os.MkdirAll(path, perm)
}

// When an edited copy cannot be preserved, the automatic rollback leaves it in
// place rather than restoring the snapshot's unmodified copy over the edit.
func TestUpgradeRollbackLeavesUnpreservableEditedCopyInPlace(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	rel := ".agent-layer/instructions/01_memory.md"
	warn := &snapshotHookWriter{hook: func() {
		writeTransitionFile(t, root, rel, []byte("edited after the snapshot\n"))
	}}
	err := Run(root, Options{System: failingRemovalDirSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0", Converter: &fakeConverter{t: t, root: root}, WarnWriter: warn})
	require.True(t, warn.done)
	require.ErrorContains(t, err, rel+" changed during the upgrade and could not be moved out of the rollback's reach")
	require.ErrorContains(t, err, "rolled back")
	data, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- test-owned project file.
	require.NoError(t, err)
	require.Equal(t, "edited after the snapshot\n", string(data), "the edit survives the rollback")
	pin, err := readCurrentPinVersion(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, "1.0.0", pin)
	cfg, err := config.LoadConfigLenient(config.DefaultPaths(root).ConfigPath)
	require.NoError(t, err)
	require.Len(t, cfg.Instructions.Local, 2, "configuration still owns the kept file")
}

// A project without config.toml is seeded inside the snapshotted conversion
// step, so its skill imports can edit configuration before the pin advances.
func TestUpgradeConvertsSkillsWhenConfigIsMissing(t *testing.T) {
	root := pristineProject(t, "1.0.0")
	for _, rel := range []string{".agent-layer/config.toml", ".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/01_memory.md"} {
		require.NoError(t, os.Remove(filepath.Join(root, rel)))
	}
	items, err := PlanPristineConversions(root, RealSystem{})
	require.NoError(t, err)
	require.Equal(t, []string{".agent-layer/skills/implement", ".agent-layer/skills/ship-pr"}, conversionPaths(items))
	require.NoError(t, conversionUpgrade(root, &fakeConverter{t: t, root: root}, autoApprovePrompter(), &bytes.Buffer{}))
	require.NoDirExists(t, filepath.Join(root, ".agent-layer/skills/implement"))
	require.DirExists(t, filepath.Join(config.DefaultPaths(root).ImportedSkillsDir, "implement"))
	cfg, err := config.LoadConfigLenient(config.DefaultPaths(root).ConfigPath)
	require.NoError(t, err)
	require.Len(t, cfg.Skills.Imports, 1)
}

func TestPlanPristineConversionsKeepsCopiesWhoseImportWouldConflict(t *testing.T) {
	t.Run("local instruction already uses the import name", func(t *testing.T) {
		root := pristineProject(t, "1.1.0")
		writeTransitionFile(t, root, ".agent-layer/instructions/rules.md", []byte("Project rules.\n"))
		path := config.DefaultPaths(root).ConfigPath
		raw, err := os.ReadFile(path) // #nosec G304 -- test-owned project config.
		require.NoError(t, err)
		raw = append(raw, []byte("\n[[instructions.local]]\nselectors = [\"rules.md\"]\norder = 20\n")...)
		require.NoError(t, os.WriteFile(path, raw, 0o600)) // #nosec G703 -- test-owned project config.
		cfg, err := config.LoadConfigLenient(path)
		require.NoError(t, err)
		require.NoError(t, config.ValidateInstructions(path, cfg.Instructions), "the project is valid before conversion")
		items, err := PlanPristineConversions(root, RealSystem{})
		require.NoError(t, err)
		require.Equal(t, []string{".agent-layer/skills/implement", ".agent-layer/skills/ship-pr", ".agent-layer/instructions/01_memory.md"}, conversionPaths(items))
	})
	t.Run("manually managed catalog block", func(t *testing.T) {
		root := pristineProject(t, "1.1.0")
		path := config.DefaultPaths(root).ConfigPath
		raw, err := os.ReadFile(path) // #nosec G304 -- test-owned project config.
		require.NoError(t, err)
		raw = append(raw, []byte("\n[[skills.imports]]\nrepository = \""+templates.GeneralSkillsRepository+"\"\nselectors = [\"skills/tools/*\"]\n")...)
		require.NoError(t, os.WriteFile(path, raw, 0o600)) // #nosec G703 -- test-owned project config.
		items, err := PlanPristineConversions(root, RealSystem{})
		require.NoError(t, err)
		require.Equal(t, []string{".agent-layer/instructions/00_rules.md", ".agent-layer/instructions/01_memory.md"}, conversionPaths(items),
			"imports never edit a wildcard block, so its shipped skill copies stay user-managed")
	})
}

// After recovery restores configuration, an instruction block without a
// selector is refused with validation guidance instead of panicking.
func TestUnconfiguredInstructionCheckValidatesRecoveredConfig(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
	path := config.DefaultPaths(root).ConfigPath
	raw, err := os.ReadFile(path) // #nosec G304 -- test-owned project config.
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(raw, []byte("\n[[instructions.local]]\nselectors = []\norder = 0\n")...), 0o600)) // #nosec G703 -- test-owned project config.
	require.ErrorContains(t, checkUnconfiguredInstructions(root, RealSystem{}), "each instructions block requires exactly one selector")
}
