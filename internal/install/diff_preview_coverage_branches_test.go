package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildManagedAndMemoryDiffPreviews_TemplateMappingErrors(t *testing.T) {
	origWalk := templates.WalkFunc
	templates.WalkFunc = func(string, fs.WalkDirFunc) error {
		return errors.New("walk templates boom")
	}
	t.Cleanup(func() { templates.WalkFunc = origWalk })

	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	if _, _, err := inst.buildManagedDiffPreviews([]LabeledPath{}); err == nil || !strings.Contains(err.Error(), "walk templates boom") {
		t.Fatalf("expected managed template mapping error, got %v", err)
	}
	if _, _, err := inst.buildMemoryDiffPreviews([]LabeledPath{}); err == nil || !strings.Contains(err.Error(), "walk templates boom") {
		t.Fatalf("expected memory template mapping error, got %v", err)
	}
}

func TestBuildSingleDiffPreview_GitignoreBlockUsesMergedTarget(t *testing.T) {
	root := t.TempDir()
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(blockPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	customized := customizeGitignoreTracking(string(templateBytes))
	if err := os.WriteFile(blockPath, []byte(customized), 0o600); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}, diffMaxLines: 40}
	templatePathByRel := map[string]string{
		".agent-layer/gitignore.block": templateGitignoreBlock,
	}

	preview, err := inst.buildSingleDiffPreview(LabeledPath{
		Path:      ".agent-layer/gitignore.block",
		Ownership: OwnershipLocalCustomization,
	}, templatePathByRel)
	if err != nil {
		t.Fatalf("buildSingleDiffPreview: %v", err)
	}
	if preview.UnifiedDiff != "" || preview.LinesAdded != 0 || preview.LinesRemoved != 0 {
		t.Fatalf("expected no preview when merged write is a no-op, got %+v", preview)
	}

	originalRead := templates.ReadFunc
	templates.ReadFunc = func(templatePath string) ([]byte, error) {
		data, readErr := originalRead(templatePath)
		if readErr != nil || templatePath != templateGitignoreBlock {
			return data, readErr
		}
		return append(data, []byte("\n/new-generated-output/\n")...), nil
	}
	t.Cleanup(func() { templates.ReadFunc = originalRead })

	preview, err = inst.buildSingleDiffPreview(LabeledPath{
		Path:      ".agent-layer/gitignore.block",
		Ownership: OwnershipLocalCustomization,
	}, templatePathByRel)
	if err != nil {
		t.Fatalf("buildSingleDiffPreview with template update: %v", err)
	}
	if !strings.Contains(preview.UnifiedDiff, "/new-generated-output/") {
		t.Fatalf("expected template update in preview, got %q", preview.UnifiedDiff)
	}
	if strings.Contains(preview.UnifiedDiff, "-"+AgentLayerGitignorePattern) ||
		strings.Contains(preview.UnifiedDiff, "+# "+AgentLayerGitignorePattern) ||
		strings.Contains(preview.UnifiedDiff, "-# "+DocsAgentLayerGitignorePattern) ||
		strings.Contains(preview.UnifiedDiff, "+"+DocsAgentLayerGitignorePattern) {
		t.Fatalf("preview treated preserved tracking settings as a change:\n%s", preview.UnifiedDiff)
	}
}

func TestBuildSingleDiffPreview_TemplateSectionSplitError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	seedWorkflowBundleForTest(t, root)

	inst := &installer{root: root, sys: RealSystem{}, diffMaxLines: 20}
	templatePathByRel, err := inst.templates().memoryTemplatePathByRel()
	if err != nil {
		t.Fatalf("memoryTemplatePathByRel: %v", err)
	}
	backlogTemplatePath := templatePathByRel["docs/agent-layer/BACKLOG.md"]
	if strings.TrimSpace(backlogTemplatePath) == "" {
		t.Fatal("missing backlog template path")
	}

	origRead := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == backlogTemplatePath {
			return []byte("# BACKLOG without marker\n"), nil
		}
		return origRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = origRead })

	_, err = inst.buildSingleDiffPreview(LabeledPath{
		Path:      "docs/agent-layer/BACKLOG.md",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, templatePathByRel)
	if err == nil || !strings.Contains(err.Error(), "missing in") {
		t.Fatalf("expected template section marker split error, got %v", err)
	}
}

func TestDiffPreviewHelpers_AdditionalBranches(t *testing.T) {
	if got := collapseEquivalentDiffRun(nil); got != nil {
		t.Fatalf("collapseEquivalentDiffRun(nil) = %#v, want nil", got)
	}

	if got := pruneEmptyUnifiedDiffHunks(nil); got != nil {
		t.Fatalf("pruneEmptyUnifiedDiffHunks(nil) = %#v, want nil", got)
	}

	noHunks := []string{"--- a.txt", "+++ b.txt", "+changed line"}
	got := pruneEmptyUnifiedDiffHunks(noHunks)
	if len(got) != len(noHunks) {
		t.Fatalf("expected no-hunk input to be returned unchanged, got %#v", got)
	}
}
