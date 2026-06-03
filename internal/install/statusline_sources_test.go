package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	source := statuslineSourceTemplate{
		relPath:      ".agent-layer/custom-statusline",
		templatePath: "claude-statusline.sh",
		perm:         0o755,
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
		err := (&installer{root: root, sys: RealSystem{}}).writeStatuslineSourceTemplate(statuslineSourceTemplate{
			relPath:      ".agent-layer/missing-statusline",
			templatePath: "missing-statusline-template",
			perm:         0o644,
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

		_, err := (&installer{root: root, sys: RealSystem{}}).buildStatuslineSourceDiffPreview(statuslineSourceTemplate{
			relPath:      ".agent-layer/custom-statusline",
			templatePath: "missing-statusline-template",
			perm:         0o644,
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
