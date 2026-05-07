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

func TestSkillTemplatesIncludeRequiredFrontMatterAndSections(t *testing.T) {
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "SKILL.md" {
			t.Fatalf("unexpected skill template path %q: expected SKILL.md files only", path)
		}
		data, err := Read(path)
		if err != nil {
			return err
		}
		content := string(data)
		for _, snippet := range []string{
			"\nname: ",
			"\ndescription:",
			"## Global constraints",
			"## Guardrails",
			"## Definition of done",
			"## Final handoff",
		} {
			if !strings.Contains(content, snippet) {
				t.Fatalf("expected %q in %s", snippet, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}

func TestSkillTemplatesKeepDefinitionOfDoneBeforeFinalHandoff(t *testing.T) {
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := Read(path)
		if err != nil {
			return err
		}
		content := string(data)
		definitionIndex := strings.Index(content, "## Definition of done")
		finalIndex := strings.Index(content, "## Final handoff")
		if definitionIndex < 0 || finalIndex < 0 {
			t.Fatalf("expected definition-of-done and final-handoff sections in %s", path)
		}
		if definitionIndex > finalIndex {
			t.Fatalf("expected Definition of done before Final handoff in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}

func TestSkillTemplatesCaptureArtifactReportConventions(t *testing.T) {
	tests := map[string][]string{
		"skills/audit-and-fix-uncommitted-changes/SKILL.md": {
			".agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md",
			"Create the file with `touch` before writing.",
			"Escalate if the loop is not converging",
		},
		"skills/resolve-findings/SKILL.md": {
			".agent-layer/tmp/resolve-findings.<run-id>.report.md",
			"Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.",
			"Create each file with `touch` before writing.",
		},
		"skills/review-scope/SKILL.md": {
			".agent-layer/tmp/review-scope.<run-id>.report.md",
			"## Self-Check",
			"Every finding names location, severity, confidence, evidence, and recommendation",
		},
		"skills/verify-against-plan/SKILL.md": {
			".agent-layer/tmp/verify-against-plan.<run-id>.report.md",
			"Create the file with `touch` before writing.",
		},
	}
	for path, requiredSnippets := range tests {
		data, err := Read(path)
		if err != nil {
			t.Fatalf("Read error for %s: %v", path, err)
		}
		content := string(data)
		for _, snippet := range requiredSnippets {
			if !strings.Contains(content, snippet) {
				t.Fatalf("expected %q in %s", snippet, path)
			}
		}
	}
}

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/find-issues/SKILL.md",
		"skills/mechanical-cleanup/SKILL.md",
	} {
		if _, err := Read(path); err == nil {
			t.Fatalf("expected removed skill template %s to stay absent", path)
		}
	}
}

func TestSkillTemplatesAvoidMandatoryApprovalGateLanguage(t *testing.T) {
	disallowed := []string{
		"mandatory approval gate",
		"stop and require explicit human approval",
		"approval-gated",
	}
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		data, err := Read(path)
		if err != nil {
			return err
		}
		content := string(data)
		for _, snippet := range disallowed {
			if strings.Contains(content, snippet) {
				t.Fatalf("did not expect %q in %s", snippet, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}
