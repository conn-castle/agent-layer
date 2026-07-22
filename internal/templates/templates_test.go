package templates

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedDispatchWorkflowSkillsEncodeReliabilityContract(t *testing.T) {
	dispatchTemplate, err := Read("skills-catalog/agent-dispatch/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	dispatchSkill := string(dispatchTemplate)
	if !strings.Contains(dispatchSkill, "al dispatch --help") || !strings.Contains(dispatchSkill, "al dispatch options") || !strings.Contains(dispatchSkill, "al dispatch start --agent <agent> --prompt-file <path>") || !strings.Contains(dispatchSkill, "al dispatch wait <handle>") || !strings.Contains(dispatchSkill, "al dispatch continue <handle> --prompt-file <path>") || !strings.Contains(dispatchSkill, "al dispatch cancel <handle>") {
		t.Fatal("agent-dispatch skill lacks the asynchronous conversation workflow")
	}
	reviewTemplate, err := Read("skills/review-plan/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	reviewSkill := string(reviewTemplate)
	if !strings.Contains(reviewSkill, "al dispatch start --agent <reviewer-agent> --model <reviewer-model>") || !strings.Contains(reviewSkill, "al dispatch wait <handle>") || !strings.Contains(reviewSkill, `--prompt-file ".agent-layer/tmp/review-plan.<run-id>.prompt.md"`) {
		t.Fatal("review-plan skill lacks its reviewer dispatch commands")
	}
	reviewerTemplate, err := Read("skills/review-plan/references/agent-review-prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	reviewerPrompt := string(reviewerTemplate)
	if !strings.Contains(reviewerPrompt, "ask for it") || !strings.Contains(reviewerPrompt, "resume the dispatch with the answer") {
		t.Fatal("reviewer prompt lacks terminal missing-input contract")
	}
}

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
	if len(data) == 0 {
		t.Fatal("expected launcher template content")
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

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/complete-current-phase/SKILL.md",
		"skills/find-issues/SKILL.md",
		"skills/fix-issues/SKILL.md",
		"skills/finish-task/SKILL.md",
		"skills/loop-clean-and-fix/SKILL.md",
		"skills/mechanical-cleanup/SKILL.md",
		"skills/audit-and-fix-uncommitted/SKILL.md",
		"skills/audit-and-fix-uncommitted-changes/SKILL.md",
		"skills/prune-new-tests/SKILL.md",
		"skills/prune-new-tests/reviewer-prompt.md",
		"skills/repair-checks/SKILL.md",
		"skills/run-all-checks/SKILL.md",
		"skills/run-and-fix-checks/SKILL.md",
		"skills/simplify-new-code/SKILL.md",
		"skills/simplify-new-code/reviewer-prompt.md",
		"skills/simplify-code/SKILL.md",
		"skills/resolve-findings/SKILL.md",
	} {
		if _, err := Read(path); err == nil {
			t.Fatalf("expected removed skill template %s to stay absent", path)
		}
	}
}
