package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnershipLabelDisplay_EmptyDefaultsToLocalCustomization(t *testing.T) {
	if got := OwnershipLabel("").Display(); got != string(OwnershipLocalCustomization) {
		t.Fatalf("Display() = %q, want %q", got, string(OwnershipLocalCustomization))
	}
}

func TestShouldOverwriteAllManaged_FormatsOwnershipLabels(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("custom config\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var promptPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllFunc: func(paths []string) (bool, error) {
				promptPaths = append(promptPaths, paths...)
				return false, nil
			},
			OverwriteAllMemoryFunc: func([]string) (bool, error) { return false, nil },
			OverwriteFunc:          func(string) (bool, error) { return false, nil },
		},
	}

	if _, err := inst.shouldOverwriteAllManaged(); err != nil {
		t.Fatalf("shouldOverwriteAllManaged: %v", err)
	}
	if len(promptPaths) == 0 {
		t.Fatalf("expected prompt paths")
	}
	if !strings.Contains(promptPaths[0], "local customization") {
		t.Fatalf("expected ownership label in prompt path, got %q", promptPaths[0])
	}
}

func TestShouldOverwriteAllMemory_FormatsOwnershipLabels(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o755); err != nil {
		t.Fatalf("mkdir baseline docs: %v", err)
	}
	content := []byte("previous template content\n")
	docPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
	if err := os.WriteFile(docPath, content, 0o644); err != nil {
		t.Fatalf("write doc: %v", err)
	}
	if err := os.WriteFile(baselinePath, content, 0o644); err != nil {
		t.Fatalf("write baseline doc: %v", err)
	}

	var promptPaths []string
	inst := &installer{
		root:      root,
		overwrite: true,
		sys:       RealSystem{},
		prompter: PromptFuncs{
			OverwriteAllFunc:       func([]string) (bool, error) { return false, nil },
			OverwriteAllMemoryFunc: func(paths []string) (bool, error) { promptPaths = append(promptPaths, paths...); return false, nil },
			OverwriteFunc:          func(string) (bool, error) { return false, nil },
		},
	}

	if _, err := inst.shouldOverwriteAllMemory(); err != nil {
		t.Fatalf("shouldOverwriteAllMemory: %v", err)
	}
	if len(promptPaths) == 0 {
		t.Fatalf("expected prompt paths")
	}
	if !strings.Contains(promptPaths[0], "upstream template delta") {
		t.Fatalf("expected ownership label in prompt path, got %q", promptPaths[0])
	}
}

func TestClassifyOrphanOwnership_DocsAgentLayer_UsesBaseline(t *testing.T) {
	root := t.TempDir()
	localPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	baselinePath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		t.Fatalf("mkdir local: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(baselinePath), 0o755); err != nil {
		t.Fatalf("mkdir baseline: %v", err)
	}
	content := []byte("same as baseline\n")
	if err := os.WriteFile(localPath, content, 0o644); err != nil {
		t.Fatalf("write local: %v", err)
	}
	if err := os.WriteFile(baselinePath, content, 0o644); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership("docs/agent-layer/ROADMAP.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership: %v", err)
	}
	if ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("ownership = %s, want %s", ownership, OwnershipUpstreamTemplateDelta)
	}
}

func TestClassifyOrphanOwnership_TemplatesDocs_AlwaysUpstream(t *testing.T) {
	root := t.TempDir()
	orphanPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, []byte("orphan template snapshot\n"), 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership(".agent-layer/templates/docs/ROADMAP.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership: %v", err)
	}
	if ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("ownership = %s, want %s", ownership, OwnershipUpstreamTemplateDelta)
	}
}

func TestClassifyOrphanOwnership_DocsMissingBaselineAndDefaultFallback(t *testing.T) {
	root := t.TempDir()
	docsPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.MkdirAll(filepath.Dir(docsPath), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(docsPath, []byte("local docs orphan\n"), 0o644); err != nil {
		t.Fatalf("write docs orphan: %v", err)
	}
	defaultPath := filepath.Join(root, ".agent-layer", "slash-commands", "local-orphan.md")
	if err := os.MkdirAll(filepath.Dir(defaultPath), 0o755); err != nil {
		t.Fatalf("mkdir default orphan dir: %v", err)
	}
	if err := os.WriteFile(defaultPath, []byte("local orphan\n"), 0o644); err != nil {
		t.Fatalf("write default orphan: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	ownership, err := inst.classifyOrphanOwnership("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership docs: %v", err)
	}
	if ownership != OwnershipLocalCustomization {
		t.Fatalf("docs missing baseline ownership = %s, want %s", ownership, OwnershipLocalCustomization)
	}

	ownership, err = inst.classifyOrphanOwnership(".agent-layer/slash-commands/local-orphan.md")
	if err != nil {
		t.Fatalf("classifyOrphanOwnership default: %v", err)
	}
	if ownership != OwnershipLocalCustomization {
		t.Fatalf("default fallback ownership = %s, want %s", ownership, OwnershipLocalCustomization)
	}
}
