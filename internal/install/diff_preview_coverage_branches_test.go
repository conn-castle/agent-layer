package install

import (
	"errors"
	"io/fs"
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

func TestBuildSingleDiffPreview_TemplateSectionSplitError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}, diffMaxLines: 20}
	templatePathByRel, err := inst.templates().memoryTemplatePathByRel()
	if err != nil {
		t.Fatalf("memoryTemplatePathByRel: %v", err)
	}
	roadmapTemplatePath := templatePathByRel["docs/agent-layer/ROADMAP.md"]
	if strings.TrimSpace(roadmapTemplatePath) == "" {
		t.Fatal("missing roadmap template path")
	}

	origRead := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == roadmapTemplatePath {
			return []byte("# ROADMAP without marker\n"), nil
		}
		return origRead(name)
	}
	t.Cleanup(func() { templates.ReadFunc = origRead })

	_, err = inst.buildSingleDiffPreview(LabeledPath{
		Path:      "docs/agent-layer/ROADMAP.md",
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
