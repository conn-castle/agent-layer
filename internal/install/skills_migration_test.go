package install

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func recordMovedImport(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-layer", "skills-imported", "ship-pr")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("imported customization\n"), 0o600))
	lock := skilllock.New()
	lock.Upsert(skilllock.Entry{Name: "ship-pr", Repository: templates.GeneralSkillsRepository, Selector: "skills/development/ship-pr", SelectedPath: "skills/development/ship-pr", ResolvedRef: "main", RefKind: "branch", Tracking: "tracked", Commit: strings.Repeat("a", 40), TreeHash: "sha256:" + strings.Repeat("b", 64)})
	data, err := lock.Marshal()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), data, 0o600))
}

func TestRetiredLocalRootSymlinkIsProtectedWithoutTraversal(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}}))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0o600))
	local := filepath.Join(root, ".agent-layer", "skills", "ship-pr")
	require.NoError(t, os.Symlink(outside, local))
	inst := &installer{root: root, sys: RealSystem{}}
	known, err := inst.buildKnownPaths()
	require.NoError(t, err)
	require.Contains(t, known, local)
	require.NotContains(t, known, filepath.Join(local, "keep"))
}

func TestMigrationGuardsRefuseBeforeChangingInstalledState(t *testing.T) {
	for _, kind := range []string{"local file", "local directory", "old pin", "local instruction"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: skillmigration.MinimumCLI}))
			recordMovedImport(t, root)
			perm := uint32(0o600)
			entry := upgradeSnapshotEntry{Path: ".agent-layer/skills/ship-pr/SKILL.md", Kind: upgradeSnapshotEntryKindFile, Perm: &perm, ContentBase64: base64.StdEncoding.EncodeToString([]byte("custom local"))}
			if kind == "local instruction" {
				entry.Path = ".agent-layer/instructions/00_rules.md"
				dir := filepath.Join(root, ".agent-layer", "instructions-imported")
				require.NoError(t, os.MkdirAll(dir, 0o750))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "rules.md"), []byte("imported rule edits"), 0o600))
				lock := skilllock.NewInstructions()
				lock.Upsert(skilllock.Entry{Name: "rules.md", Repository: templates.GeneralSkillsRepository, Selector: "instructions/rules.md", SelectedPath: "instructions/rules.md", ResolvedRef: "main", RefKind: "branch", Tracking: "tracked", Commit: strings.Repeat("a", 40), TreeHash: "sha256:" + strings.Repeat("b", 64)})
				data, err := lock.Marshal()
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions.lock.json"), data, 0o600))
			}
			if kind == "local directory" {
				entry.Path, entry.Kind, entry.ContentBase64 = ".agent-layer/skills/ship-pr", upgradeSnapshotEntryKindDir, ""
			}
			if kind == "old pin" {
				entry.Path, entry.ContentBase64 = pinVersionRelPath, base64.StdEncoding.EncodeToString([]byte("0.99.9\n"))
			}
			snapshot := upgradeSnapshot{SchemaVersion: upgradeSnapshotSchemaVersion, SnapshotID: "migration-guard", CreatedAtUTC: time.Now().UTC().Format(time.RFC3339), Status: upgradeSnapshotStatusApplied, Entries: []upgradeSnapshotEntry{entry}}
			inst := &installer{root: root, sys: RealSystem{}}
			require.NoError(t, inst.writeUpgradeSnapshot(snapshot, false))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "sync.lock"), nil, 0o600))
			before := testutil.SnapshotEvidence(t, root)
			if kind == "old pin" {
				err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.99.9"})
				require.ErrorContains(t, err, "cannot select pre-migration CLI")
				require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			}
			require.Error(t, RollbackUpgradeSnapshot(root, "migration-guard", RollbackUpgradeSnapshotOptions{System: RealSystem{}}))
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		})
	}
}

func TestInstallAndRollbackRecoverBeforeMutation(t *testing.T) {
	for _, operation := range []string{"install", "rollback"} {
		for _, future := range []bool{false, true} {
			root := t.TempDir()
			stage := skilljournal.StagingRoot(filepath.Join(root, ".agent-layer", "skills-imported"))
			require.NoError(t, os.MkdirAll(stage, 0o750))
			raw := "{\"version\":1,\"committed\":true}"
			if future {
				raw = "{\"version\":3}"
			}
			require.NoError(t, os.WriteFile(filepath.Join(stage, skilljournal.FileName), []byte(raw), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", SyncLockFileName), nil, 0o600))
			before := testutil.SnapshotEvidence(t, root)
			var err error
			if operation == "install" {
				err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: skillmigration.MinimumCLI})
			} else {
				err = RollbackUpgradeSnapshot(root, "absent", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
			}
			if future {
				require.ErrorContains(t, err, "unsupported schema version 3")
				require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			} else {
				require.NoDirExists(t, stage)
				if operation == "install" {
					require.NoError(t, err)
				}
			}
		}
	}
}

func TestUpgradeRollbackPreservesUnmanagedSkillEdits(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: skillmigration.MinimumCLI}))
	custom := filepath.Join(root, ".agent-layer", "skills", "user-skill", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(custom), 0o750))
	require.NoError(t, os.WriteFile(custom, []byte("before upgrade"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", UpgradeKeepListFileName), []byte(".agent-layer/skills/user-skill\n"), 0o600))
	retained := filepath.Join(root, ".agent-layer", "skills", "dispatch-agent", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(retained), 0o750))
	require.NoError(t, os.WriteFile(retained, []byte("custom dispatch-agent"), 0o600))
	require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: skillmigration.MinimumCLI}))
	content, err := os.ReadFile(retained) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	require.NotEqual(t, "custom dispatch-agent", string(content))
	snapshot := latestSnapshot(t, root)
	require.NoError(t, os.WriteFile(custom, []byte("after upgrade"), 0o600))
	newSkill := filepath.Join(root, ".agent-layer", "skills", "new-user-skill")
	require.NoError(t, os.MkdirAll(newSkill, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(newSkill, "SKILL.md"), []byte("created after upgrade"), 0o600))
	before := testutil.SnapshotEvidence(t, filepath.Dir(custom), newSkill)
	require.NoError(t, RollbackUpgradeSnapshot(root, snapshot.SnapshotID, RollbackUpgradeSnapshotOptions{System: RealSystem{}}))
	require.Equal(t, before, testutil.SnapshotEvidence(t, filepath.Dir(custom), newSkill))
	content, err = os.ReadFile(retained) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	require.Equal(t, "custom dispatch-agent", string(content))
}
