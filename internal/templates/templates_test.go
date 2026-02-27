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

func TestReadReviewPlanSkillTemplate(t *testing.T) {
	data, err := Read("skills/review-plan/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, ".agent-layer/tmp/*.plan.md") {
		t.Fatalf("expected plan glob discovery in review-plan template")
	}
	if !strings.Contains(content, "<workflow>.<run-id>.plan.md") {
		t.Fatalf("expected standard artifact naming in review-plan template")
	}
	if !strings.Contains(content, "If no valid plan artifact exists") {
		t.Fatalf("expected explicit no-plan fallback in review-plan template")
	}
}

func TestSkillTemplatesIncludeRequiredFrontMatter(t *testing.T) {
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
		if !strings.Contains(content, "\nname: ") {
			t.Fatalf("expected name frontmatter in %s", path)
		}
		if !strings.Contains(content, "\ndescription:") {
			t.Fatalf("expected description frontmatter in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}
