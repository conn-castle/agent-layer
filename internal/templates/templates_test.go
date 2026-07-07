package templates

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTemplate(t *testing.T) {
	data, err := Read("config.toml")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected template content")
	}
}

func TestReadTemplateMissing(t *testing.T) {
	_, err := Read("missing.txt")
	if err == nil {
		t.Fatalf("expected error for missing template")
	}
}

func TestReadLauncherTemplate(t *testing.T) {
	data, err := Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if !strings.Contains(string(data), "al vscode --no-sync") {
		t.Fatalf("expected launcher command in template")
	}
}

func TestReadManifestTemplate(t *testing.T) {
	data, err := Read("manifests/0.7.0.json")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected manifest content")
	}
}

func TestReadMigrationManifestTemplate(t *testing.T) {
	data, err := Read("migrations/0.7.0.json")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected migration manifest content")
	}
}

func TestWalkTemplates(t *testing.T) {
	var seen bool
	err := Walk("instructions", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			seen = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	if !seen {
		t.Fatalf("expected to see at least one instruction template")
	}
}

func TestSkillTemplatesAllowResourceFiles(t *testing.T) {
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Base(path) == "SKILL.md" {
			return nil
		}
		if _, err := Read(path); err != nil {
			t.Fatalf("expected embedded skill resource %s to be readable: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}

// TestToolInstructionsKeepSkillOwnedRoutingOutOfBaseInstructions enforces the
// architectural rule that per-tool routing for skill-owned CLIs (Context7,
// Tavily, Playwright) lives in the matching skill body, not in the base
// instruction template. The absence assertions catch a copy-paste regression
// where skill-specific guidance leaks back into instructions/03_tools.md.
// Wording assertions are intentionally avoided per docs/agent-layer/CONTEXT.md
// "Test policy".
func TestToolInstructionsKeepSkillOwnedRoutingOutOfBaseInstructions(t *testing.T) {
	data, err := Read("instructions/03_tools.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Documentation-first retrieval order",
		"Context7 (library documentation)",
		"npx ctx7",
		"`tvly`:",
		"`playwright-cli`:",
	} {
		if strings.Contains(content, snippet) {
			t.Fatalf("expected skill-owned tool routing %q to stay out of 03_tools instruction template", snippet)
		}
	}
}

// TestAutoLoopSkillContractsDoNotDeferForPointFixScope enforces the
// autonomous-loop contract that accepted findings stay actionable unless they
// cross a real human-review gate. The prompt text is the runtime behavior here,
// so these assertions guard against reintroducing "too broad for a point fix"
// as a deferral reason.
func TestAutoLoopSkillContractsDoNotDeferForPointFixScope(t *testing.T) {
	required := map[string][]string{
		"skills/auto-skill-loop/references/blocker-classification.md": {
			"Fix size, multi-file scope, or \"broader scope than a point fix\" is not a user-only blocker by itself.",
		},
		"skills/audit-and-fix-uncommitted/SKILL.md": {
			"A broad-but-clear fix is still in scope when it resolves an accepted finding against the working-tree target and does not trigger a human checkpoint.",
		},
		"skills/resolve-findings/SKILL.md": {
			"Fix size alone is not scope: broad, multi-file, or non-point fixes remain in scope when they resolve accepted findings against the reviewed target.",
		},
	}
	for path, snippets := range required {
		data, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%q) error: %v", path, err)
		}
		content := strings.Join(strings.Fields(string(data)), " ")
		for _, snippet := range snippets {
			if !strings.Contains(content, snippet) {
				t.Fatalf("expected %s to contain %q", path, snippet)
			}
		}
	}

	for _, forbidden := range []struct {
		path    string
		snippet string
	}{
		{
			path:    "skills/resolve-findings/SKILL.md",
			snippet: "If the finding is technically true but out of scope for this run, mark it `defer`.",
		},
		{
			path:    "skills/audit-and-fix-uncommitted/SKILL.md",
			snippet: "Required: ask when an accepted finding requires materially broader scope",
		},
		{
			path:    "skills/resolve-findings/SKILL.md",
			snippet: "Required: ask when an accepted fix would require materially broader scope",
		},
	} {
		data, err := Read(forbidden.path)
		if err != nil {
			t.Fatalf("Read(%q) error: %v", forbidden.path, err)
		}
		if strings.Contains(string(data), forbidden.snippet) {
			t.Fatalf("expected %s not to contain stale deferral rule %q", forbidden.path, forbidden.snippet)
		}
	}
}

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/find-issues/SKILL.md",
		"skills/mechanical-cleanup/SKILL.md",
		"skills/audit-and-fix-uncommitted-changes/SKILL.md",
		"skills/simplify-code/SKILL.md",
	} {
		if _, err := Read(path); err == nil {
			t.Fatalf("expected removed skill template %s to stay absent", path)
		}
	}
}
