package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestWriteStatuslineSources_SeedsMissingEnabledSources(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, true)

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeStatuslineSources(); err != nil {
		t.Fatalf("writeStatuslineSources: %v", err)
	}

	assertStatuslineSourceMatchesTemplate(t, root, ".agent-layer/claude-statusline.sh", "claude-statusline.sh")
	assertStatuslineSourceMatchesTemplate(t, root, ".agent-layer/codex-statusline.toml", "codex-statusline.toml")
}

func TestWriteStatuslineSources_PreservesExistingCustomSourceWithoutPrompt(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, false)
	sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
	if err := os.WriteFile(sourcePath, []byte("# custom statusline\n"), 0o600); err != nil {
		t.Fatalf("write custom source: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeStatuslineSources(); err != nil {
		t.Fatalf("writeStatuslineSources: %v", err)
	}

	data, err := os.ReadFile(sourcePath) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read custom source: %v", err)
	}
	if string(data) != "# custom statusline\n" {
		t.Fatalf("expected custom source to be preserved, got %q", string(data))
	}
}

func TestWriteStatuslineSources_OverwritesCustomSourceWhenPromptApproves(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, false)
	sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
	if err := os.WriteFile(sourcePath, []byte("# custom statusline\n"), 0o600); err != nil {
		t.Fatalf("write custom source: %v", err)
	}

	var promptedPath string
	inst := &installer{
		root: root,
		sys:  RealSystem{},
		prompter: PromptFuncs{
			StatuslineSourcePreviewFunc: func(preview DiffPreview) (bool, error) {
				promptedPath = preview.Path
				return true, nil
			},
		},
	}
	if err := inst.writeStatuslineSources(); err != nil {
		t.Fatalf("writeStatuslineSources: %v", err)
	}

	if promptedPath != ".agent-layer/claude-statusline.sh" {
		t.Fatalf("prompted path = %q", promptedPath)
	}
	assertStatuslineSourceMatchesTemplate(t, root, ".agent-layer/claude-statusline.sh", "claude-statusline.sh")
}

func TestBuildUpgradePlan_StatuslineSourceAdditionsAndUpdates(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, true)
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "claude-statusline.sh"), []byte("# custom statusline\n"), 0o600); err != nil {
		t.Fatalf("write custom claude source: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("BuildUpgradePlan: %v", err)
	}

	if findUpgradeChange(plan.StatuslineSourceUpdates, ".agent-layer/claude-statusline.sh") == nil {
		t.Fatalf("expected claude statusline source update in plan")
	}
	if findUpgradeChange(plan.StatuslineSourceAdditions, ".agent-layer/codex-statusline.toml") == nil {
		t.Fatalf("expected codex statusline source addition in plan")
	}

	previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("BuildUpgradePlanDiffPreviews: %v", err)
	}
	for _, path := range []string{".agent-layer/claude-statusline.sh", ".agent-layer/codex-statusline.toml"} {
		if previews[path].Path != path {
			t.Fatalf("expected diff preview for %s, got %#v", path, previews[path])
		}
	}
}

func TestBuildUpgradePlanDiffPreviews_StatuslineAdditionUsesLegacySourceContent(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, false)
	legacyPath := filepath.Join(root, ".agent-layer", "statusline.sh")
	if err := os.WriteFile(legacyPath, []byte("# legacy statusline\n"), 0o600); err != nil {
		t.Fatalf("write legacy source: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("BuildUpgradePlan: %v", err)
	}
	if findUpgradeChange(plan.StatuslineSourceAdditions, ".agent-layer/claude-statusline.sh") == nil {
		t.Fatalf("expected claude statusline source addition in plan")
	}

	previews, err := BuildUpgradePlanDiffPreviews(root, plan, UpgradePlanDiffPreviewOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("BuildUpgradePlanDiffPreviews: %v", err)
	}
	preview := previews[".agent-layer/claude-statusline.sh"]
	if !strings.Contains(preview.UnifiedDiff, "# legacy statusline") {
		t.Fatalf("expected preview to show legacy source content, got:\n%s", preview.UnifiedDiff)
	}
	if !strings.Contains(preview.UnifiedDiff, ".agent-layer/statusline.sh") {
		t.Fatalf("expected preview label to name legacy source, got:\n%s", preview.UnifiedDiff)
	}
}

func TestWriteStatuslineSources_NoConfigAndDisabledAreNoop(t *testing.T) {
	t.Run("missing config", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}

		if err := inst.writeStatuslineSources(); err != nil {
			t.Fatalf("writeStatuslineSources: %v", err)
		}
		assertNoStatuslineSources(t, root)
	})

	t.Run("absent keys disabled", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, false, false)
		inst := &installer{root: root, sys: RealSystem{}}

		if err := inst.writeStatuslineSources(); err != nil {
			t.Fatalf("writeStatuslineSources: %v", err)
		}
		assertNoStatuslineSources(t, root)
	})
}

func TestWriteStatuslineSources_SeedsClaudeFromLegacySource(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, true, false)
	legacyPath := filepath.Join(root, ".agent-layer", "statusline.sh")
	if err := os.WriteFile(legacyPath, []byte("# legacy statusline\n"), 0o600); err != nil {
		t.Fatalf("write legacy source: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeStatuslineSources(); err != nil {
		t.Fatalf("writeStatuslineSources: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, ".agent-layer", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read seeded source: %v", err)
	}
	if string(got) != "# legacy statusline\n" {
		t.Fatalf("expected legacy source content, got %q", string(got))
	}
}

func TestWriteStatuslineSources_SourcePathDirectoryErrors(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, false, true)
	sourcePath := filepath.Join(root, ".agent-layer", "codex-statusline.toml")
	if err := os.MkdirAll(sourcePath, 0o750); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeStatuslineSources(); err == nil {
		t.Fatal("expected directory source error")
	}
}

func TestWriteStatuslineSources_ErrorBranches(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			t.Fatalf("mkdir config dir: %v", err)
		}
		if err := os.WriteFile(configPath, []byte("[agents.claude\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		err := (&installer{root: root, sys: RealSystem{}}).writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "config") {
			t.Fatalf("expected config load error, got %v", err)
		}
	})

	t.Run("source stat", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(sourcePath)] = errors.New("stat boom")

		err := (&installer{root: root, sys: sys}).writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("legacy source read", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		legacyPath := filepath.Join(root, ".agent-layer", "statusline.sh")
		if err := os.WriteFile(legacyPath, []byte("# legacy\n"), 0o600); err != nil {
			t.Fatalf("write legacy source: %v", err)
		}
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(legacyPath)] = errors.New("legacy boom")

		err := (&installer{root: root, sys: sys}).writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "legacy boom") {
			t.Fatalf("expected legacy read error, got %v", err)
		}
	})

	t.Run("write source", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		sys := newFaultSystem(RealSystem{})
		sys.writeErrs[normalizePath(sourcePath)] = errors.New("write boom")

		err := (&installer{root: root, sys: sys}).writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected source write error, got %v", err)
		}
	})
}

func TestWriteStatuslineSource_CustomSourceBranches(t *testing.T) {
	t.Run("template match skips prompt", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		writeStatuslineSourceTemplateForTest(t, root, ".agent-layer/claude-statusline.sh", "claude-statusline.sh")
		prompted := false
		inst := &installer{
			root: root,
			sys:  RealSystem{},
			prompter: PromptFuncs{
				StatuslineSourcePreviewFunc: func(DiffPreview) (bool, error) {
					prompted = true
					return true, nil
				},
			},
		}

		if err := inst.writeStatuslineSources(); err != nil {
			t.Fatalf("writeStatuslineSources: %v", err)
		}
		if prompted {
			t.Fatal("did not expect matching template source to prompt")
		}
	})

	t.Run("match read error", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		if err := os.WriteFile(sourcePath, []byte("# custom\n"), 0o600); err != nil {
			t.Fatalf("write custom source: %v", err)
		}
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(sourcePath)] = errors.New("match boom")

		err := (&installer{root: root, sys: sys}).writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "match boom") {
			t.Fatalf("expected match read error, got %v", err)
		}
	})

	t.Run("prompt declined", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		custom := []byte("# custom statusline\n")
		if err := os.WriteFile(sourcePath, custom, 0o600); err != nil {
			t.Fatalf("write custom source: %v", err)
		}
		inst := &installer{
			root: root,
			sys:  RealSystem{},
			prompter: PromptFuncs{
				StatuslineSourcePreviewFunc: func(DiffPreview) (bool, error) {
					return false, nil
				},
			},
		}

		if err := inst.writeStatuslineSources(); err != nil {
			t.Fatalf("writeStatuslineSources: %v", err)
		}
		got, err := os.ReadFile(sourcePath) // #nosec G304 -- test-controlled path.
		if err != nil {
			t.Fatalf("read custom source: %v", err)
		}
		if string(got) != string(custom) {
			t.Fatalf("expected declined prompt to preserve custom source, got %q", string(got))
		}
	})

	t.Run("prompt error", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, true, false)
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		if err := os.WriteFile(sourcePath, []byte("# custom\n"), 0o600); err != nil {
			t.Fatalf("write custom source: %v", err)
		}
		inst := &installer{
			root: root,
			sys:  RealSystem{},
			prompter: PromptFuncs{
				StatuslineSourcePreviewFunc: func(DiffPreview) (bool, error) {
					return false, errors.New("prompt boom")
				},
			},
		}

		err := inst.writeStatuslineSources()
		if err == nil || !strings.Contains(err.Error(), "prompt boom") {
			t.Fatalf("expected prompt error, got %v", err)
		}
	})
}

func TestStatuslineSourceHelpers_ErrorBranches(t *testing.T) {
	root := t.TempDir()
	source := StatuslineSourceTemplate{
		RelPath:      ".agent-layer/custom-statusline",
		TemplatePath: "claude-statusline.sh",
		Perm:         0o755,
	}

	t.Run("mkdir source parent", func(t *testing.T) {
		path := filepath.Join(root, ".agent-layer", "custom-statusline")
		sys := newFaultSystem(RealSystem{})
		sys.mkdirErrs[normalizePath(filepath.Dir(path))] = errors.New("mkdir boom")

		err := (&installer{root: root, sys: sys}).writeStatuslineSourceTemplate(source, path)
		if err == nil || !strings.Contains(err.Error(), "mkdir boom") {
			t.Fatalf("expected mkdir error, got %v", err)
		}
	})

	t.Run("template read", func(t *testing.T) {
		err := (&installer{root: root, sys: RealSystem{}}).writeStatuslineSourceTemplate(StatuslineSourceTemplate{
			RelPath:      ".agent-layer/missing-statusline",
			TemplatePath: "missing-statusline-template",
			Perm:         0o644,
		}, filepath.Join(root, ".agent-layer", "missing-statusline"))
		if err == nil || !strings.Contains(err.Error(), "missing-statusline-template") {
			t.Fatalf("expected template read error, got %v", err)
		}
	})

	t.Run("preview source read", func(t *testing.T) {
		path := filepath.Join(root, ".agent-layer", "custom-statusline")
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(path)] = errors.New("preview read boom")

		_, err := (&installer{root: root, sys: sys}).buildStatuslineSourceDiffPreview(source)
		if err == nil || !strings.Contains(err.Error(), "preview read boom") {
			t.Fatalf("expected preview read error, got %v", err)
		}
	})

	t.Run("preview template read", func(t *testing.T) {
		path := filepath.Join(root, ".agent-layer", "custom-statusline")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("mkdir source dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("# custom\n"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}

		_, err := (&installer{root: root, sys: RealSystem{}}).buildStatuslineSourceDiffPreview(StatuslineSourceTemplate{
			RelPath:      ".agent-layer/custom-statusline",
			TemplatePath: "missing-statusline-template",
			Perm:         0o644,
		})
		if err == nil || !strings.Contains(err.Error(), "missing-statusline-template") {
			t.Fatalf("expected preview template read error, got %v", err)
		}
	})
}

func TestPlanStatuslineSourceChanges_UsesMigrationDefaults(t *testing.T) {
	root := t.TempDir()
	writeStatuslineConfigForTest(t, root, false, false)
	plan := migrationPlan{configMigrations: []ConfigKeyMigration{
		{Key: "agents.claude.statusline", From: "(unset)", To: "true"},
		{Key: "agents.codex.statusline", From: "(unset)", To: "false"},
	}}

	additions, updates, err := (&installer{root: root, sys: RealSystem{}}).planStatuslineSourceChanges(plan)
	if err != nil {
		t.Fatalf("planStatuslineSourceChanges: %v", err)
	}
	if !hasStatuslineTemplateChange(additions, ".agent-layer/claude-statusline.sh") {
		t.Fatalf("expected claude statusline addition from migration default")
	}
	if hasStatuslineTemplateChange(additions, ".agent-layer/codex-statusline.toml") {
		t.Fatalf("did not expect codex statusline addition for false migration default")
	}
	if len(updates) != 0 {
		t.Fatalf("expected no statusline updates, got %#v", updates)
	}
}

func TestPlanStatuslineSourceChanges_ErrorAndSkipBranches(t *testing.T) {
	t.Run("invalid config", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			t.Fatalf("mkdir config dir: %v", err)
		}
		if err := os.WriteFile(configPath, []byte("[agents.codex\n"), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}

		_, _, err := (&installer{root: root, sys: RealSystem{}}).planStatuslineSourceChanges(migrationPlan{})
		if err == nil || !strings.Contains(err.Error(), "config") {
			t.Fatalf("expected config load error, got %v", err)
		}
	})

	t.Run("source stat", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, false, true)
		sourcePath := filepath.Join(root, ".agent-layer", "codex-statusline.toml")
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(sourcePath)] = errors.New("stat boom")

		_, _, err := (&installer{root: root, sys: sys}).planStatuslineSourceChanges(migrationPlan{})
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("directory source skipped", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, false, true)
		sourcePath := filepath.Join(root, ".agent-layer", "codex-statusline.toml")
		if err := os.MkdirAll(sourcePath, 0o700); err != nil {
			t.Fatalf("mkdir source dir: %v", err)
		}

		additions, updates, err := (&installer{root: root, sys: RealSystem{}}).planStatuslineSourceChanges(migrationPlan{})
		if err != nil {
			t.Fatalf("planStatuslineSourceChanges: %v", err)
		}
		if len(additions) != 0 || len(updates) != 0 {
			t.Fatalf("expected directory source to be skipped, got additions=%#v updates=%#v", additions, updates)
		}
	})

	t.Run("match read error", func(t *testing.T) {
		root := t.TempDir()
		writeStatuslineConfigForTest(t, root, false, true)
		sourcePath := filepath.Join(root, ".agent-layer", "codex-statusline.toml")
		if err := os.WriteFile(sourcePath, []byte("# custom\n"), 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(sourcePath)] = errors.New("match boom")

		_, _, err := (&installer{root: root, sys: sys}).planStatuslineSourceChanges(migrationPlan{})
		if err == nil || !strings.Contains(err.Error(), "match boom") {
			t.Fatalf("expected match read error, got %v", err)
		}
	})
}

// TestStatuslineSourceEnabled_UnknownRelPathIsDisabled guards the default branch:
// any relPath that is not a known statusline source must report disabled, so an
// unrelated file never gets seeded. Would fail if the default returned true.
func TestStatuslineSourceEnabled_UnknownRelPathIsDisabled(t *testing.T) {
	cfg, err := config.LoadConfigLenient(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("load config: %v", err)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	if statuslineSourceEnabled(cfg, ".agent-layer/some-other-file") {
		t.Fatal("expected unknown statusline source relPath to be disabled")
	}
}

// TestStatuslineSourceSeedBytes_FallsBackToTemplateWithoutLegacy verifies that when
// no legacy source exists the seed content comes from the embedded template and is
// labeled "template". Would fail if seeding skipped the template fallback.
func TestStatuslineSourceSeedBytes_FallsBackToTemplateWithoutLegacy(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	source := StatuslineSourceTemplate{
		RelPath:       claudeStatuslineSourceRelPath,
		TemplatePath:  "claude-statusline.sh",
		LegacyRelPath: ".agent-layer/statusline.sh",
		Perm:          0o755,
	}

	data, origin, err := inst.statuslineSourceSeedBytes(source)
	if err != nil {
		t.Fatalf("statuslineSourceSeedBytes: %v", err)
	}
	if origin != "template" {
		t.Fatalf("origin = %q, want template", origin)
	}
	want, err := templates.Read("claude-statusline.sh")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if string(data) != string(want) {
		t.Fatal("seed bytes do not match embedded template")
	}
}

// TestStatuslineSourceSeedBytes_ReportsMissingTemplate guards the template-read
// error path: a source with no legacy file and an unknown template must surface a
// read error rather than seed empty content.
func TestStatuslineSourceSeedBytes_ReportsMissingTemplate(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}
	_, _, err := inst.statuslineSourceSeedBytes(StatuslineSourceTemplate{
		RelPath:      ".agent-layer/custom-statusline",
		TemplatePath: "missing-statusline-template",
		Perm:         0o644,
	})
	if err == nil || !strings.Contains(err.Error(), "missing-statusline-template") {
		t.Fatalf("expected missing template error, got %v", err)
	}
}

// TestStatuslineSourceEnabledAfterMigrations_Branches exercises the migration-aware
// enablement: explicit config wins, an unset key enabled by a "true" migration
// default counts as enabled, a "false" migration default does not, and an unknown
// relPath is disabled. Each branch would fail if its outcome were inverted.
func TestStatuslineSourceEnabledAfterMigrations_Branches(t *testing.T) {
	root := t.TempDir()

	t.Run("explicit config true wins over plan", func(t *testing.T) {
		writeStatuslineConfigForTest(t, root, true, false)
		cfg, err := config.LoadConfigLenient(filepath.Join(root, ".agent-layer", "config.toml"))
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !statuslineSourceEnabledAfterMigrations(cfg, claudeStatuslineSourceRelPath, migrationPlan{}) {
			t.Fatal("expected explicit statusline=true to be enabled")
		}
	})

	t.Run("unset key disabled without enabling migration", func(t *testing.T) {
		writeStatuslineConfigForTest(t, root, false, false)
		cfg, err := config.LoadConfigLenient(filepath.Join(root, ".agent-layer", "config.toml"))
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		plan := migrationPlan{configMigrations: []ConfigKeyMigration{
			{Key: "agents.codex.statusline", From: "(unset)", To: "false"},
		}}
		if statuslineSourceEnabledAfterMigrations(cfg, codexStatuslineSourceRelPath, plan) {
			t.Fatal("expected codex statusline disabled when migration default is false")
		}
	})

	t.Run("unknown relPath disabled", func(t *testing.T) {
		writeStatuslineConfigForTest(t, root, false, false)
		cfg, err := config.LoadConfigLenient(filepath.Join(root, ".agent-layer", "config.toml"))
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if statuslineSourceEnabledAfterMigrations(cfg, ".agent-layer/some-other-file", migrationPlan{}) {
			t.Fatal("expected unknown statusline source relPath to be disabled")
		}
	})
}

func writeStatuslineConfigForTest(t *testing.T, root string, claude bool, codex bool) {
	t.Helper()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	content := "[agents.claude]\n"
	if claude {
		content += "statusline = true\n"
	}
	content += "\n[agents.codex]\n"
	if codex {
		content += "statusline = true\n"
	}
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func assertNoStatuslineSources(t *testing.T, root string) {
	t.Helper()
	for _, relPath := range []string{".agent-layer/claude-statusline.sh", ".agent-layer/codex-statusline.toml"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relPath))); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be absent, stat err = %v", relPath, err)
		}
	}
}

func writeStatuslineSourceTemplateForTest(t *testing.T, root string, relPath string, templatePath string) {
	t.Helper()
	source, err := templates.Read(templatePath)
	if err != nil {
		t.Fatalf("read template %s: %v", templatePath, err)
	}
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	if err := os.WriteFile(path, source, 0o600); err != nil {
		t.Fatalf("write source %s: %v", relPath, err)
	}
}

func hasStatuslineTemplateChange(changes []upgradeChangeWithTemplate, path string) bool {
	for _, change := range changes {
		if change.path == path {
			return true
		}
	}
	return false
}

func assertStatuslineSourceMatchesTemplate(t *testing.T, root string, relPath string, templatePath string) {
	t.Helper()
	want, err := templates.Read(templatePath)
	if err != nil {
		t.Fatalf("read template %s: %v", templatePath, err)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath))) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("source %s does not match template %s", relPath, templatePath)
	}
}
