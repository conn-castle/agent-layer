package benchmark

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreatmentProjectionAndManifestContainOnlyEffectiveFiles(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "SKILL.md"), []byte("effective skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "workspace", "skills")
	if err := copyRequiredTree(source, destination); err != nil {
		t.Fatalf("copyRequiredTree: %v", err)
	}
	copied, err := os.ReadFile(filepath.Join(destination, "nested", "SKILL.md")) // #nosec G304 -- destination is a test-owned temporary projection.
	if err != nil || string(copied) != "effective skill" {
		t.Fatalf("copied projection = %q, %v", copied, err)
	}
	config := filepath.Join(t.TempDir(), ".agent-layer", "config.toml")
	if err := writeNormalizedDispatchConfig(config, []string{"writer", "reviewer"}, model, effort); err != nil {
		t.Fatalf("writeNormalizedDispatchConfig: %v", err)
	}
	manifestRoot := t.TempDir()
	if err := copyRequiredTree(destination, filepath.Join(manifestRoot, "workspace", "skills")); err != nil {
		t.Fatal(err)
	}
	if err := copyRequiredFile(config, filepath.Join(manifestRoot, ".agent-layer", "config.toml")); err != nil {
		t.Fatal(err)
	}
	manifest, err := treatmentManifest(manifestRoot, TreatmentInstructionsAndSkills, []string{"reviewer", "writer"})
	if err != nil {
		t.Fatalf("treatmentManifest: %v", err)
	}
	if len(manifest.Files) != 2 || strings.Join(manifest.RequiredRoles, ",") != "reviewer,writer" ||
		manifest.AgentTimeoutMultiplier != skillsAgentTimeoutFactor {
		t.Fatalf("manifest = %#v", manifest)
	}
	if _, _, err := buildDevelopmentLinuxBinary(t.TempDir(), t.TempDir(), "amd64"); err == nil {
		t.Fatal("development binary build accepted a non-checkout directory")
	}
}

func TestTreatmentManifestRejectsForbiddenContent(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := treatmentManifest(root, TreatmentInstructionsOnly, nil); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("forbidden treatment content error = %v", err)
	}
	if _, err := BuildTreatmentBundle(root, "mips", TreatmentInstructionsOnly, model, effort); err == nil {
		t.Fatal("unsupported architecture was accepted")
	}
}

func TestSkillsTreatmentPassesManifestTimeoutPolicyToPier(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := treatmentPierArguments(ExecutionRequest{
		Model:  model,
		Effort: effort,
		Bundle: &TreatmentBundle{
			Root: "/immutable-treatment",
			Manifest: TreatmentManifest{
				Mode:                   TreatmentInstructionsAndSkills,
				AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(arguments, " "), "--agent-timeout-multiplier 4") {
		t.Fatalf("Pier arguments omitted treatment timeout policy: %#v", arguments)
	}
}

func TestSkillsTreatmentUsesWorkflowRolesAndAutonomousPrompt(t *testing.T) {
	workflow, err := treatmentAssets.ReadFile("assets/workflow-prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	const autonomous = "This run is fully autonomous. If any human decision is required, answer and proceed.\n\n"
	if !strings.HasPrefix(string(workflow), autonomous) {
		t.Fatalf("workflow prompt does not start with the autonomous-run instruction")
	}
	for _, required := range []string{"Execute the following skills sequentially.", "instructions: .agent-layer/tmp/instructions.md", "plan_reviewers ({plan_reviewer_count}): {plan_reviewers}", "implementer: {implementer}", "code_reviewer: {code_reviewer}", "fixer: {fixer}"} {
		if !strings.Contains(string(workflow), required) {
			t.Fatalf("workflow prompt does not require %q", required)
		}
	}
	for _, forbidden := range []string{"closed task contract", "without asking for interactive input", "constraint overrides", "Never use `al dispatch continue`", "workflow is invalid"} {
		if strings.Contains(string(workflow), forbidden) {
			t.Fatalf("workflow prompt hard-enforces model behavior with %q", forbidden)
		}
	}
	// The prompt cites a path that a different asset writes. If the two drift,
	// every skills run points the workflow at a file that does not exist and the
	// failure is invisible until the transcript is read after the run is paid for.
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adapter), `REMOTE_INSTRUCTIONS_FILE = f"{REMOTE_WORKSPACE}/.agent-layer/tmp/instructions.md"`) {
		t.Fatal("adapter does not write the instructions file the workflow prompt cites")
	}
}

func TestBenchmarkDispatchGateFiltersOptionsAndRejectsWrongSelections(t *testing.T) {
	gate, err := treatmentAssets.ReadFile("assets/al_dispatch_gate.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`arguments[:2] == ["dispatch", "options"]`,
		`"suggestions": [model]`,
		`"allow_custom": False`,
		`arguments[:2] == ["dispatch", "start"]`,
		`selected_agent != agent`,
		`selected_model and selected_model != model`,
		`selected_effort and selected_effort != effort`,
		`arguments.extend(["--model", model])`,
		`arguments.extend(["--reasoning-effort", effort])`,
		`os.execv(REAL_AL, [REAL_AL, *arguments])`,
	} {
		if !strings.Contains(string(gate), required) {
			t.Fatalf("benchmark dispatch gate does not enforce %q", required)
		}
	}
}

func TestTreatmentAdapterLabelsReasoningEffortInRoleTargets(t *testing.T) {
	adapter, err := treatmentAssets.ReadFile("assets/pier_agent_layer.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`f"{self._treatment_agent} {self._treatment_model} with "`,
		`f"{self._treatment_reasoning_effort} reasoning-effort"`,
	} {
		if !strings.Contains(string(adapter), required) {
			t.Fatalf("DeepSWE adapter does not label role-target reasoning effort with %q", required)
		}
	}
}

func TestBuildInstructionsOnlyTreatmentFromRepositoryProjection(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := BuildTreatmentBundle(repoRoot, "amd64", TreatmentInstructionsOnly, model, effort)
	if err != nil {
		t.Fatalf("BuildTreatmentBundle: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(bundle.Root); err != nil {
			t.Errorf("remove treatment bundle: %v", err)
		}
	})

	if bundle.Manifest.Mode != TreatmentInstructionsOnly {
		t.Fatalf("manifest mode = %q", bundle.Manifest.Mode)
	}
	if bundle.LinuxBinary != "" || bundle.LinuxBinarySHA256 != "" {
		t.Fatal("instructions-only treatment unexpectedly included an Agent Layer binary")
	}
	if len(bundle.Manifest.RequiredRoles) != 0 {
		t.Fatalf("instructions-only required roles = %v", bundle.Manifest.RequiredRoles)
	}
	if bundle.ManifestHash == "" || bundle.AdapterSHA256 == "" {
		t.Fatal("treatment bundle omitted content-addressed identities")
	}
	if _, err := os.Stat(bundle.AdapterPath); err != nil {
		t.Fatalf("adapter projection: %v", err)
	}
	assertTemplateProjection(
		t,
		filepath.Join(repoRoot, "internal", "templates", "instructions"),
		filepath.Join(bundle.Root, ".agent-layer", "instructions"),
	)
	for _, file := range bundle.Manifest.Files {
		if strings.Contains(file.Path, "/skills/") || strings.HasSuffix(file.Path, "/SKILL.md") {
			t.Fatalf("instructions-only treatment leaked a skill: %s", file.Path)
		}
	}
}

func TestBuildSkillsTreatmentIsIndependentOfTemporaryStagePath(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	first, err := BuildTreatmentBundle(repoRoot, "amd64", TreatmentInstructionsAndSkills, model, effort)
	if err != nil {
		t.Fatalf("first BuildTreatmentBundle: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(first.Root); err != nil {
			t.Errorf("remove first treatment bundle: %v", err)
		}
	})
	second, err := BuildTreatmentBundle(repoRoot, "amd64", TreatmentInstructionsAndSkills, model, effort)
	if err != nil {
		t.Fatalf("second BuildTreatmentBundle: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(second.Root); err != nil {
			t.Errorf("remove second treatment bundle: %v", err)
		}
	})
	if first.Root == second.Root {
		t.Fatal("test did not exercise distinct temporary stage paths")
	}
	if first.ManifestHash != second.ManifestHash {
		t.Fatalf("temporary stage path changed treatment identity: %s != %s", first.ManifestHash, second.ManifestHash)
	}
	assertTemplateProjection(
		t,
		filepath.Join(repoRoot, "internal", "templates", "instructions"),
		filepath.Join(first.Root, ".agent-layer", "instructions"),
	)
	assertTemplateProjection(
		t,
		filepath.Join(repoRoot, "internal", "templates", "skills"),
		filepath.Join(first.Root, ".agent-layer", "skills"),
	)
	assertTemplateProjection(
		t,
		filepath.Join(repoRoot, "internal", "templates", "skills-catalog", "agent-dispatch"),
		filepath.Join(first.Root, ".agent-layer", "skills", "agent-dispatch"),
	)
	for _, bundle := range []*TreatmentBundle{first, second} {
		for _, file := range bundle.Manifest.Files {
			if strings.HasPrefix(file.Path, ".agent-layer/bin/") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(bundle.Root, filepath.FromSlash(file.Path))) // #nosec G304 -- manifest paths are rooted in test-owned bundles.
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), filepath.ToSlash(bundle.Root)) {
				t.Fatalf("treatment file %s retained temporary stage root", file.Path)
			}
		}
	}
}

func assertTemplateProjection(t *testing.T, source, destination string) {
	t.Helper()
	sourceFS := os.DirFS(source)
	destinationFS := os.DirFS(destination)
	if err := fs.WalkDir(sourceFS, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		want, err := fs.ReadFile(sourceFS, path)
		if err != nil {
			return err
		}
		got, err := fs.ReadFile(destinationFS, path)
		if err != nil {
			return err
		}
		if string(got) != string(want) {
			t.Fatalf("staged treatment file %s differs from canonical template", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("compare canonical template projection: %v", err)
	}
}

// Dirty-tree runs are allowed: TreatmentManifestHash pins file contents, so the
// run stays reproducible. The flag exists so a report can say the commit alone
// is not enough, and it must react only to the templates tree.
func TestTemplatesProvenanceReportsCommitAndScopesDirtinessToTemplates(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...) // #nosec G204 -- fixed git arguments below a test-owned root.
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	write := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if commit, dirty, err := templatesProvenance(t.Context(), root); err != nil || commit != "" || dirty {
		t.Fatalf("non-repository root reported provenance: %q %v %v", commit, dirty, err)
	}

	run("init")
	run("config", "user.email", "benchmark@local.invalid")
	run("config", "user.name", "Benchmark")
	write("internal/templates/skills/example/SKILL.md", "original\n")
	write("unrelated.txt", "original\n")
	run("add", "-A")
	run("commit", "-m", "initial")

	commit, dirty, err := templatesProvenance(t.Context(), root)
	if err != nil || len(commit) != 40 || dirty {
		t.Fatalf("clean tree = %q dirty=%v err=%v", commit, dirty, err)
	}

	write("unrelated.txt", "changed\n")
	if _, dirty, err := templatesProvenance(t.Context(), root); err != nil || dirty {
		t.Fatalf("a change outside internal/templates was reported as dirty: %v %v", dirty, err)
	}

	write("internal/templates/skills/example/SKILL.md", "edited\n")
	if changed, dirty, err := templatesProvenance(t.Context(), root); err != nil || !dirty || changed != commit {
		t.Fatalf("edited templates tree = %q dirty=%v err=%v", changed, dirty, err)
	}
}
