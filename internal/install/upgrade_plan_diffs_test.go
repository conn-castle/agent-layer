package install

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildUpgradePlanDiffPreviews_GeneratesChangedFileAndPinDiffs(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("git status\n"), 0o644); err != nil {
		t.Fatalf("write managed customization: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{
		TargetPinVersion: "0.7.0",
		System:           RealSystem{},
	})
	if err != nil {
		t.Fatalf("BuildUpgradePlan: %v", err)
	}

	previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err != nil {
		t.Fatalf("BuildUpgradePlanDiffPreviews: %v", err)
	}

	allowPreview, ok := previews[".agent-layer/commands.allow"]
	if !ok {
		t.Fatalf("expected preview for .agent-layer/commands.allow, got keys: %v", mapsKeys(previews))
	}
	if strings.TrimSpace(allowPreview.UnifiedDiff) == "" {
		t.Fatalf("expected non-empty diff preview for commands.allow")
	}

	if _, ok := previews[pinVersionRelPath]; ok {
		t.Fatalf("did not expect preview for %s even when pin changes", pinVersionRelPath)
	}
}

func TestBuildUpgradePlanDiffPreviews_RequiresInputs(t *testing.T) {
	_, err := BuildUpgradePlanDiffPreviews("", UpgradePlan{}, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil || err.Error() != messages.InstallRootRequired {
		t.Fatalf("expected root required error, got: %v", err)
	}

	_, err = BuildUpgradePlanDiffPreviews("/tmp", UpgradePlan{}, UpgradePlanDiffPreviewOptions{
		MaxDiffLines: 20,
	})
	if err == nil || err.Error() != messages.InstallSystemRequired {
		t.Fatalf("expected system required error, got: %v", err)
	}
}

func TestBuildPlanChangeDiffPreview_Modes(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("git status\n"), 0o644); err != nil {
		t.Fatalf("write managed customization: %v", err)
	}
	templatePathByRel, err := inst.allTemplatePathByRel()
	if err != nil {
		t.Fatalf("allTemplatePathByRel: %v", err)
	}

	tests := []struct {
		name string
		mode planDiffMode
		path string
	}{
		{name: "update", mode: planDiffModeUpdate, path: ".agent-layer/commands.allow"},
		{name: "addition", mode: planDiffModeAddition, path: ".agent-layer/commands.allow"},
		{name: "removal", mode: planDiffModeRemoval, path: ".agent-layer/commands.allow"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preview, err := inst.buildPlanChangeDiffPreview(UpgradeChange{
				Path:      tc.path,
				Ownership: OwnershipUpstreamTemplateDelta,
			}, tc.mode, templatePathByRel)
			if err != nil {
				t.Fatalf("buildPlanChangeDiffPreview(%s): %v", tc.mode, err)
			}
			if preview.Path != tc.path {
				t.Fatalf("preview path = %q, want %q", preview.Path, tc.path)
			}
			if strings.TrimSpace(preview.UnifiedDiff) == "" {
				t.Fatalf("expected non-empty diff for mode %s", tc.mode)
			}
		})
	}
}

func TestBuildPlanChangeDiffPreview_Errors(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	templatePathByRel, err := inst.allTemplatePathByRel()
	if err != nil {
		t.Fatalf("allTemplatePathByRel: %v", err)
	}

	_, err = inst.buildPlanChangeDiffPreview(UpgradeChange{
		Path:      "missing/path.md",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, planDiffModeAddition, templatePathByRel)
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected missing template path mapping error, got: %v", err)
	}

	_, err = inst.buildPlanChangeDiffPreview(UpgradeChange{
		Path:      ".agent-layer/commands.allow",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, planDiffMode("unknown"), templatePathByRel)
	if err == nil || !strings.Contains(err.Error(), "unknown plan diff mode") {
		t.Fatalf("expected unknown mode error, got: %v", err)
	}
}

func TestAllTemplatePathByRel_MergesManagedAndMemory(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root: root,
		sys:  RealSystem{},
	}
	paths, err := inst.allTemplatePathByRel()
	if err != nil {
		t.Fatalf("allTemplatePathByRel: %v", err)
	}

	required := []string{
		".agent-layer/commands.allow",
		"docs/agent-layer/ROADMAP.md",
	}
	for _, path := range required {
		if strings.TrimSpace(paths[path]) == "" {
			t.Fatalf("expected template path mapping for %s", path)
		}
	}
}

func TestBuildUpgradePlanDiffPreviews_PropagatesChangeError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	plan := UpgradePlan{
		TemplateAdditions: []UpgradeChange{
			{
				Path:      fmt.Sprintf("missing-%d.md", 1),
				Ownership: OwnershipUpstreamTemplateDelta,
			},
		},
	}
	_, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected missing template path mapping error, got: %v", err)
	}
}

func TestBuildUpgradePlanDiffPreviews_CoversAllCollectionsWithoutPinDiff(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	managedPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(managedPath, []byte("git status\n"), 0o644); err != nil {
		t.Fatalf("write managed customization: %v", err)
	}

	plan := UpgradePlan{
		TemplateAdditions: []UpgradeChange{
			{
				Path:      ".agent-layer/instructions/00_base.md",
				Ownership: OwnershipUpstreamTemplateDelta,
			},
		},
		TemplateUpdates: []UpgradeChange{
			{
				Path:      ".agent-layer/commands.allow",
				Ownership: OwnershipLocalCustomization,
			},
		},
		SectionAwareUpdates: []UpgradeChange{
			{
				Path:      "docs/agent-layer/ISSUES.md",
				Ownership: OwnershipLocalCustomization,
			},
		},
		TemplateRemovalsOrOrphans: []UpgradeChange{
			{
				Path:      ".agent-layer/templates/docs/ROADMAP.md",
				Ownership: OwnershipLocalCustomization,
			},
		},
		PinVersionChange: UpgradePinVersionDiff{
			Action: UpgradePinActionNone,
			Target: "1.0.0",
		},
	}

	previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err != nil {
		t.Fatalf("BuildUpgradePlanDiffPreviews: %v", err)
	}

	required := []string{
		".agent-layer/instructions/00_base.md",
		".agent-layer/commands.allow",
		"docs/agent-layer/ISSUES.md",
		".agent-layer/templates/docs/ROADMAP.md",
	}
	for _, path := range required {
		if _, ok := previews[path]; !ok {
			t.Fatalf("expected preview for %q; keys=%v", path, mapsKeys(previews))
		}
	}
	if _, ok := previews[pinVersionRelPath]; ok {
		t.Fatalf("did not expect pin preview when action is %q", UpgradePinActionNone)
	}
}

func TestBuildPlanChangeDiffPreview_AdditionTemplateReadError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	templatePathByRel, err := inst.allTemplatePathByRel()
	if err != nil {
		t.Fatalf("allTemplatePathByRel: %v", err)
	}
	templatePath := templatePathByRel[".agent-layer/commands.allow"]
	originalRead := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == templatePath {
			return nil, fmt.Errorf("forced template read failure")
		}
		return originalRead(path)
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	_, err = inst.buildPlanChangeDiffPreview(UpgradeChange{
		Path:      ".agent-layer/commands.allow",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, planDiffModeAddition, templatePathByRel)
	if err == nil || !strings.Contains(err.Error(), "forced template read failure") {
		t.Fatalf("expected forced template read failure, got: %v", err)
	}
}

func TestBuildPlanChangeDiffPreview_RemovalReadError(t *testing.T) {
	root := t.TempDir()
	dirAsFile := filepath.Join(root, ".agent-layer", "dir-as-file")
	if err := os.MkdirAll(dirAsFile, 0o755); err != nil {
		t.Fatalf("mkdir dir-as-file: %v", err)
	}

	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	_, err := inst.buildPlanChangeDiffPreview(UpgradeChange{
		Path:      ".agent-layer/dir-as-file",
		Ownership: OwnershipLocalCustomization,
	}, planDiffModeRemoval, nil)
	if err == nil {
		t.Fatalf("expected removal read error for directory path")
	}
}

func TestAllTemplatePathByRel_ErrorPaths(t *testing.T) {
	t.Run("managed mapping error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{
			root: root,
			sys:  RealSystem{},
		}
		originalWalk := templates.WalkFunc
		templates.WalkFunc = func(string, fs.WalkDirFunc) error {
			return fmt.Errorf("managed walk failure")
		}
		t.Cleanup(func() { templates.WalkFunc = originalWalk })

		if _, err := inst.allTemplatePathByRel(); err == nil || !strings.Contains(err.Error(), "managed walk failure") {
			t.Fatalf("expected managed walk failure, got: %v", err)
		}
	})

	t.Run("memory mapping error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{
			root: root,
			sys:  RealSystem{},
		}
		originalWalk := templates.WalkFunc
		callCount := 0
		templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
			callCount++
			if callCount == 4 {
				return fmt.Errorf("memory walk failure")
			}
			return originalWalk(root, fn)
		}
		t.Cleanup(func() { templates.WalkFunc = originalWalk })

		if _, err := inst.allTemplatePathByRel(); err == nil || !strings.Contains(err.Error(), "memory walk failure") {
			t.Fatalf("expected memory walk failure, got: %v", err)
		}
	})
}

func TestBuildUpgradePlanDiffPreviews_PropagatesTemplateMapError(t *testing.T) {
	root := t.TempDir()
	originalWalk := templates.WalkFunc
	templates.WalkFunc = func(string, fs.WalkDirFunc) error {
		return fmt.Errorf("template mapping failure")
	}
	t.Cleanup(func() { templates.WalkFunc = originalWalk })

	_, err := BuildUpgradePlanDiffPreviews(root, UpgradePlan{}, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "template mapping failure") {
		t.Fatalf("expected template mapping failure, got: %v", err)
	}
}

func TestBuildUpgradePlanDiffPreviews_PropagatesTemplateUpdateError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	plan := UpgradePlan{
		TemplateUpdates: []UpgradeChange{
			{
				Path:      "missing/update/path.md",
				Ownership: OwnershipUpstreamTemplateDelta,
			},
		},
	}
	_, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected update preview error, got: %v", err)
	}
}

func TestBuildUpgradePlanDiffPreviews_PropagatesSectionAwareUpdateError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	plan := UpgradePlan{
		SectionAwareUpdates: []UpgradeChange{
			{
				Path:      "missing/section-aware/path.md",
				Ownership: OwnershipUpstreamTemplateDelta,
			},
		},
	}
	_, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected section-aware preview error, got: %v", err)
	}
}

func TestBuildUpgradePlanDiffPreviews_PropagatesTemplateRemovalError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "dir-as-file"), 0o755); err != nil {
		t.Fatalf("mkdir dir-as-file: %v", err)
	}
	plan := UpgradePlan{
		TemplateRemovalsOrOrphans: []UpgradeChange{
			{
				Path:      ".agent-layer/dir-as-file",
				Ownership: OwnershipLocalCustomization,
			},
		},
	}
	_, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{
		System:       RealSystem{},
		MaxDiffLines: 20,
	})
	if err == nil {
		t.Fatalf("expected removal preview read error")
	}
}

func mapsKeys(m map[string]DiffPreview) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
