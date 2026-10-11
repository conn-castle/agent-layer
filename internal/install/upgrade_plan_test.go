package install

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildUpgradePlan_ListsUserPathsAndPreviewsNonRegularRemovals(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "bare layout"
		if active {
			name = "active template roots"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
			if active {
				seedWorkflowBundleForTest(t, root)
			}
			skill := ".agent-layer/skills/my-workflow"
			instruction := ".agent-layer/instructions/10_project.md"
			link := ".agent-layer/instructions/local-link"
			skillPath := filepath.Join(root, filepath.FromSlash(skill))
			require.NoError(t, os.MkdirAll(skillPath, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("my workflow\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(instruction)), []byte("project rules\n"), 0o600))
			linkPath := filepath.Join(root, filepath.FromSlash(link))
			require.NoError(t, os.Symlink("missing-target", linkPath))
			// Previews must not read non-regular paths.
			sys := newFaultSystem(RealSystem{})
			sys.readErrs[skillPath] = errors.New("directory must not be read")
			sys.readErrs[linkPath] = errors.New("symlink must not be read")
			if active {
				// Ensure rename detection runs while the dangling orphan link exists.
				require.NoError(t, os.Remove(filepath.Join(root, ".agent-layer", "commands.allow")))
			}

			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: sys})
			require.NoError(t, err)
			for _, path := range []string{skill} {
				change := findUpgradeChange(plan.TemplateRemovalsOrOrphans, path)
				require.NotNil(t, change, "missing removal %s", path)
			}
			require.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, skill+"/SKILL.md"))
			previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: sys})
			require.NoError(t, err)
			for _, path := range []string{skill} {
				preview, ok := previews[path]
				require.True(t, ok, "missing preview %s", path)
				require.Equal(t, path, preview.Path)
				require.Empty(t, preview.UnifiedDiff)
			}
			require.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, instruction))
			require.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, link))
		})
	}
}

func TestBuildUpgradePlan_UnknownDeletionsMatchApplyScan(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
	for _, path := range []string{
		".agent-layer/skills/my-workflow/SKILL.md",
		".agent-layer/instructions/10_project.md",
		".agent-layer/tmp/run.log",
		"docs/agent-layer/NOTES.md",
		".agent-layer/skills/kept-skill/SKILL.md",
		".agent-layer/skills/partly-kept/keep.md",
		".agent-layer/skills/partly-kept/remove.md",
		".agent-layer/state/local-runtime.json",
		".agent-layer/skills-imported/imported-skill/SKILL.md",
	} {
		abs := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o700))
		require.NoError(t, os.WriteFile(abs, []byte("local\n"), 0o600))
	}
	keepData := ".agent-layer/skills/kept-skill\n.agent-layer/skills/partly-kept/keep.md\n"
	require.NoError(t, os.WriteFile(inst.upgradeKeepListPath(), []byte(keepData), 0o600))
	unknowns, err := inst.scanCurrentUnknowns()
	require.NoError(t, err)
	tmp, nonTmp := inst.partitionTmpUnknowns(unknowns)
	require.Len(t, tmp, 1)
	expected := make([]string, 0, len(nonTmp))
	for _, path := range nonTmp {
		expected = append(expected, filepath.ToSlash(inst.relativePath(path)))
	}
	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)
	actual := make([]string, 0, len(plan.TemplateRemovalsOrOrphans))
	for _, change := range plan.TemplateRemovalsOrOrphans {
		actual = append(actual, change.Path)
	}
	require.Equal(t, expected, actual)
	require.True(t, sort.StringsAreSorted(actual))
	require.Equal(t, []string{
		".agent-layer/skills/my-workflow",
		".agent-layer/skills/partly-kept/remove.md",
		"docs/agent-layer/NOTES.md",
	}, actual)
}

func TestPlanUnknownDeletions_ExcludesRepresentedDeletedKeptAndKnownPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	paths := []string{
		".agent-layer/skills/orphan-dir/file.md",
		".agent-layer/skills/rename-dir/file.md",
		".agent-layer/instructions/orphan.md",
		".agent-layer/instructions/rename.md",
		".agent-layer/instructions/migrated.md",
		".agent-layer/instructions/legacy-allow.md",
		".agent-layer/instructions/kept.md",
	}
	for _, path := range paths {
		abs := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o700))
		require.NoError(t, os.WriteFile(abs, []byte("local\n"), 0o600))
	}
	effects, err := inst.planMigrationPathEffects([]upgradeMigrationOperation{
		{Kind: upgradeMigrationKindDeleteFile, Path: paths[4]},
		{Kind: upgradeMigrationKindRenameFile, From: paths[5], To: ".agent-layer/commands.allow"},
	})
	require.NoError(t, err)
	changes, err := inst.planUnknownDeletions(
		[]upgradeChangeWithTemplate{{path: paths[0]}, {path: paths[2]}},
		[]UpgradeRename{{From: paths[1]}, {From: paths[3]}},
		effects,
		upgradeKeepList{paths[6]: {}},
	)
	require.NoError(t, err)
	require.Empty(t, changes)
}

func TestPlanUnknownDeletions_ReportsWhereMigrationsLeaveUnknownPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	for _, path := range []string{
		".agent-layer/skills/old-name/SKILL.md",
		".agent-layer/skills/new-name/SKILL.md",
		".agent-layer/skills/playwright-cli/SKILL.md",
		".agent-layer/skills/playwright-cli/my-notes.md",
		".agent-layer/skills/kept-old/SKILL.md",
		".agent-layer/skills/flat.md",
		".agent-layer/instructions/appended.md",
	} {
		abs := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o700))
		require.NoError(t, os.WriteFile(abs, []byte("local\n"), 0o600))
	}
	effects, err := inst.planMigrationPathEffects([]upgradeMigrationOperation{
		// A chained rename onto an existing unknown path is reported once.
		{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/skills/old-name", To: ".agent-layer/skills/mid-name"},
		{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/skills/mid-name", To: ".agent-layer/skills/new-name"},
		// A rename into a known catalog directory leaves its user file unknown.
		{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/skills/playwright-cli", To: ".agent-layer/skills/playwright"},
		// Keeping a path does not keep the destination a migration moves it to.
		{Kind: upgradeMigrationKindRenameFile, From: ".agent-layer/skills/kept-old", To: ".agent-layer/skills/kept-new"},
		{Kind: upgradeMigrationKindMigrateSkillsFormat, Path: ".agent-layer/skills"},
		{Kind: upgradeMigrationKindAppendToFile, Path: ".agent-layer/instructions/appended.md"},
		{Kind: upgradeMigrationKindAppendToFile, Path: ".agent-layer/instructions/04_conventions.md"},
	})
	require.NoError(t, err)
	changes, err := inst.planUnknownDeletions(nil, nil, effects, upgradeKeepList{".agent-layer/skills/kept-old": {}})
	require.NoError(t, err)
	paths := make([]string, 0, len(changes))
	for _, change := range changes {
		paths = append(paths, change.path)
	}
	require.ElementsMatch(t, []string{
		".agent-layer/skills/flat",
		".agent-layer/skills/kept-new",
		".agent-layer/skills/new-name",
	}, paths)
}

func TestBuildUpgradePlan_ListsRenamedSkillThatApplyWouldDelete(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "0.12.0"}))
	skill := filepath.Join(root, ".agent-layer", "skills", "review-scope", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skill), 0o700))
	require.NoError(t, os.WriteFile(skill, []byte("legacy review skill\n"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)
	require.NotNil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/skills/review-uncommitted-code"))
	require.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/skills/review-scope"))
	_, err = BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}})
	require.NoError(t, err)
}

func TestBuildUpgradePlan_ListsDanglingRenameSourceThatApplyWouldDelete(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "0.12.0"}))
	require.NoError(t, os.Remove(filepath.Join(root, ".agent-layer", "config.toml")))
	source := ".agent-layer/skills/review-scope"
	sourcePath := filepath.Join(root, filepath.FromSlash(source))
	require.NoError(t, os.Symlink("missing-target", sourcePath))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)
	require.NotNil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, source))
	require.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/skills/review-uncommitted-code"))
	previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}})
	require.NoError(t, err)
	require.Contains(t, previews, source)
	require.Empty(t, previews[source].UnifiedDiff)

	var deleted []string
	prompter := autoApprovePrompter()
	prompter.DeleteUnknownAllFunc = func(paths []string) (bool, error) {
		deleted = append(deleted, paths...)
		return true, nil
	}
	require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: prompter}))
	require.Contains(t, deleted, source)
	_, err = os.Lstat(sourcePath)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestPlanMigrationPathEffects_RenameSourceStatSemantics(t *testing.T) {
	for _, kind := range []upgradeMigrationOperationKind{upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact} {
		t.Run(string(kind), func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, ".agent-layer", "skills", "old")
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "target"), []byte("local\n"), 0o600))
			require.NoError(t, os.Symlink("target", filepath.Join(dir, "valid")))
			require.NoError(t, os.Symlink("missing-target", filepath.Join(dir, "dangling")))
			ops := []upgradeMigrationOperation{
				{Kind: kind, From: ".agent-layer/skills/old", To: ".agent-layer/skills/moved"},
				{Kind: kind, From: ".agent-layer/skills/moved/dangling", To: ".agent-layer/skills/destination"},
				{Kind: kind, From: ".agent-layer/skills/moved/valid", To: ".agent-layer/skills/moved/renamed"},
				{Kind: kind, From: ".agent-layer/skills/moved/renamed", To: ".agent-layer/skills/moved/final"},
			}
			inst := &installer{root: root, sys: RealSystem{}}
			effects, err := inst.planMigrationPathEffects(ops)
			require.NoError(t, err)
			require.Contains(t, effects.tree, ".agent-layer/skills/moved/dangling")
			require.NotContains(t, effects.tree, ".agent-layer/skills/destination")
			require.Contains(t, effects.tree, ".agent-layer/skills/moved/final")
			require.NotContains(t, effects.tree, ".agent-layer/skills/moved/valid")
			require.NotContains(t, effects.tree, ".agent-layer/skills/moved/renamed")

			failure := errors.New("stat denied")
			sys := newFaultSystem(RealSystem{})
			sys.statErrs[filepath.Join(dir, "dangling")] = failure
			inst.sys = sys
			_, err = inst.planMigrationPathEffects(ops)
			require.ErrorIs(t, err, failure)

			inst.sys = RealSystem{}
			for _, op := range ops {
				_, err = inst.executeRenameMigration(op.From, op.To)
				require.NoError(t, err)
			}
			actual, err := inst.planMigrationPathEffects(nil)
			require.NoError(t, err)
			require.Equal(t, actual.tree, effects.tree)
		})
	}
}

func TestBuildUpgradePlan_DetectsCategoriesAndRename(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	seedWorkflowBundleForTest(t, root)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(baselineStateRelPath))); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}

	// Simulate an unchanged local docs file relative to its legacy snapshot,
	// while the embedded template has since changed.
	oldBacklog := []byte("# BACKLOG\n\nLegacy header\n\n<!-- ENTRIES START -->\n")
	backlogPath := filepath.Join(root, "docs", "agent-layer", "BACKLOG.md")
	if err := os.WriteFile(backlogPath, oldBacklog, 0o600); err != nil {
		t.Fatalf("write backlog: %v", err)
	}

	baselineBacklogPath := filepath.Join(root, ".agent-layer", "templates", "docs", "BACKLOG.md")
	if err := os.WriteFile(baselineBacklogPath, oldBacklog, 0o600); err != nil {
		t.Fatalf("write baseline backlog: %v", err)
	}

	// Simulate a local customization in memory docs.
	issuesPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("read issues template: %v", err)
	}
	customIssues := strings.Replace(string(issuesTemplate), "<!-- ENTRIES START -->\n", "<!-- ENTRIES START -->\n\n- issue from repo\n", 1)
	if err := os.WriteFile(issuesPath, []byte(customIssues), 0o600); err != nil {
		t.Fatalf("write issues: %v", err)
	}

	// Simulate a rename candidate for a selected catalog skill. LICENSE keeps
	// the directory active after SKILL.md is moved to the orphan path.
	playwrightDir := filepath.Join(root, ".agent-layer", "skills", "benchmark")
	manager := (&installer{root: root, sys: RealSystem{}}).templates()
	if err := manager.writeTemplateDirCached(templateDir{templateRoot: "skills-catalog/benchmark", destRoot: playwrightDir}); err != nil {
		t.Fatalf("seed playwright catalog skill: %v", err)
	}
	playwrightPath := filepath.Join(playwrightDir, "SKILL.md")
	if err := os.Remove(playwrightPath); err != nil {
		t.Fatalf("remove playwright skill: %v", err)
	}
	playwrightTemplate, err := templates.Read("skills-catalog/benchmark/SKILL.md")
	if err != nil {
		t.Fatalf("read playwright template skill: %v", err)
	}
	orphanRenamePath := filepath.Join(playwrightDir, "playwright-legacy.md")
	if err := os.WriteFile(orphanRenamePath, playwrightTemplate, 0o600); err != nil {
		t.Fatalf("write orphan rename path: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{
		TargetPinVersion: "0.7.0",
		System:           RealSystem{},
	})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}

	if len(plan.ConfigKeyMigrations) != 0 {
		t.Fatalf("expected empty config migrations, got %d", len(plan.ConfigKeyMigrations))
	}
	if plan.PinVersionChange.Action != UpgradePinActionUpdate {
		t.Fatalf("expected pin update action, got %s", plan.PinVersionChange.Action)
	}
	if plan.PinVersionChange.Current != "1.2.3" || plan.PinVersionChange.Target != "0.7.0" {
		t.Fatalf("unexpected pin transition: %#v", plan.PinVersionChange)
	}

	backlogUpdate := findUpgradeChange(plan.SectionAwareUpdates, "docs/agent-layer/BACKLOG.md")
	if backlogUpdate == nil {
		t.Fatalf("expected backlog update in section-aware updates")
	}

	// ISSUES.md has a user entry below the marker but its managed section matches
	// the template. Section-aware matching should exclude it from the plan entirely.
	issuesInUpdates := findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md")
	issuesInSection := findUpgradeChange(plan.SectionAwareUpdates, "docs/agent-layer/ISSUES.md")
	if issuesInUpdates != nil || issuesInSection != nil {
		t.Fatal("ISSUES.md should be excluded: managed section matches template, only user entries differ")
	}

	if len(plan.TemplateRenames) == 0 {
		t.Fatalf("expected at least one rename")
	}
	rename := plan.TemplateRenames[0]
	if rename.From != ".agent-layer/skills/benchmark/playwright-legacy.md" {
		t.Fatalf("unexpected rename from path: %s", rename.From)
	}
	if rename.To != ".agent-layer/skills/benchmark/SKILL.md" {
		t.Fatalf("unexpected rename to path: %s", rename.To)
	}
}

func findUpgradeChange(changes []UpgradeChange, path string) *UpgradeChange {
	for _, change := range changes {
		if change.Path == path {
			c := change
			return &c
		}
	}
	return nil
}

func TestBuildUpgradePlan_ValidationErrors(t *testing.T) {
	_, err := BuildUpgradePlan("", UpgradePlanOptions{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), "root path is required") {
		t.Fatalf("expected root validation error, got %v", err)
	}

	root := t.TempDir()
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "install system is required") {
		t.Fatalf("expected system validation error, got %v", err)
	}

	_, err = BuildUpgradePlan(root, UpgradePlanOptions{
		System:           RealSystem{},
		TargetPinVersion: "bad-version",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid pin version") {
		t.Fatalf("expected invalid pin version error, got %v", err)
	}
}

func TestUpgradeEvidence_ManagedDiffBaselineValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		baseline string
	}{
		{name: "valid"},
		{name: "corrupt", baseline: "{bad-json"},
		{name: "invalid", baseline: `{"schema_version":99}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}}))
			require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(commandsAllowRelPath)), []byte("custom command\n"), 0o600))
			if tc.baseline != "" {
				require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(baselineStateRelPath)), []byte(tc.baseline), 0o600))
			}
			_, baselineErr := readManagedBaselineState(root, RealSystem{})
			promptCalled := false
			inst := &installer{
				root: root, sys: RealSystem{},
				prompter: &PromptFuncs{
					OverwriteAllUnifiedPreviewFunc: func(managed, memory []DiffPreview) (bool, bool, error) {
						promptCalled = true
						require.Len(t, managed, 1)
						require.Equal(t, commandsAllowRelPath, managed[0].Path)
						require.Contains(t, managed[0].UnifiedDiff, "-custom command")
						return false, false, nil
					},
				},
			}
			plan, planErr := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
			_, listErr := inst.templates().listManagedDiffs()
			_, lookupErr := inst.lookupDiffPreview(commandsAllowRelPath)
			reviewErr := inst.resolveOverwriteAllDecisions()
			writeErr := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByOverwrite)
			if tc.baseline != "" {
				require.Error(t, baselineErr)
				if tc.name == "corrupt" {
					require.Contains(t, baselineErr.Error(), "decode managed baseline state")
				}
				for _, err := range []error{planErr, listErr, lookupErr, reviewErr, writeErr} {
					require.EqualError(t, err, baselineErr.Error())
				}
				require.False(t, promptCalled)
				require.False(t, inst.overwriteAllDecided)
			} else {
				require.NoError(t, planErr)
				require.NotNil(t, findUpgradeChange(plan.TemplateUpdates, commandsAllowRelPath))
				for _, err := range []error{listErr, lookupErr, reviewErr, writeErr} {
					require.NoError(t, err)
				}
				require.True(t, promptCalled)
			}
		})
	}
}

func TestBuildUpgradePlan_InvalidPinWithoutBaselinePreviewsRepair(t *testing.T) {
	cases := map[string]string{
		"conflict markers": "<<<<<<< HEAD\n0.23.0\n=======\n0.23.1\n>>>>>>> branch\n",
		"non-semver":       "not-a-version\n",
	}
	for name, pin := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := Run(root, Options{System: RealSystem{}}); err != nil {
				t.Fatalf("seed repo: %v", err)
			}
			if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
				t.Fatalf("remove canonical baseline: %v", err)
			}
			if err := os.WriteFile(filepath.Join(root, ".agent-layer", "al.version"), []byte(pin), 0o600); err != nil {
				t.Fatalf("write invalid pin: %v", err)
			}
			allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
			if err := os.WriteFile(allowPath, []byte("# custom allowlist\n"), 0o600); err != nil {
				t.Fatalf("write custom allowlist: %v", err)
			}

			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "0.7.0"})
			if err != nil {
				t.Fatalf("build upgrade plan: %v", err)
			}
			allowUpdate := findUpgradeChange(plan.TemplateUpdates, commandsAllowRelPath)
			if allowUpdate == nil {
				t.Fatal("expected commands.allow update in plan")
			}
			want := UpgradePinVersionDiff{Current: strings.TrimSpace(pin), Target: "0.7.0", Action: UpgradePinActionUpdate}
			if plan.PinVersionChange != want {
				t.Fatalf("pin change = %#v, want %#v", plan.PinVersionChange, want)
			}
		})
	}
}

func TestBuildUpgradePlan_StatErrorOnTemplateEntry(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	sys.statErrs[normalizePath(allowPath)] = errors.New("stat boom")

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{
		TargetPinVersion: "0.7.0",
		System:           sys,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestBuildUpgradePlan_WalkTemplateOrphansErrors(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("read issues template: %v", err)
	}
	issuesPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(issuesPath), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(issuesPath, issuesTemplate, 0o600); err != nil {
		t.Fatalf("write issues evidence: %v", err)
	}

	instructionsRoot := filepath.Join(root, ".agent-layer", "templates", "docs")
	rulesPath := filepath.Join(instructionsRoot, "ISSUES.md")
	if err := os.MkdirAll(instructionsRoot, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.WriteFile(rulesPath, []byte("rules"), 0o600); err != nil {
		t.Fatalf("write instruction evidence: %v", err)
	}
	sys.statErrs[normalizePath(rulesPath)] = errors.New("permission denied")
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: sys, TargetPinVersion: "0.7.0"})
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error from orphan root, got %v", err)
	}

	delete(sys.statErrs, normalizePath(rulesPath))
	sys.walkErrs[normalizePath(instructionsRoot)] = errors.New("walk failed")
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: sys, TargetPinVersion: "0.7.0"})
	if err == nil || !strings.Contains(err.Error(), "walk failed") {
		t.Fatalf("expected walk error, got %v", err)
	}
}

func TestPinVersionDiff_EdgeCases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.2.3", sys: RealSystem{}}
	diff, err := inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff missing file: %v", err)
	}
	if diff.Action != UpgradePinActionSet {
		t.Fatalf("expected set action for missing file, got %s", diff.Action)
	}

	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("write empty pin: %v", err)
	}
	diff, err = inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff empty file: %v", err)
	}
	if diff.Action != UpgradePinActionSet {
		t.Fatalf("expected set action for empty pin, got %s", diff.Action)
	}

	if err := os.WriteFile(path, []byte("not-semver\n"), 0o600); err != nil {
		t.Fatalf("write corrupt pin: %v", err)
	}
	diff, err = inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff corrupt file: %v", err)
	}
	if diff.Action != UpgradePinActionUpdate {
		t.Fatalf("expected update action for corrupt pin, got %s", diff.Action)
	}

	if err := os.WriteFile(path, []byte("# team pin\nv1.2.2\n"), 0o600); err != nil {
		t.Fatalf("write commented pin: %v", err)
	}
	diff, err = inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff commented file: %v", err)
	}
	if diff.Current != "1.2.2" || diff.Action != UpgradePinActionUpdate {
		t.Fatalf("expected update from 1.2.2 for commented pin, got current %q action %s", diff.Current, diff.Action)
	}
}

func TestDetectUpgradeRenames_ErrorAndAmbiguityPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	renames, remainingAdditions, remainingOrphans, err := detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{{path: ".agent-layer/config.toml", templatePath: "missing-template.md"}},
		[]upgradeChangeWithTemplate{{
			path: ".agent-layer/orphan.md",
		}},
	)
	if err == nil {
		t.Fatal("expected template read error")
	}
	if len(renames) != 0 || len(remainingAdditions) != 0 || len(remainingOrphans) != 0 {
		t.Fatal("expected empty results on early template read error")
	}

	validTemplateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	orphanPath := filepath.Join(root, ".agent-layer", "orphan.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o700); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, validTemplateBytes, 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	badReadSys := newFaultSystem(RealSystem{})
	badReadSys.readErrs[normalizePath(orphanPath)] = errors.New("read boom")
	inst.sys = badReadSys
	renames, remainingAdditions, remainingOrphans, err = detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{{path: ".agent-layer/config.toml", templatePath: "config.toml"}},
		[]upgradeChangeWithTemplate{{
			path: ".agent-layer/orphan.md",
		}},
	)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected orphan read error, got %v", err)
	}
	if len(renames) != 0 || len(remainingAdditions) != 0 || len(remainingOrphans) != 0 {
		t.Fatal("expected empty results on orphan read error")
	}

	inst.sys = RealSystem{}
	secondOrphanPath := filepath.Join(root, ".agent-layer", "orphan-2.md")
	if err := os.WriteFile(secondOrphanPath, validTemplateBytes, 0o600); err != nil {
		t.Fatalf("write second orphan: %v", err)
	}
	renames, additions, orphans, err := detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{
			{path: ".agent-layer/config-a.toml", templatePath: "config.toml"},
			{path: ".agent-layer/config-b.toml", templatePath: "config.toml"},
		},
		[]upgradeChangeWithTemplate{
			{
				path: ".agent-layer/orphan.md",
			},
			{
				path: ".agent-layer/orphan-2.md",
			},
		},
	)
	if err != nil {
		t.Fatalf("detectUpgradeRenames ambiguity: %v", err)
	}
	if len(renames) != 0 {
		t.Fatalf("expected no rename for ambiguous hash matches, got %d", len(renames))
	}
	if len(additions) != 2 || len(orphans) != 2 {
		t.Fatalf("expected additions/orphans unchanged on ambiguity, got %d/%d", len(additions), len(orphans))
	}
}

func TestDetectUpgradeRenames_NoCandidates(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	renames, additions, orphans, err := detectUpgradeRenames(inst, nil, nil)
	if err != nil {
		t.Fatalf("detectUpgradeRenames(nil,nil): %v", err)
	}
	if len(renames) != 0 || len(additions) != 0 || len(orphans) != 0 {
		t.Fatalf("expected all empty, got %d/%d/%d", len(renames), len(additions), len(orphans))
	}
}

func TestPinVersionDiff_RemoveNoneAndReadError(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("1.2.3\n"), 0o600); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	inst := &installer{root: root, pinVersion: "", sys: RealSystem{}}
	diff, err := inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff remove: %v", err)
	}
	if diff.Action != UpgradePinActionRemove {
		t.Fatalf("expected remove action, got %s", diff.Action)
	}

	inst.pinVersion = "1.2.3"
	diff, err = inst.templates().pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff none: %v", err)
	}
	if diff.Action != UpgradePinActionNone {
		t.Fatalf("expected none action, got %s", diff.Action)
	}

	readFault := newFaultSystem(RealSystem{})
	readFault.readErrs[normalizePath(pinPath)] = errors.New("read boom")
	inst.sys = readFault
	if _, err := inst.templates().pinVersionDiff(); err == nil {
		t.Fatal("expected read error")
	}
}

func TestBuildUpgradePlan_RemovalsMatchApplyDeletionsAcrossMigrations(t *testing.T) {
	for _, source := range []string{"0.8.0", "0.12.0", "0.14.0"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: source}))
			// Current config keys conflict with old config migrations; file parity does not need config.
			require.NoError(t, os.Remove(filepath.Join(root, ".agent-layer", "config.toml")))
			for _, path := range []string{
				".agent-layer/skills/my-workflow/SKILL.md",
				".agent-layer/skills/flat-user.md",
				".agent-layer/skills/review-scope/SKILL.md",
				".agent-layer/skills/playwright-cli/my-notes.md",
				".agent-layer/instructions/10_project.md",
				".agent-layer/tmp/run.log",
			} {
				abs := filepath.Join(root, filepath.FromSlash(path))
				require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o700))
				require.NoError(t, os.WriteFile(abs, []byte("local\n"), 0o600))
			}
			keep := filepath.Join(root, ".agent-layer", UpgradeKeepListFileName)
			require.NoError(t, os.WriteFile(keep, []byte(".agent-layer/skills/review-scope\n"), 0o600))

			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
			require.NoError(t, err)
			planned := make([]string, 0, len(plan.TemplateRemovalsOrOrphans))
			for _, change := range plan.TemplateRemovalsOrOrphans {
				planned = append(planned, change.Path)
			}

			var deleted []string
			prompter := autoApprovePrompter()
			prompter.DeleteUnknownAllFunc = func(paths []string) (bool, error) {
				for _, path := range paths {
					if !strings.HasPrefix(path, ".agent-layer/tmp/") {
						deleted = append(deleted, path)
					}
				}
				return true, nil
			}
			prompter.ConfirmSkillsMigrationFunc = func([]string, []SkillsMigrationConflict) (bool, error) { return true, nil }
			require.NoError(t, Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: prompter}))
			require.ElementsMatch(t, deleted, planned)
			require.NotEmpty(t, planned)
		})
	}
}

func TestUpgradeChangeAt_OutOfRange(t *testing.T) {
	changes := []upgradeChangeWithTemplate{{path: "a"}}
	if _, ok := upgradeChangeAt(changes, -1); ok {
		t.Fatal("expected out-of-range for negative index")
	}
	if _, ok := upgradeChangeAt(changes, 1); ok {
		t.Fatal("expected out-of-range for index past end")
	}
}

func TestBuildUpgradePlan_ManagedDiffWithPinnedManifestEvidence(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.7.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}

	manifest, err := loadTemplateManifestByVersion("0.7.0")
	if err != nil {
		t.Fatalf("load 0.7.0 manifest: %v", err)
	}
	allowEntry, ok := manifestFileMap(manifest.Files)[commandsAllowRelPath]
	if !ok {
		t.Fatal("0.7.0 manifest missing commands.allow")
	}
	payload, err := parseAllowlistPolicyPayload(allowEntry.PolicyPayload)
	if err != nil {
		t.Fatalf("parse allowlist payload: %v", err)
	}

	currentTemplate, err := templates.Read("commands.allow")
	if err != nil {
		t.Fatalf("read current commands.allow template: %v", err)
	}
	currentComp, err := buildOwnershipComparable(commandsAllowRelPath, currentTemplate)
	if err != nil {
		t.Fatalf("build current allowlist comparable: %v", err)
	}
	if currentComp.AllowHash == payload.UpstreamSetHash {
		t.Skip("current commands.allow matches 0.7.0 manifest allowlist; cannot exercise upstream delta inference")
	}

	localContent := strings.Join(payload.UpstreamSet, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "commands.allow"), []byte(localContent), 0o600); err != nil {
		t.Fatalf("write local commands.allow from manifest set: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "0.7.0"})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	change := findUpgradeChange(plan.TemplateUpdates, commandsAllowRelPath)
	if change == nil {
		t.Fatal("expected commands.allow update in plan")
	}
}

func TestBuildUpgradePlan_ManagedDiffWithoutBaseline(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("# custom allowlist\n"), 0o600); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	allowUpdate := findUpgradeChange(plan.TemplateUpdates, commandsAllowRelPath)
	if allowUpdate == nil {
		t.Fatal("expected commands.allow update in plan")
	}
}

func TestBuildUpgradePlan_RemovalEvidenceFailuresBeforeFiltering(t *testing.T) {
	for _, path := range []string{".agent-layer/templates/docs/local.md", ".agent-layer/local.txt"} {
		for _, operation := range []string{"lstat", "read"} {
			t.Run(path+"/"+operation, func(t *testing.T) {
				root := t.TempDir()
				require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
				seedWorkflowBundleForTest(t, root)
				absPath := filepath.Join(root, filepath.FromSlash(path))
				require.NoError(t, os.WriteFile(absPath, []byte("local\n"), 0o600))
				inst := &installer{root: root, sys: RealSystem{}}
				entries, err := inst.templates().currentTemplateEntries()
				require.NoError(t, err)
				for _, entry := range entries {
					_, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.relPath)))
					require.NoError(t, err, "must have no additions so rename detection cannot cause the failure")
				}
				failure := errors.New(operation + " denied")
				sys := newFaultSystem(RealSystem{})
				if operation == "lstat" {
					sys.lstatErrs[normalizePath(absPath)] = failure
				} else {
					sys.readErrs[normalizePath(absPath)] = failure
				}
				_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: sys})
				require.ErrorIs(t, err, failure)
				if operation == "read" {
					require.EqualError(t, err, failure.Error())
				} else {
					require.Contains(t, err.Error(), "failed to stat")
				}
				// Orphans are checked before the keep list filters them out.
				if strings.HasPrefix(path, ".agent-layer/templates/docs/") {
					require.NoError(t, os.WriteFile(inst.upgradeKeepListPath(), []byte(path+"\n"), 0o600))
					_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: sys})
					require.ErrorIs(t, err, failure)
				}
			})
		}
	}
}

func TestBuildUpgradePlan_TemplateDocsOrphanShortcut(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
	seedWorkflowBundleForTest(t, root)
	path := ".agent-layer/templates/docs/local.md"
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte("local snapshot\n"), 0o600))
	// Legacy template snapshots skip baseline evidence, so a corrupt baseline is not read for them.
	require.NoError(t, os.WriteFile(filepath.Join(root, filepath.FromSlash(baselineStateRelPath)), []byte("{bad-json"), 0o600))
	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)
	require.NotNil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, path))
}
