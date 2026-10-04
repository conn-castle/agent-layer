package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

type renamedReviewFixture struct {
	source   string
	target   string
	template string
}

var renamedMemoryFixture = renamedReviewFixture{
	source:   ".agent-layer/instructions/02_memory.md",
	target:   ".agent-layer/instructions/01_memory.md",
	template: "instructions/01_memory.md",
}

var renamedSkillFixture = renamedReviewFixture{
	source:   ".agent-layer/skills/agent-dispatch/SKILL.md",
	target:   ".agent-layer/skills/dispatch-agent/SKILL.md",
	template: "skills-catalog/dispatch-agent/SKILL.md",
}

const renamedReviewCustomLine = "- MY CUSTOM RENAME RULE"

func seedRenamedReview(t *testing.T, fixture renamedReviewFixture, customized bool) string {
	t.Helper()
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.15.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	data, err := templates.Read(fixture.template)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if customized {
		data = append(data, []byte("\n"+renamedReviewCustomLine+"\n")...)
	}
	path := filepath.Join(root, filepath.FromSlash(fixture.source))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(fixture.target))); !os.IsNotExist(err) {
		t.Fatalf("destination must be absent before migration: %v", err)
	}
	return root
}

func assertRenamedReviewPreview(t *testing.T, previews map[string]DiffPreview, fixture renamedReviewFixture) {
	t.Helper()
	preview, ok := previews[fixture.target]
	if !ok {
		t.Fatalf("missing renamed destination preview %s", fixture.target)
	}
	if !strings.Contains(preview.UnifiedDiff, "-"+renamedReviewCustomLine) {
		t.Fatalf("destination diff must remove custom line, got %q", preview.UnifiedDiff)
	}
}

func TestUpgrade_RenamedCustomizedFileReview(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fixture renamedReviewFixture
		unified bool
	}{
		{"UnifiedFile", renamedMemoryFixture, true},
		{"NonUnifiedFile", renamedMemoryFixture, false},
		{"Directory", renamedSkillFixture, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := seedRenamedReview(t, tc.fixture, true)
			if err := os.WriteFile(filepath.Join(root, ".agent-layer", commandsAllowName), []byte("echo custom\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var captured []DiffPreview
			prompter := autoApprovePrompter()
			if tc.unified {
				prompter.OverwriteAllUnifiedPreviewFunc = func(managed, memory []DiffPreview) (bool, bool, error) {
					captured = managed
					return true, false, nil
				}
			} else {
				prompter.OverwriteAllPreviewFunc = func(previews []DiffPreview) (bool, error) {
					captured = previews
					return true, nil
				}
			}
			if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.23.1", Overwrite: true, Prompter: prompter}); err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			assertRenamedReviewPreview(t, indexDiffPreviews(captured), tc.fixture)
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tc.fixture.target))) // #nosec G304 -- path is constructed from test-controlled inputs.
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), renamedReviewCustomLine) {
				t.Fatal("approved overwrite did not replace custom line")
			}
		})
	}
}

func TestBuildUpgradePlan_RenamedCustomizedFileReview(t *testing.T) {
	for name, fixture := range map[string]renamedReviewFixture{"File": renamedMemoryFixture, "Directory": renamedSkillFixture} {
		t.Run(name, func(t *testing.T) {
			root := seedRenamedReview(t, fixture, true)
			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.23.1", System: RealSystem{}})
			if err != nil {
				t.Fatal(err)
			}
			updates := append(append([]UpgradeChange{}, plan.TemplateUpdates...), plan.SectionAwareUpdates...)
			change := findUpgradeChange(updates, fixture.target)
			if change == nil || change.Ownership == "" {
				t.Fatalf("missing labeled destination update %s: %#v", fixture.target, updates)
			}
			if findUpgradeChange(plan.TemplateAdditions, fixture.target) != nil {
				t.Fatal("renamed destination must not be an addition")
			}
			for _, changes := range [][]UpgradeChange{plan.TemplateAdditions, updates, plan.TemplateRemovalsOrOrphans} {
				if findUpgradeChange(changes, fixture.source) != nil {
					t.Fatal("rename source must stay hidden")
				}
			}
			for _, rename := range plan.TemplateRenames {
				if rename.From == fixture.source || rename.To == fixture.source {
					t.Fatal("manifest rename must not feed hash rename inference")
				}
			}
		})
	}
}

func TestBuildUpgradePlan_RenamedSymlinkedSourceReview(t *testing.T) {
	root := seedRenamedReview(t, renamedMemoryFixture, true)
	source := filepath.Join(root, filepath.FromSlash(renamedMemoryFixture.source))
	external := filepath.Join(t.TempDir(), "memory.md")
	if err := os.Rename(source, external); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, source); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.23.1", System: RealSystem{}})
	if err != nil {
		t.Fatal(err)
	}
	if findUpgradeChange(append(plan.TemplateUpdates, plan.SectionAwareUpdates...), renamedMemoryFixture.target) == nil {
		t.Fatal("symlinked rename source must be listed like apply lists it")
	}
}

func TestBuildUpgradePlanDiffPreviews_RenamedCustomizedFileReview(t *testing.T) {
	for name, fixture := range map[string]renamedReviewFixture{"File": renamedMemoryFixture, "Directory": renamedSkillFixture} {
		t.Run(name, func(t *testing.T) {
			root := seedRenamedReview(t, fixture, true)
			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.23.1", System: RealSystem{}})
			if err != nil {
				t.Fatal(err)
			}
			previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}, MaxDiffLines: 1000})
			if err != nil {
				t.Fatal(err)
			}
			assertRenamedReviewPreview(t, previews, fixture)
		})
	}
}

// Regression guard: selective prompts already preserve declined renamed files.
func TestUpgrade_RenamedReviewSelectiveDeclineGuard(t *testing.T) {
	root := seedRenamedReview(t, renamedMemoryFixture, true)
	commandsPath := filepath.Join(root, ".agent-layer", commandsAllowName)
	if err := os.WriteFile(commandsPath, []byte("echo custom\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prompted := make(map[string]bool)
	prompter := autoApprovePrompter()
	prompter.OverwriteAllPreviewFunc = func([]DiffPreview) (bool, error) { return false, nil }
	prompter.OverwritePreviewFunc = func(preview DiffPreview) (bool, error) {
		prompted[preview.Path] = true
		return preview.Path == ".agent-layer/commands.allow", nil
	}
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.23.1", Overwrite: true, Prompter: prompter}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(renamedMemoryFixture.target))) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil || !strings.Contains(string(data), renamedReviewCustomLine) {
		t.Fatalf("declined destination lost custom line: %s, %v", data, err)
	}
	commands, err := os.ReadFile(commandsPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	want, templateErr := templates.Read(commandsAllowName)
	if err != nil || templateErr != nil || string(commands) != string(want) {
		t.Fatalf("commands.allow not updated: %v, %v", err, templateErr)
	}
	if !prompted[renamedMemoryFixture.target] || !prompted[".agent-layer/commands.allow"] {
		t.Fatalf("missing selective prompts: %#v", prompted)
	}
}

// Regression guard: moving template-equal content requires no template update.
func TestBuildUpgradePlan_RenamedReviewUnchangedGuard(t *testing.T) {
	for name, fixture := range map[string]renamedReviewFixture{"File": renamedMemoryFixture, "Directory": renamedSkillFixture} {
		t.Run(name, func(t *testing.T) {
			root := seedRenamedReview(t, fixture, false)
			plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.23.1", System: RealSystem{}})
			if err != nil {
				t.Fatal(err)
			}
			for _, changes := range [][]UpgradeChange{plan.TemplateAdditions, plan.TemplateUpdates, plan.SectionAwareUpdates} {
				if findUpgradeChange(changes, fixture.target) != nil {
					t.Fatal("template-equal destination must not be listed")
				}
			}
		})
	}
}

// Regression guard: non-rename coverage wins when it overlaps a rename.
func TestBuildUpgradePlan_RenamedReviewNonRenameCoverageGuard(t *testing.T) {
	root := seedRenamedReview(t, renamedSkillFixture, true)
	withMigrationManifestOverride(t, "0.16.0", `{
  "schema_version": 1,
  "target_version": "0.16.0",
  "min_prior_version": "0.15.0",
  "operations": [
    {"id": "a-rename", "kind": "rename_file", "rationale": "Move skill", "source_agnostic": true,
     "from": ".agent-layer/skills/agent-dispatch", "to": ".agent-layer/skills/dispatch-agent"},
    {"id": "b-format", "kind": "migrate_skills_format", "rationale": "Update format", "source_agnostic": true,
     "path": ".agent-layer/skills"}
  ]
}`)
	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.16.0", System: RealSystem{}})
	if err != nil {
		t.Fatal(err)
	}
	if findUpgradeChange(plan.TemplateUpdates, renamedSkillFixture.target) != nil {
		t.Fatal("non-rename coverage must keep destination hidden")
	}
	inst := &installer{root: root, pinVersion: "0.16.0", sys: RealSystem{}}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatal(err)
	}
	if previews := inst.filterMigrationCoveredDiffs([]LabeledPath{{Path: renamedSkillFixture.target}}); len(previews) != 0 {
		t.Fatal("non-rename coverage must also hide apply-time destination review")
	}
}

func TestBuildUpgradePlan_RenamedReviewChains(t *testing.T) {
	for _, kind := range []upgradeMigrationOperationKind{upgradeMigrationKindRenameFile, upgradeMigrationKindRenameGeneratedArtifact} {
		for _, roundTrip := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/roundTrip=%t", kind, roundTrip), func(t *testing.T) {
				root := seedRenamedReview(t, renamedMemoryFixture, true)
				from := renamedMemoryFixture.source
				if roundTrip {
					if err := os.Rename(filepath.Join(root, filepath.FromSlash(from)), filepath.Join(root, filepath.FromSlash(renamedMemoryFixture.target))); err != nil {
						t.Fatal(err)
					}
					from = renamedMemoryFixture.target
				}
				withMigrationManifestOverride(t, "0.16.0", fmt.Sprintf(`{
  "schema_version": 1,
  "target_version": "0.16.0",
  "min_prior_version": "0.15.0",
  "operations": [
    {"id": "a-first", "kind": %q, "rationale": "First move", "source_agnostic": true,
     "from": %q, "to": ".agent-layer/instructions/intermediate.md"},
    {"id": "b-second", "kind": %q, "rationale": "Second move", "source_agnostic": true,
     "from": ".agent-layer/instructions/intermediate.md", "to": %q}
  ]
}`, kind, from, kind, renamedMemoryFixture.target))
				plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.16.0", System: RealSystem{}})
				if err != nil {
					t.Fatal(err)
				}
				count := 0
				for _, change := range plan.TemplateUpdates {
					if change.Path == renamedMemoryFixture.target {
						count++
					}
				}
				if count != 1 || findUpgradeChange(plan.TemplateAdditions, renamedMemoryFixture.target) != nil {
					t.Fatalf("chained destination must appear once as update: %#v", plan)
				}
				// Preview reconstruction must not require a shipped target manifest.
				plan.PinVersionChange.Target = "1.0.0"
				previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}, MaxDiffLines: 1000})
				if err != nil {
					t.Fatal(err)
				}
				assertRenamedReviewPreview(t, previews, renamedMemoryFixture)
			})
		}
	}
}

func TestMovedFileUpdates_RenamedReviewErrorsAndNoRename(t *testing.T) {
	root := seedRenamedReview(t, renamedMemoryFixture, true)
	inst := &installer{root: root, pinVersion: "0.23.1", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, filepath.FromSlash(renamedMemoryFixture.source))
	for _, operation := range []string{"walk", "stat", "read"} {
		t.Run(operation, func(t *testing.T) {
			failure := errors.New("origin access denied")
			sys := newFaultSystem(RealSystem{})
			switch operation {
			case "walk":
				sys.walkErrs[filepath.Join(root, ".agent-layer")] = failure
			case "stat":
				sys.statErrs[origin] = failure
			case "read":
				sys.readErrs[origin] = failure
			}
			inst.sys = sys
			_, _, err := inst.movedFileUpdates(plan, nil, nil)
			if !errors.Is(err, failure) {
				t.Fatalf("origin error not propagated: %v", err)
			}
		})
	}
	sys := newFaultSystem(RealSystem{})
	sys.walkErrs[filepath.Join(root, ".agent-layer")] = errors.New("unexpected walk")
	inst.sys = sys
	if _, _, err := inst.movedFileUpdates(migrationPlan{}, nil, nil); err != nil {
		t.Fatalf("plan without renames must not walk: %v", err)
	}
	inst.sys = RealSystem{}
	previewPlan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.23.1", System: RealSystem{}})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("preview origin unreadable")
	sys = newFaultSystem(RealSystem{})
	sys.readErrs[origin] = failure
	if _, err := BuildUpgradePlanDiffPreviews(root, previewPlan, UpgradePlanDiffPreviewOptions{System: sys}); !errors.Is(err, failure) {
		t.Fatalf("preview origin error not propagated: %v", err)
	}
}
