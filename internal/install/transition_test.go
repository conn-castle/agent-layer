package install

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func writeTransitionFile(t *testing.T, root, rel string, raw []byte) {
	t.Helper()
	dest := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o750))
	require.NoError(t, os.WriteFile(dest, raw, 0o600)) // #nosec G703 -- fixture paths are test-controlled and rooted in t.TempDir.
}

func TestTransitionRefusalsPreserveProject(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		setup    func(*testing.T, string)
		sentinel error
	}{
		{"old pin", func(t *testing.T, r string) { writeTransitionFile(t, r, pinVersionRelPath, []byte("0.24.3\n")) }, ErrUnsupportedSource},
		{"old baseline", func(t *testing.T, r string) {
			require.NoError(t, os.Remove(filepath.Join(r, pinVersionRelPath)))
			state, err := readManagedBaselineState(r, RealSystem{})
			require.NoError(t, err)
			state.BaselineVersion = "0.24.3"
			require.NoError(t, writeManagedBaselineState(r, RealSystem{}, state))
		}, ErrUnsupportedSource},
		{"pristine skill", func(t *testing.T, r string) {
			writeTransitionFile(t, r, ".agent-layer/skills/ship-pr/SKILL.md", testutil.TransitionFixture(t, "ship-pr.md"))
		}, ErrUnfinishedTransition},
		{"pristine instruction", func(t *testing.T, r string) {
			writeTransitionFile(t, r, ".agent-layer/instructions/00_rules.md", testutil.TransitionFixture(t, "00_rules.md"))
		}, ErrUnfinishedTransition},
		{"gemini", func(t *testing.T, r string) {
			writeTransitionFile(t, r, upgradeMigrationConfigPath, []byte("[agents.gemini]\nenabled=true\n"))
		}, ErrUnfinishedTransition},
		{"claude alias", func(t *testing.T, r string) {
			writeTransitionFile(t, r, upgradeMigrationConfigPath, []byte("[agents.claude-vscode]\nenabled=true\n"))
		}, ErrUnfinishedTransition},
		{"MCP alias", func(t *testing.T, r string) {
			writeTransitionFile(t, r, upgradeMigrationConfigPath, []byte("[[mcp.servers]]\nclients=[\"gemini\"]\n"))
		}, ErrUnfinishedTransition},
		{"retired model without enabled", func(t *testing.T, r string) {
			writeTransitionFile(t, r, upgradeMigrationConfigPath, []byte("[approvals]\nmode=\"all\"\n[agents.antigravity.agent_specific]\nmodel=\"Gemini 3.1 Pro (High)\"\n"))
		}, ErrUnfinishedTransition},
		{"journal v2", func(t *testing.T, r string) {
			writeTransitionFile(t, r, ".agent-layer/instructions-imported/.staging/journal.json", []byte(`{"version":2}`))
		}, ErrUnfinishedTransition},
		{"retirements", func(t *testing.T, r string) {
			writeTransitionFile(t, r, ".agent-layer/skills-imported/.staging/journal.json", []byte(`{"version":1,"local_retirements":["ship-pr"]}`))
		}, ErrUnfinishedTransition},
		{"local backup", func(t *testing.T, r string) {
			writeTransitionFile(t, r, ".agent-layer/skills-imported/.staging/local-ship-pr/SKILL.md", []byte("preserve"))
		}, ErrUnfinishedTransition},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
			scenario.setup(t, root)
			// Ordinary recovery must not consume this evidence before any refusal.
			writeTransitionFile(t, root, ".agent-layer/skills-imported/.staging/pending.txt", []byte("keep staging"))
			before := testutil.SnapshotEvidence(t, root)
			_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.1.0"})
			require.ErrorIs(t, err, scenario.sentinel)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0"})
			require.ErrorIs(t, err, scenario.sentinel)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		})
	}
}

func TestTransitionGateAcceptsCompletedAndUserManagedSources(t *testing.T) {
	for _, pin := range []string{"1.0.0", "1.1.0", ""} {
		t.Run(pin, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: pin}))
			writeTransitionFile(t, root, ".agent-layer/skills/ship-pr/SKILL.md", []byte("---\nname: ship-pr\ndescription: User workflow.\n---\nCustom content\n"))
			for _, name := range []string{"00_rules.md", "01_memory.md"} {
				writeTransitionFile(t, root, ".agent-layer/instructions/"+name, []byte("Custom "+name))
			}
			configureLocalInstructions(t, root, "00_rules.md", "01_memory.md")
			before := testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/ship-pr"), filepath.Join(root, ".agent-layer/instructions"), filepath.Join(root, upgradeMigrationConfigPath))
			require.NoError(t, CheckUpgradeSource(root, RealSystem{}))
			_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.1.0"})
			require.NoError(t, err)
			require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0"}))
			finalPin, err := readCurrentPinVersion(root, RealSystem{})
			require.NoError(t, err)
			require.Equal(t, "1.1.0", finalPin)
			require.Equal(t, before, testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer/skills/ship-pr"), filepath.Join(root, ".agent-layer/instructions"), filepath.Join(root, upgradeMigrationConfigPath)))
		})
	}
	root := t.TempDir()
	before := testutil.SnapshotEvidence(t, root)
	require.NoError(t, CheckUpgradeSource(root, RealSystem{}))
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestUpgradeInstructionPreflightPreservesUnorderedSources(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
	writeTransitionFile(t, root, ".agent-layer/instructions/custom.md", []byte("custom"))
	before := testutil.SnapshotEvidence(t, root)
	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.EqualError(t, err, config.UnconfiguredLocalInstruction(filepath.Join(root, ".agent-layer/instructions/custom.md"), config.InstructionsConfig{}).Error())
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
	err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter()})
	require.EqualError(t, err, config.UnconfiguredLocalInstruction(filepath.Join(root, ".agent-layer/instructions/custom.md"), config.InstructionsConfig{}).Error())
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestUpgradeInstructionPreflightRejectsInvalidBlocksBeforeMutation(t *testing.T) {
	const local = "[[instructions.local]]\nselectors = [\"custom.md\"]\n"
	const imported = "[[instructions.imports]]\nrepository = \"local-repo\"\nselectors = [\"instructions/imported.md\"]\n"
	for _, scenario := range []struct {
		name   string
		blocks string
		want   string
	}{
		{"missing local order", local, "requires a nonnegative integer order"},
		{"missing imported order", imported, "requires a nonnegative integer order"},
		{"negative local order", local + "order = -1\n", "requires a nonnegative integer order"},
		{"negative imported order", imported + "order = -1\n", "requires a nonnegative integer order"},
		{"duplicate local orders", local + "order = 0\n[[instructions.local]]\nselectors = [\"other.md\"]\norder = 0\n", "duplicate instruction order 0"},
		{"duplicate imported orders", imported + "order = 0\n[[instructions.imports]]\nrepository = \"local-repo\"\nselectors = [\"instructions/other.md\"]\norder = 0\n", "duplicate instruction order 0"},
		{"duplicate orders across tiers", local + "order = 0\n" + imported + "order = 0\n", "duplicate instruction order 0"},
		{"multiple selectors", "[[instructions.local]]\nselectors = [\"custom.md\", \"other.md\"]\norder = 0\n", "requires exactly one selector"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}))
			raw, err := os.ReadFile(filepath.Join(root, upgradeMigrationConfigPath)) // #nosec G304 -- test-owned configuration.
			require.NoError(t, err)
			writeTransitionFile(t, root, upgradeMigrationConfigPath, append(raw, []byte("\n"+scenario.blocks)...))
			writeTransitionFile(t, root, ".agent-layer/instructions/custom.md", []byte("Project-owned instructions.\n"))
			writeTransitionFile(t, root, ".agent-layer/skills-imported/.staging/pending.txt", []byte("Preserve recovery evidence.\n"))
			before := testutil.SnapshotEvidence(t, root)

			_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.1.0"})
			require.ErrorContains(t, err, scenario.want)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0"})
			require.ErrorContains(t, err, scenario.want)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		})
	}
}

func TestRetiredOperationRefusesEntirePlanBeforeMutation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}))
	writeTransitionFile(t, root, ".agent-layer/old.txt", []byte("keep"))
	writeTransitionFile(t, root, ".agent-layer/sync.lock", nil)
	withMigrationManifestOverride(t, "1.1.0", `{"schema_version":1,"target_version":"1.1.0","min_prior_version":"1.0.0","operations":[{"id":"a-rename","kind":"rename_file","from":".agent-layer/old.txt","to":".agent-layer/new.txt","rationale":"test"},{"id":"b-retired","kind":"claim_skill_projection_roots","rationale":"test"}]}`)
	before := testutil.SnapshotEvidence(t, root)
	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.1.0"})
	require.ErrorIs(t, err, ErrUnsupportedSource)
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
	err = Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "1.1.0"})
	require.ErrorIs(t, err, ErrUnsupportedSource)

	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestRollbackFloorPrecedesRecovery(t *testing.T) {
	for _, pin := range []string{"0.24.3", "1.0.0", ""} {
		t.Run(pin, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.1.0"}))
			entries := []upgradeSnapshotEntry{}
			if pin != "" {
				entries = append(entries, upgradeSnapshotEntry{Path: pinVersionRelPath, Kind: upgradeSnapshotEntryKindFile, ContentBase64: base64.StdEncoding.EncodeToString([]byte(pin + "\n"))})
			}
			snapshot := upgradeSnapshot{SchemaVersion: upgradeSnapshotSchemaVersion, SnapshotID: "floor", CreatedAtUTC: time.Now().UTC().Format(time.RFC3339), Status: upgradeSnapshotStatusApplied, Entries: entries}
			require.NoError(t, (&installer{root: root, sys: RealSystem{}}).writeUpgradeSnapshot(snapshot, false))
			writeTransitionFile(t, root, ".agent-layer/skills-imported/.staging/journal.json", []byte(`{"version":1,"committed":true}`))
			before := testutil.SnapshotEvidence(t, root)
			err := RollbackUpgradeSnapshot(root, "floor", RollbackUpgradeSnapshotOptions{System: RealSystem{}})
			if pin == "0.24.3" {
				require.ErrorIs(t, err, ErrUnsupportedSource)
				require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			} else {
				require.NoError(t, err)
				require.NoDirExists(t, skilljournal.StagingRoot(filepath.Join(root, ".agent-layer/skills-imported")))
			}
		})
	}
}

func TestTransitionDetectorReportsAllPristineFindingsAndRefusesLinks(t *testing.T) {
	root := t.TempDir()
	writeTransitionFile(t, root, ".agent-layer/skills/ship-pr/SKILL.md", testutil.TransitionFixture(t, "ship-pr.md"))
	writeTransitionFile(t, root, ".agent-layer/instructions/00_rules.md", testutil.TransitionFixture(t, "00_rules.md"))
	before := testutil.SnapshotEvidence(t, root)
	var findings TransitionFindings
	require.ErrorAs(t, CheckTransitionState(root, RealSystem{}), &findings)
	require.Len(t, findings, 2)
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
	require.NoError(t, os.RemoveAll(filepath.Join(root, ".agent-layer/skills")))
	outside := t.TempDir()
	writeTransitionFile(t, outside, "ship-pr/SKILL.md", testutil.TransitionFixture(t, "ship-pr.md"))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, ".agent-layer/skills")))
	before = testutil.SnapshotEvidence(t, root, outside)
	require.Error(t, CheckTransitionState(root, RealSystem{}))
	require.Equal(t, before, testutil.SnapshotEvidence(t, root, outside))
}

func TestEveryPublishedManifestStillRecognizesHistoricalSchemas(t *testing.T) {
	versions, err := listMigrationManifestVersions()
	require.NoError(t, err)
	require.NotEmpty(t, versions)
	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			_, _, err := loadUpgradeMigrationManifestByVersion(version)
			require.NoError(t, err)
		})
	}
	_, err = loadAllTemplateManifests()
	require.NoError(t, err)
}

func TestUnknownSourceDoesNotScheduleHistoricalOperations(t *testing.T) {
	root := t.TempDir()
	writeTransitionFile(t, root, ".agent-layer/skills/agent-dispatch/SKILL.md", []byte("custom content"))
	before := testutil.SnapshotEvidence(t, root)
	plan, err := (&installer{root: root, sys: RealSystem{}, pinVersion: "1.1.0"}).planUpgradeMigrations()
	require.NoError(t, err)
	require.Equal(t, UpgradeMigrationSourceUnknown, plan.report.SourceVersionOrigin)
	require.Equal(t, "1.1.0", plan.report.TargetVersion)
	require.Empty(t, plan.executable)
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}

func TestUpgradeRepairsMissingConfigWithTrackedLocalInstructions(t *testing.T) {
	root := t.TempDir()
	source := []byte("Project-specific tracked instruction.\n")
	writeTransitionFile(t, root, ".agent-layer/instructions/project.md", source)
	require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, PinVersion: "1.1.0", Prompter: autoApprovePrompter()}))
	raw, err := os.ReadFile(filepath.Join(root, upgradeMigrationConfigPath)) // #nosec G304 -- test-owned configuration.
	require.NoError(t, err)
	_, err = config.ParseConfig(raw, "config.toml")
	require.NoError(t, err)
	preserved, err := os.ReadFile(filepath.Join(root, ".agent-layer/instructions/project.md")) // #nosec G304 -- test-owned tracked instruction.
	require.NoError(t, err)
	require.Equal(t, source, preserved)
}

func TestUnknownSourceRunsOnlySupportedSourceAgnosticMigrations(t *testing.T) {
	for _, target := range []string{"1.1.0", ""} {
		t.Run("target="+target, func(t *testing.T) {
			root := t.TempDir()
			writeTransitionFile(t, root, upgradeMigrationConfigPath, []byte("[agents.grok]\n"))
			writeTransitionFile(t, root, "dependent.txt", []byte("keep"))
			withMigrationManifestChainOverride(t, map[string]string{
				"0.24.3": `{"schema_version":1,"target_version":"0.24.3","min_prior_version":"0.10.2","operations":[{"id":"retired","kind":"claim_skill_projection_roots","rationale":"retired","source_agnostic":true}]}`,
				"1.0.0":  `{"schema_version":1,"target_version":"1.0.0","min_prior_version":"0.10.2","operations":[{"id":"at-floor","kind":"claim_skill_projection_roots","rationale":"floor excluded","source_agnostic":true}]}`,
				"1.1.0":  `{"schema_version":1,"target_version":"1.1.0","min_prior_version":"1.0.0","operations":[{"id":"a-required","kind":"config_set_default","rationale":"new required field","source_agnostic":true,"key":"agents.grok.enabled","value":true},{"id":"b-dependent","kind":"delete_file","rationale":"requires known source","path":"dependent.txt"}]}`,
			})
			require.NoError(t, CheckUpgradeSource(root, RealSystem{}))
			plan, err := (&installer{root: root, sys: RealSystem{}, pinVersion: target}).planUpgradeMigrations()
			require.NoError(t, err)
			require.Equal(t, UpgradeMigrationSourceUnknown, plan.report.SourceVersionOrigin)
			require.Len(t, plan.executable, 1)
			require.Equal(t, "a-required", plan.executable[0].ID)
			require.Len(t, plan.report.Entries, 2)
			require.Equal(t, UpgradeMigrationStatusSkippedUnknownSource, plan.report.Entries[1].Status)
			require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, PinVersion: target, Prompter: autoApprovePrompter()}))
			cfg, err := config.LoadConfigLenient(filepath.Join(root, upgradeMigrationConfigPath))
			require.NoError(t, err)
			require.NotNil(t, cfg.Agents.Grok.Enabled)
			require.True(t, *cfg.Agents.Grok.Enabled)
			content, err := os.ReadFile(filepath.Join(root, "dependent.txt")) // #nosec G304 -- test-owned migration input.
			require.NoError(t, err)
			require.Equal(t, []byte("keep"), content)
		})
	}
}

func TestUnknownSourceRejectsRetiredKindsInSupportedRange(t *testing.T) {
	root := t.TempDir()
	writeTransitionFile(t, root, upgradeMigrationConfigPath, []byte("[agents.grok]\n"))
	withMigrationManifestOverride(t, "1.1.0", `{"schema_version":1,"target_version":"1.1.0","min_prior_version":"1.0.0","operations":[{"id":"retired","kind":"claim_skill_projection_roots","rationale":"unsupported"}]}`)
	before := testutil.SnapshotEvidence(t, root)
	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "1.1.0"})
	require.ErrorIs(t, err, ErrUnsupportedSource)
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}
