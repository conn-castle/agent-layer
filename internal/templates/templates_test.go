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
			return nil
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

func TestSkillTemplatesKeepDefinitionOfDoneBeforeFinalHandoff(t *testing.T) {
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "SKILL.md" {
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

func TestMemorySkillsConstrainDecisionLogBloat(t *testing.T) {
	tests := map[string][]string{
		"skills/audit-memory/SKILL.md": {
			"classify every entry as `keep`, `consolidate`, `remove`, or `defer`",
			"Group decisions by subsystem or decision axis",
			"Unique rationale alone is not enough to keep an entry",
			"the rationale must still guide future work",
		},
		"skills/finish-task/SKILL.md": {
			"search existing decisions for the same",
			"subsystem or decision axis",
			"consolidate them in",
			"the same closeout pass",
			"Do not append a DECISIONS.md entry while leaving an older superseded decision in place",
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

func TestFixCISkillDownloadsAvailableArtifacts(t *testing.T) {
	data, err := Read("skills/fix-ci/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"any artifacts available from the failed workflow run",
		"gh run download <run-id> --dir .agent-layer/tmp/ci-artifacts/<run-id>",
		"logs and artifacts together are the diagnostic source of truth",
		"missing or unavailable artifacts were called out explicitly",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in fix-ci skill", snippet)
		}
	}
}

func TestCISkillsRequireLocalReproducerBeforePush(t *testing.T) {
	tests := map[string][]string{
		"skills/fix-ci/SKILL.md": {
			"Do not push speculative fixes without a local reproducer.",
			"Treat GitHub-only failures as local-reproduction bugs",
			"write or adapt a local test or command that fails for the same reason",
			"stop at a human checkpoint instead of pushing a guess",
			"local reproducer command, initial red result, fix, and final green result",
		},
		"skills/ship-pr/SKILL.md": {
			"CI is not the first debugger",
			"Do not let ship-pr push CI-fix commits unless `fix-ci` found a local reproducer",
			"Run or delegate to `repair-checks` for the repo-defined local check lane",
			"CI-fix commits were not pushed without a local reproducer",
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

func TestShipPRSkillRequiresExplicitMergeAuthorizationAndSafeCleanup(t *testing.T) {
	data, err := Read("skills/ship-pr/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Sub-skill returns are intermediate, not terminal.",
		"I approve merging PR #<N>",
		"Do not pass `--admin`.",
		"viewerDefaultMergeMethod",
		"stop and ask the user to choose one of the allowed methods",
		"no remote maps unambiguously",
		"Never delete the repository's default branch",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in ship-pr skill", snippet)
		}
	}
}

func TestPRCommentPolicyLivesInAddressPRComments(t *testing.T) {
	addressData, err := Read("skills/address-pr-comments/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	addressContent := string(addressData)
	for _, snippet := range []string{
		"Comment reply format",
		"Fixed in `<short-hash>`",
		"No change",
		"Deferred",
		"Audit ran: include its `Key fixes applied` table (`| Round | Severity | Fix | Files |`); no audit: state why and what verification ran.",
		"missing reply",
		"hollow fix",
		"unjustified decline",
		"lazy deferral",
		"generic dismissal",
		"If a previously declined suggestion is subsequently implemented",
	} {
		if !strings.Contains(addressContent, snippet) {
			t.Fatalf("expected %q in address-pr-comments skill", snippet)
		}
	}

	shipData, err := Read("skills/ship-pr/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	shipContent := string(shipData)
	for _, snippet := range []string{
		"PR feedback handling must pass the `address-pr-comments` definition of done",
		"Verify the `address-pr-comments` definition of done against the fetched PR state",
	} {
		if !strings.Contains(shipContent, snippet) {
			t.Fatalf("expected %q in ship-pr skill", snippet)
		}
	}
	for _, snippet := range []string{
		"## Comment reply format",
		"hollow fix",
		"lazy deferral",
	} {
		if strings.Contains(shipContent, snippet) {
			t.Fatalf("did not expect comment-policy detail %q in ship-pr skill", snippet)
		}
	}
}

func TestPlanWorkSkillAsksSubstantiveQuestionsDuringDrafting(t *testing.T) {
	data, err := Read("skills/plan-work/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Ask substantive questions as they arise during planning",
		"Substantive questions are questions where the answer changes user-facing behavior, architecture, scope, sequencing, risk, or cost.",
		"Do not save substantive questions for the execution gatekeeper",
		"After the user answers, incorporate the decision into the draft",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in plan-work skill", snippet)
		}
	}
}

func TestInstructionTemplatesDefineQuestionStyle(t *testing.T) {
	data, err := Read("instructions/01_base.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Question Style",
		"clear, concise, and free of unnecessary jargon",
		"at least two concrete options",
		"Pros",
		"Cons",
		"Recommendation",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in 01_base instruction template", snippet)
		}
	}
}

func TestInstructionTemplatesRejectTautologicalOrSelfConfirmingTests(t *testing.T) {
	data, err := Read("instructions/00_rules.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"No tautological or self-confirming tests",
		"Every test must be able to fail because of a real implementation defect",
		"mocked values echoed back unchanged",
		"constraints already enforced by a language, compiler, type checker, schema, or static analyzer",
		"test behavior, logic, integration, and runtime failure modes instead",
		"Prefer a visible coverage gap to false coverage",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in 00_rules instruction template", snippet)
		}
	}

	conventions, err := Read("instructions/04_conventions.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if strings.Contains(string(conventions), "self-confirming tests") {
		t.Fatalf("expected tautological/self-confirming test rule to live in 00_rules, not 04_conventions")
	}
}

func TestAuditTestsSkillDeletesTautologicalOrSelfConfirmingTests(t *testing.T) {
	data, err := Read("skills/audit-tests/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Tautological/self-confirming tests",
		"assertions are satisfied",
		"delete clear cases instead of counting them as coverage",
		"only re-check constraints already enforced",
		"language, compiler, type checker, schema, or static analyzer",
		"report the resulting",
		"coverage gap instead of replacing them with false coverage",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in audit-tests skill", snippet)
		}
	}
	if strings.Contains(content, "Rules file") {
		t.Fatalf("expected audit-tests skill not to reference always-loaded rules")
	}
}

func TestBoostCoverageSkillRejectsTautologicalOrSelfConfirmingTests(t *testing.T) {
	data, err := Read("skills/boost-coverage/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Do not add tautological or self-confirming tests",
		"stop at the real shortfall instead",
		"Do not add runtime tests for constraints already enforced",
		"behavior, logic, integration, or runtime failure modes",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in boost-coverage skill", snippet)
		}
	}
	if strings.Contains(content, "Rules file") {
		t.Fatalf("expected boost-coverage skill not to reference always-loaded rules")
	}
}

func TestAuditAndFixSkillUsesCriticalHighAppliedFixGate(t *testing.T) {
	data, err := Read("skills/audit-and-fix-uncommitted-changes/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Use Critical and High applied-fix counts as the repeat gate.",
		"Rejected findings do not count toward the repeat gate.",
		"Critical/High applied fixes: <count>",
		"Do not run an automatic confirmation round after a round with zero Critical/High applied fixes.",
		"Present a **Key fixes applied** table sorted by Round then Severity.",
		"The Round and Severity columns are required",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in audit-and-fix skill", snippet)
		}
	}
	for _, snippet := range []string{
		"zero accepted findings",
		"The confirmation round must be a separate round",
	} {
		if strings.Contains(content, snippet) {
			t.Fatalf("did not expect %q in audit-and-fix skill", snippet)
		}
	}
}

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/find-issues/SKILL.md",
		"skills/mechanical-cleanup/SKILL.md",
		"skills/simplify-code/SKILL.md",
	} {
		if _, err := Read(path); err == nil {
			t.Fatalf("expected removed skill template %s to stay absent", path)
		}
	}
}

func TestSimplifyCodebaseSkillRenamed(t *testing.T) {
	data, err := Read("skills/simplify-codebase/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"name: simplify-codebase",
		".agent-layer/tmp/simplify-codebase.<run-id>.report.md",
		"Use `simplify-new-code` instead when the target is the current uncommitted",
		"Default scope is the full codebase",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in simplify-codebase skill", snippet)
		}
	}
	if strings.Contains(content, "name: simplify-code\n") {
		t.Fatalf("simplify-codebase skill still has legacy name")
	}
}

func TestPruneNewTestsSkillEnforcesBurdenOfProof(t *testing.T) {
	data, err := Read("skills/prune-new-tests/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"name: prune-new-tests",
		".agent-layer/tmp/prune-new-tests.<run-id>.report.md",
		"tests added in the current uncommitted diff",
		"concrete mutation in production code that would flip the assertion",
		"Burden-of-proof reviewer",
		"It does **not** receive the implementer's narrative",
		"Survival Check",
		"> 0.90",
		"Surviving Coverage Gaps",
		"`audit-tests` instead when the target is the full existing suite",
		"`boost-coverage` instead when the goal is to **add** tests",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in prune-new-tests skill", snippet)
		}
	}
}

func TestSimplifyNewCodeSkillScansForScopeCreep(t *testing.T) {
	skillData, err := Read("skills/simplify-new-code/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	skillContent := string(skillData)
	for _, snippet := range []string{
		"name: simplify-new-code",
		".agent-layer/tmp/simplify-new-code.<run-id>.report.md",
		"current uncommitted diff only",
		"Smell-pattern reviewer",
		"It does **not** receive the user's original",
		"Preserve the user-requested behavior",
		"Use `simplify-codebase` instead",
	} {
		if !strings.Contains(skillContent, snippet) {
			t.Fatalf("expected %q in simplify-new-code SKILL.md", snippet)
		}
	}

	promptData, err := Read("skills/simplify-new-code/reviewer-prompt.md")
	if err != nil {
		t.Fatalf("Read error for reviewer-prompt.md: %v", err)
	}
	promptContent := string(promptData)
	for _, snippet := range []string{
		"Speculative flexibility",
		"Premature abstraction",
		"Single-caller indirection",
		"Dead branches",
		"Error handling for impossible cases",
		"Defensive scaffolding",
		"Overly clever patterns",
		"Half-finished implementations",
	} {
		if !strings.Contains(promptContent, snippet) {
			t.Fatalf("expected %q in simplify-new-code reviewer-prompt.md", snippet)
		}
	}
}

func TestImplementPlanWiresInDiffScopedCleanupSkills(t *testing.T) {
	data, err := Read("skills/implement-plan/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"### Phase 4: Prune speculative tests",
		"Run the `prune-new-tests` skill against the uncommitted diff before verification.",
		"### Phase 5: Simplify agent-added code",
		"Run the `simplify-new-code` skill against the uncommitted diff before verification.",
		"### Phase 6: Verify against the plan",
		"`prune-new-tests` ran when Phase 2 added or modified test files",
		"`simplify-new-code` ran when Phase 2 produced production-code changes",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in implement-plan skill", snippet)
		}
	}
	if strings.Contains(content, "- use the `simplify-code` skill") {
		t.Fatalf("implement-plan still references legacy simplify-code skill")
	}
}

func TestAuditAndFixAddsCleanupPrePass(t *testing.T) {
	data, err := Read("skills/audit-and-fix-uncommitted-changes/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"### Phase 0.5: Prune agent-side scope creep before any review round",
		"**`prune-new-tests`** when the diff contains added test files",
		"**`simplify-new-code`** when the diff contains added or modified production code",
		"## Pre-pass Cleanup",
		"`prune-new-tests` (mandatory pre-pass when the diff added test files)",
		"`simplify-new-code` (mandatory pre-pass when the diff added or modified production code)",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in audit-and-fix-uncommitted-changes skill", snippet)
		}
	}
	if strings.Contains(content, "- `simplify-code` when a fix exposes obvious local complexity") {
		t.Fatalf("audit-and-fix still references legacy simplify-code skill")
	}
}

func TestVerifyAgainstPlanUsesPlanAnchoredReviewer(t *testing.T) {
	data, err := Read("skills/verify-against-plan/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	content := string(data)
	for _, snippet := range []string{
		"Plan-anchored reviewer",
		"no implementer narrative, no prior conversation, and no rationalizations",
		"The plan/task/context artifacts ARE in scope",
		"implementer's narrative ABOUT those artifacts is NOT in scope",
		"Phase 2 used the plan-anchored fresh-context reviewer subagent",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected %q in verify-against-plan skill", snippet)
		}
	}
}

func TestAddressPRCommentsPhase6UsesFreshContextAuditor(t *testing.T) {
	skillData, err := Read("skills/address-pr-comments/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	skillContent := string(skillData)
	for _, snippet := range []string{
		"### Phase 6: Audit reply coverage (Fresh-context comment auditor)",
		"fresh-context reviewer subagent",
		"every comment came back with a `pass` verdict",
	} {
		if !strings.Contains(skillContent, snippet) {
			t.Fatalf("expected %q in address-pr-comments SKILL.md", snippet)
		}
	}

	promptData, err := Read("skills/address-pr-comments/reviewer-prompt.md")
	if err != nil {
		t.Fatalf("Read error for reviewer-prompt.md: %v", err)
	}
	if !strings.Contains(string(promptData), "You are auditing a single PR comment reply") {
		t.Fatalf("expected reviewer-prompt.md to contain the auditor prompt opening")
	}
}

func TestImproveCodebasePerChunkReauditUsesFreshContext(t *testing.T) {
	skillData, err := Read("skills/improve-codebase/SKILL.md")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	skillContent := string(skillData)
	for _, snippet := range []string{
		"fresh-context reviewer subagent",
		"never the fix narrative",
		"per-chunk re-audit loops used a fresh-context reviewer subagent (not the fixer's context)",
	} {
		if !strings.Contains(skillContent, snippet) {
			t.Fatalf("expected %q in improve-codebase SKILL.md", snippet)
		}
	}

	promptData, err := Read("skills/improve-codebase/reviewer-prompt.md")
	if err != nil {
		t.Fatalf("Read error for reviewer-prompt.md: %v", err)
	}
	if !strings.Contains(string(promptData), "You are re-auditing a chunk of code after fixes were applied") {
		t.Fatalf("expected reviewer-prompt.md to contain the re-audit prompt opening")
	}
}

func TestFreshContextReviewerPromptsAreExtractedToSiblingFiles(t *testing.T) {
	cases := []struct {
		skill        string
		promptAnchor string
	}{
		{"prune-new-tests", "concrete mutation in the production"},
		{"simplify-new-code", "Speculative flexibility"},
		{"verify-against-plan", "undocumented_deviation"},
		{"address-pr-comments", "You are auditing a single PR comment reply"},
		{"improve-codebase", "You are re-auditing a chunk of code after fixes were applied"},
	}
	for _, tc := range cases {
		skillPath := "skills/" + tc.skill + "/SKILL.md"
		skillData, err := Read(skillPath)
		if err != nil {
			t.Fatalf("Read error %s: %v", skillPath, err)
		}
		if !strings.Contains(string(skillData), "reviewer-prompt.md") {
			t.Fatalf("expected %s to reference reviewer-prompt.md", skillPath)
		}

		promptPath := "skills/" + tc.skill + "/reviewer-prompt.md"
		promptData, err := Read(promptPath)
		if err != nil {
			t.Fatalf("Read error %s: %v", promptPath, err)
		}
		if !strings.Contains(string(promptData), tc.promptAnchor) {
			t.Fatalf("expected %s to contain %q", promptPath, tc.promptAnchor)
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
