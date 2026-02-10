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

type callbackErrSystem struct {
	RealSystem
}

func (c callbackErrSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return fn(filepath.Join(root, "bad"), nil, errors.New("callback boom"))
}

func TestBuildUpgradePlan_CurrentTemplateEntriesError(t *testing.T) {
	original := templates.WalkFunc
	templates.WalkFunc = func(string, fs.WalkDirFunc) error {
		return errors.New("walk boom")
	}
	t.Cleanup(func() { templates.WalkFunc = original })

	_, err := BuildUpgradePlan(t.TempDir(), UpgradePlanOptions{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected walk error, got %v", err)
	}
}

func TestBuildUpgradePlan_MatchTemplateError(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o755); err != nil {
		t.Fatalf("mkdir allow dir: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("write allow: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(allowPath)] = errors.New("read boom")

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: sys})
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestBuildUpgradePlan_ClassifyOwnershipDetailError(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o755); err != nil {
		t.Fatalf("mkdir allow dir: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("write allow: %v", err)
	}

	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir baseline dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err == nil {
		t.Fatal("expected classify ownership error due to corrupted baseline state")
	}
}

func TestBuildUpgradePlan_DetectUpgradeRenamesError(t *testing.T) {
	root := t.TempDir()
	orphanPath := filepath.Join(root, ".agent-layer", "slash-commands", "orphan-coverage-test.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan\n"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == "commands.allow" {
			return nil, errors.New("template boom")
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), "template boom") {
		t.Fatalf("expected template boom error, got %v", err)
	}
}

func TestBuildUpgradePlan_PinVersionDiffError(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(pinPath)] = errors.New("read boom")

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: sys})
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected pin read error, got %v", err)
	}
}

func TestTemplateOrphans_ClassifyOrphanOwnershipDetailError(t *testing.T) {
	root := t.TempDir()
	orphanPath := filepath.Join(root, "docs", "agent-layer", "ORPHAN.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan\n"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(orphanPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: sys}
	templateEntries, err := inst.currentTemplateEntries()
	if err != nil {
		t.Fatalf("currentTemplateEntries: %v", err)
	}

	if _, err := inst.templateOrphans(templateEntries); err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected orphan classify read error, got %v", err)
	}
}

func TestTemplateOrphans_SortsOrphans(t *testing.T) {
	root := t.TempDir()
	aPath := filepath.Join(root, "docs", "agent-layer", "A.md")
	zPath := filepath.Join(root, "docs", "agent-layer", "Z.md")
	if err := os.MkdirAll(filepath.Dir(aPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.WriteFile(aPath, []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(zPath, []byte("z\n"), 0o644); err != nil {
		t.Fatalf("write z: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	templateEntries, err := inst.currentTemplateEntries()
	if err != nil {
		t.Fatalf("currentTemplateEntries: %v", err)
	}
	orphans, err := inst.templateOrphans(templateEntries)
	if err != nil {
		t.Fatalf("templateOrphans: %v", err)
	}
	if len(orphans) != 2 {
		t.Fatalf("expected 2 orphans, got %d", len(orphans))
	}
	if orphans[0].path != "docs/agent-layer/A.md" || orphans[1].path != "docs/agent-layer/Z.md" {
		t.Fatalf("unexpected orphan sort order: %#v", orphans)
	}
}

func TestWalkTemplateOrphans_MissingRootReturnsNil(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	templatePaths := map[string]struct{}{}
	orphanSet := map[string]struct{}{}
	missingRoot := filepath.Join(root, "does-not-exist")
	if err := inst.walkTemplateOrphans(missingRoot, templatePaths, orphanSet); err != nil {
		t.Fatalf("expected nil for missing root, got %v", err)
	}
}

func TestWalkTemplateOrphans_CallbackErrorPropagates(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "exists")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	inst := &installer{root: root, sys: callbackErrSystem{RealSystem{}}}
	templatePaths := map[string]struct{}{}
	orphanSet := map[string]struct{}{}
	if err := inst.walkTemplateOrphans(existing, templatePaths, orphanSet); err == nil || !strings.Contains(err.Error(), "callback boom") {
		t.Fatalf("expected callback boom error, got %v", err)
	}
}

func TestDetectUpgradeRenames_SortsRenames(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	configBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	allowBytes, err := templates.Read("commands.allow")
	if err != nil {
		t.Fatalf("read commands.allow template: %v", err)
	}

	orphanZ := filepath.Join(root, ".agent-layer", "slash-commands", "z.md")
	orphanA := filepath.Join(root, ".agent-layer", "slash-commands", "a.md")
	if err := os.MkdirAll(filepath.Dir(orphanZ), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanZ, configBytes, 0o644); err != nil {
		t.Fatalf("write orphan z: %v", err)
	}
	if err := os.WriteFile(orphanA, allowBytes, 0o644); err != nil {
		t.Fatalf("write orphan a: %v", err)
	}

	additions := []upgradeChangeWithTemplate{
		{path: ".agent-layer/add-config.toml", templatePath: "config.toml"},
		{path: ".agent-layer/add-allow.allow", templatePath: "commands.allow"},
	}
	orphans := []upgradeChangeWithTemplate{
		{path: ".agent-layer/slash-commands/z.md"},
		{path: ".agent-layer/slash-commands/a.md"},
	}

	renames, remainingAdditions, remainingOrphans, err := detectUpgradeRenames(inst, additions, orphans)
	if err != nil {
		t.Fatalf("detectUpgradeRenames: %v", err)
	}
	if len(remainingAdditions) != 0 || len(remainingOrphans) != 0 {
		t.Fatalf("expected renames to consume all candidates, got %d additions / %d orphans", len(remainingAdditions), len(remainingOrphans))
	}
	if len(renames) != 2 {
		t.Fatalf("expected 2 renames, got %d", len(renames))
	}
	if renames[0].From != ".agent-layer/slash-commands/a.md" {
		t.Fatalf("expected sorted renames, got %#v", renames)
	}
}

func TestSectionAwareTemplateMatch_EdgeCases(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	absPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}

	t.Run("local read error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(absPath)] = errors.New("read boom")
		inst.sys = sys
		_, err := inst.sectionAwareTemplateMatch("docs/agent-layer/ISSUES.md", absPath, "docs/agent-layer/ISSUES.md")
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("template read error", func(t *testing.T) {
		inst.sys = RealSystem{}
		if err := os.WriteFile(absPath, []byte("# ok\n\n<!-- ENTRIES START -->\n"), 0o644); err != nil {
			t.Fatalf("write local: %v", err)
		}
		_, err := inst.sectionAwareTemplateMatch("docs/agent-layer/ISSUES.md", absPath, "missing-template.md")
		if err == nil {
			t.Fatal("expected template read error")
		}
	})

	t.Run("local parse error falls through", func(t *testing.T) {
		inst.sys = RealSystem{}
		if err := os.WriteFile(absPath, []byte("# missing marker\n"), 0o644); err != nil {
			t.Fatalf("write local: %v", err)
		}
		match, err := inst.sectionAwareTemplateMatch("docs/agent-layer/ISSUES.md", absPath, "docs/agent-layer/ISSUES.md")
		if err != nil {
			t.Fatalf("sectionAwareTemplateMatch: %v", err)
		}
		if match {
			t.Fatal("expected no match on local parse error")
		}
	})

	t.Run("target parse error falls through", func(t *testing.T) {
		inst.sys = RealSystem{}
		if err := os.WriteFile(absPath, []byte("# ok\n\n<!-- ENTRIES START -->\n"), 0o644); err != nil {
			t.Fatalf("write local: %v", err)
		}
		match, err := inst.sectionAwareTemplateMatch("docs/agent-layer/ISSUES.md", absPath, "commands.allow")
		if err != nil {
			t.Fatalf("sectionAwareTemplateMatch: %v", err)
		}
		if match {
			t.Fatal("expected no match on target parse error")
		}
	})
}
