package install

import (
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
		TargetPinVersion: "1.1.0",
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

	pinPreview, ok := previews[".agent-layer/al.version"]
	if !ok {
		t.Fatalf("expected preview for .agent-layer/al.version when pin changes")
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

func mapsKeys(m map[string]DiffPreview) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
