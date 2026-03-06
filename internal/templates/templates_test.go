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
	if !strings.Contains(content, ".agent-layer/tmp/review-plan.<run-id>.report.md") {
		t.Fatalf("expected review-plan report artifact naming in review-plan template")
	}
	if !strings.Contains(content, "If no valid plan/task pair exists") {
		t.Fatalf("expected explicit no-plan-pair fallback in review-plan template")
	}
}

func TestSkillTemplatesContainNormalizedWorkflowGuidance(t *testing.T) {
	tests := map[string][]string{
		"skills/audit-and-fix-uncommitted-changes/SKILL.md": {
			"## Human checkpoints",
			"all uncommitted changes in the current working tree",
			"Do not stop merely because Critical and High findings reach zero",
			"### Phase 0: Preflight (Repo scout)",
			"## Final handoff",
		},
		"skills/audit-documentation/SKILL.md": {
			"## Human checkpoints",
			"requested scope is ambiguous enough that the audit target itself is unclear",
		},
		"skills/boost-coverage/SKILL.md": {
			"## Human checkpoints",
			"ask when no threshold is documented",
		},
		"skills/mechanical-cleanup/SKILL.md": {
			"## Human checkpoints",
			"ask when no credible verification path exists for a non-trivial cleanup",
		},
		"skills/plan-work/SKILL.md": {
			"## Global constraints",
			"first incomplete roadmap phase",
			"### Phase 1: Preflight (Scout)",
		},
		"skills/review-scope/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			"proactive hotspot audit",
		},
		"skills/finish-task/SKILL.md": {
			"## Global constraints",
			"### Phase 2: Curate memory updates (Memory curator)",
		},
		"skills/fix-issues/SKILL.md": {
			"## Global constraints",
			"When no broader orchestrator already owns closeout, use the `finish-task` skill here.",
		},
		"skills/resolve-findings/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			"Apply accepted fixes to the actual reviewed target",
		},
		"skills/repair-checks/SKILL.md": {
			"## Human checkpoints",
			"ask when the required check lane is unclear or conflicting",
		},
		"skills/implement-plan/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			"When no broader orchestrator already owns closeout, use the `finish-task` skill after Phase 4.",
		},
		"skills/verify-against-plan/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			"### Phase 1: Extract the contract (Plan reader)",
		},
		"skills/complete-current-phase/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			"Do not jump ahead to a later incomplete phase unless the user explicitly names it.",
			"selected roadmap phase is fully complete",
			"Do not stop after a single package if unchecked tasks still remain in the selected phase.",
		},
		"skills/review-plan/SKILL.md": {
			"## Global constraints",
			"## Human checkpoints",
			".agent-layer/tmp/review-plan.<run-id>.report.md",
		},
		"skills/schedule-backlog/SKILL.md": {
			"## Human checkpoints",
			"requested apply step would commit to a non-obvious prioritization or sequencing choice",
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

func TestSkillTemplatesCaptureArtifactReportConventions(t *testing.T) {
	tests := map[string][]string{
		"skills/audit-and-fix-uncommitted-changes/SKILL.md": {
			".agent-layer/tmp/audit-and-fix-uncommitted-changes.<run-id>.report.md",
			"Create the file with `touch` before writing.",
			"Recommended cap: no more than 4 audit/fix rounds for the same working tree before escalating.",
		},
		"skills/implement-plan/SKILL.md": {
			"Use `run-id = YYYYMMDD-HHMMSS-<short-rand>`.",
			"Create the file with `touch` before writing.",
		},
		"skills/resolve-findings/SKILL.md": {
			"Use one shared `run-id = YYYYMMDD-HHMMSS-<short-rand>`.",
			"Create each file with `touch` before writing.",
		},
		"skills/verify-against-plan/SKILL.md": {
			".agent-layer/tmp/verify-against-plan.<run-id>.report.md",
			"Create the file with `touch` before writing.",
		},
		"skills/boost-coverage/SKILL.md": {
			".agent-layer/tmp/boost-coverage.<run-id>.report.md",
			"## Required report structure",
		},
		"skills/mechanical-cleanup/SKILL.md": {
			".agent-layer/tmp/mechanical-cleanup.<run-id>.report.md",
			"## Required report structure",
		},
		"skills/repair-checks/SKILL.md": {
			".agent-layer/tmp/repair-checks.<run-id>.report.md",
			"## Required report structure",
		},
		"skills/finish-task/SKILL.md": {
			".agent-layer/tmp/finish-task.<run-id>.report.md",
			"## Required report structure",
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

func TestCompleteCurrentPhaseTemplateClarifiesLoopControl(t *testing.T) {
	data, err := Read("skills/complete-current-phase/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)

	required := []string{
		"Use subagents liberally when available.",
		"Use the `review-scope` skill on the actual implementation:",
		"- then continue to Phase 6",
		"Count every return to Phase 6 after Phase 7 begins, including cleanup-triggered returns, toward the same loop cap.",
		"Recommended cap: no more than 2 Phase 6-8 review/audit loops for the same work package before escalating.",
	}
	for _, snippet := range required {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in skills/complete-current-phase/SKILL.md", snippet)
		}
	}

	disallowed := []string{
		"Use the `review-scope` skill again, this time on the actual implementation:",
		"- then jump back to Phase 6 and Phase 7",
	}
	for _, snippet := range disallowed {
		if strings.Contains(content, snippet) {
			t.Fatalf("did not expect %q in skills/complete-current-phase/SKILL.md", snippet)
		}
	}
}

func TestAuditAndFixUncommittedChangesTemplateUsesSingleEscalationSection(t *testing.T) {
	data, err := Read("skills/audit-and-fix-uncommitted-changes/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)

	required := []string{
		"## Human checkpoints",
		"global round cap is reached",
		"## Final handoff",
	}
	for _, snippet := range required {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in skills/audit-and-fix-uncommitted-changes/SKILL.md", snippet)
		}
	}

	if strings.Contains(content, "## Stop conditions") {
		t.Fatal("did not expect separate stop-conditions section in skills/audit-and-fix-uncommitted-changes/SKILL.md")
	}
}

func TestSkillTemplatesAvoidBackwardLanguageForForwardPhaseTransitions(t *testing.T) {
	tests := map[string][]string{
		"skills/complete-current-phase/SKILL.md": {
			"- then continue to Phase 6",
		},
		"skills/implement-plan/SKILL.md": {
			"- then continue to Phase 4",
		},
		"skills/fix-issues/SKILL.md": {
			"- then continue to Phase 5",
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

	disallowed := map[string][]string{
		"skills/implement-plan/SKILL.md": {
			"- then jump back to Phase 4",
		},
		"skills/fix-issues/SKILL.md": {
			"- then jump back to Phase 5",
		},
	}
	for path, forbiddenSnippets := range disallowed {
		data, err := Read(path)
		if err != nil {
			t.Fatalf("Read error for %s: %v", path, err)
		}
		content := string(data)
		for _, snippet := range forbiddenSnippets {
			if strings.Contains(content, snippet) {
				t.Fatalf("did not expect %q in %s", snippet, path)
			}
		}
	}
}

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/find-issues/SKILL.md",
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

func TestAllSkillTemplatesExposeConstraintAndGuardrailSections(t *testing.T) {
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
		for _, snippet := range []string{
			"## Global constraints",
			"## Guardrails",
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
