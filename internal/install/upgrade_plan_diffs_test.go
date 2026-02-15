package install

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
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

	pinPreview, ok := previews[pinVersionRelPath]
	if !ok {
		t.Fatalf("expected preview for %s when pin changes", pinVersionRelPath)
	}
	if strings.TrimSpace(pinPreview.UnifiedDiff) == "" {
		t.Fatalf("expected non-empty pin diff preview")
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

func mapsKeys(m map[string]DiffPreview) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
