package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func statuslineProject(enabled *bool) *config.ProjectConfig {
	return &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Statusline: enabled},
			},
		},
	}
}

func writeSourceStatusline(t *testing.T, root, content string) {
	t.Helper()
	writeStatuslineFile(t, root, "claude-statusline.sh", content)
}

func writeLegacySourceStatusline(t *testing.T, root, content string) {
	t.Helper()
	writeStatuslineFile(t, root, "statusline.sh", content)
}

func writeStatuslineFile(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir source dir: %v", err)
	}
	// Source perm is irrelevant: WriteClaudeStatusline forces 0o755 on the copy.
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write source statusline: %v", err)
	}
}

// When enabled, the editable source is copied verbatim to
// .claude/claude-statusline.sh. User edits to the source must be preserved (not
// replaced by the template).
func TestWriteClaudeStatusline_EnabledCopiesEditedSourceToNewPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const edited = "#!/usr/bin/env bash\necho my custom status\n"
	writeSourceStatusline(t, root, edited)

	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(nil)); err != nil {
		t.Fatalf("WriteClaudeStatusline: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, ".claude", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read projected statusline: %v", err)
	}
	if string(got) != edited {
		t.Fatalf("projected statusline = %q, want edited source %q", string(got), edited)
	}
	info, err := os.Stat(filepath.Join(root, ".claude", "claude-statusline.sh"))
	if err != nil {
		t.Fatalf("stat projected statusline: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("projected statusline perm = %v, want 0755", info.Mode().Perm())
	}
}

// With no source on disk, sync seeds .agent-layer/claude-statusline.sh from the
// embedded template and then projects it, so a standalone `al sync` is
// self-healing.
func TestWriteClaudeStatusline_SeedsMissingNewSourceFromTemplate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(nil)); err != nil {
		t.Fatalf("WriteClaudeStatusline: %v", err)
	}

	src, err := os.ReadFile(filepath.Join(root, ".agent-layer", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("expected source seeded: %v", err)
	}
	if !strings.Contains(string(src), "Claude Code status line") {
		t.Fatalf("seeded source does not look like the template:\n%s", string(src))
	}
	if !strings.Contains(string(src), "rate_limits.seven_day.used_percentage") {
		t.Fatalf("expected weekly limit parsing in seeded source:\n%s", string(src))
	}
	if !strings.Contains(string(src), "7d:") {
		t.Fatalf("expected weekly limit segment in seeded source:\n%s", string(src))
	}
	if !strings.Contains(string(src), ".pr.number") || !strings.Contains(string(src), "PR#") {
		t.Fatalf("expected PR segment in seeded source:\n%s", string(src))
	}
	dest, err := os.ReadFile(filepath.Join(root, ".claude", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("expected projection: %v", err)
	}
	if string(dest) != string(src) {
		t.Fatalf("projection does not match seeded source")
	}
}

func TestWriteClaudeStatusline_MigratesLegacySourceWhenNewMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const legacy = "#!/usr/bin/env bash\necho legacy edit\n"
	writeLegacySourceStatusline(t, root, legacy)

	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(nil)); err != nil {
		t.Fatalf("WriteClaudeStatusline: %v", err)
	}

	newSource, err := os.ReadFile(filepath.Join(root, ".agent-layer", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read migrated source: %v", err)
	}
	if string(newSource) != legacy {
		t.Fatalf("migrated source = %q, want legacy edit %q", string(newSource), legacy)
	}
	legacySource, err := os.ReadFile(filepath.Join(root, ".agent-layer", "statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("legacy source should remain: %v", err)
	}
	if string(legacySource) != legacy {
		t.Fatalf("legacy source changed: %q", string(legacySource))
	}
}

func TestWriteClaudeStatusline_BothSourcesUsesNewAndLeavesLegacy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const legacy = "#!/usr/bin/env bash\necho legacy\n"
	const current = "#!/usr/bin/env bash\necho current\n"
	writeLegacySourceStatusline(t, root, legacy)
	writeSourceStatusline(t, root, current)

	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(nil)); err != nil {
		t.Fatalf("WriteClaudeStatusline: %v", err)
	}

	projected, err := os.ReadFile(filepath.Join(root, ".claude", "claude-statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	if string(projected) != current {
		t.Fatalf("projected source = %q, want new source %q", string(projected), current)
	}
	legacySource, err := os.ReadFile(filepath.Join(root, ".agent-layer", "statusline.sh")) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatalf("legacy source should remain: %v", err)
	}
	if string(legacySource) != legacy {
		t.Fatalf("legacy source changed: %q", string(legacySource))
	}
}

// When disabled, a previously generated copy is removed so no stale script lingers.
func TestWriteClaudeStatusline_DisabledRemovesStaleCopiesAndPreservesSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSourceStatusline(t, root, "#!/usr/bin/env bash\necho keep me\n")
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	for _, name := range []string{"claude-statusline.sh", "statusline.sh"} {
		if err := os.WriteFile(filepath.Join(claudeDir, name), []byte("stale"), 0o600); err != nil {
			t.Fatalf("seed stale copy %s: %v", name, err)
		}
	}

	disabled := false
	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(&disabled)); err != nil {
		t.Fatalf("WriteClaudeStatusline disabled: %v", err)
	}
	for _, name := range []string{"claude-statusline.sh", "statusline.sh"} {
		if _, err := os.Stat(filepath.Join(claudeDir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected stale copy %s removed, stat err=%v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "claude-statusline.sh")); err != nil {
		t.Fatalf("source should be preserved: %v", err)
	}
}

// Disabling when nothing was generated is a clean no-op (no error, no files).
func TestWriteClaudeStatusline_DisabledNoCopyIsNoop(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disabled := false
	if err := WriteClaudeStatusline(RealSystem{}, root, statuslineProject(&disabled)); err != nil {
		t.Fatalf("WriteClaudeStatusline disabled no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "claude-statusline.sh")); !os.IsNotExist(err) {
		t.Fatalf("did not expect a projected statusline, stat err=%v", err)
	}
}

// A non-NotExist failure removing a stale copy on disable must surface, not be
// swallowed as a no-op.
func TestWriteClaudeStatusline_DisabledRemoveErrorSurfaces(t *testing.T) {
	t.Parallel()
	boom := errors.New("remove boom")
	sys := &MockSystem{
		Fallback:   RealSystem{},
		RemoveFunc: func(string) error { return boom },
	}
	disabled := false
	err := WriteClaudeStatusline(sys, t.TempDir(), statuslineProject(&disabled))
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped remove error, got %v", err)
	}
}

// A non-NotExist failure reading the editable source must surface rather than
// fall through to seeding.
func TestWriteClaudeStatusline_SourceReadErrorSurfaces(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	boom := errors.New("read boom")
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(name string) ([]byte, error) {
			if strings.HasSuffix(name, "claude-statusline.sh") {
				return nil, boom
			}
			return os.ReadFile(name) // #nosec G304 -- test-controlled path.
		},
	}
	err := WriteClaudeStatusline(sys, root, statuslineProject(nil))
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped source read error, got %v", err)
	}
}

// A failure creating the .claude destination directory must surface.
func TestWriteClaudeStatusline_DestMkdirErrorSurfaces(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSourceStatusline(t, root, "#!/usr/bin/env bash\necho hi\n")
	boom := errors.New("mkdir boom")
	sys := &MockSystem{
		Fallback: RealSystem{},
		MkdirAllFunc: func(path string, perm os.FileMode) error {
			if strings.HasSuffix(path, ".claude") {
				return boom
			}
			return os.MkdirAll(path, perm)
		},
	}
	err := WriteClaudeStatusline(sys, root, statuslineProject(nil))
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("expected wrapped mkdir error, got %v", err)
	}
}

// buildClaudeSettings wires statusLine to the projected script by default.
func TestBuildClaudeSettings_StatusLineEnabledByDefault(t *testing.T) {
	t.Parallel()
	settings, err := buildClaudeSettings("/repo", statuslineProject(nil))
	if err != nil {
		t.Fatalf("buildClaudeSettings: %v", err)
	}
	block, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("expected statusLine block, got %#v", settings["statusLine"])
	}
	if block["type"] != "command" {
		t.Fatalf("statusLine.type = %v, want command", block["type"])
	}
	want := "bash '" + filepath.Join("/repo", ".claude", "claude-statusline.sh") + "'"
	if block["command"] != want {
		t.Fatalf("statusLine.command = %v, want %q", block["command"], want)
	}
}

func TestBuildClaudeSettings_StatusLinePathIsShellQuoted(t *testing.T) {
	t.Parallel()
	root := "/repo with spaces/it's here"
	settings, err := buildClaudeSettings(root, statuslineProject(nil))
	if err != nil {
		t.Fatalf("buildClaudeSettings: %v", err)
	}
	block, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("expected statusLine block, got %#v", settings["statusLine"])
	}
	want := "bash '/repo with spaces/it'\\''s here/.claude/claude-statusline.sh'"
	if block["command"] != want {
		t.Fatalf("statusLine.command = %v, want %q", block["command"], want)
	}
}

// An explicit false opt-out omits the statusLine block entirely.
func TestBuildClaudeSettings_StatusLineDisabledOmitsBlock(t *testing.T) {
	t.Parallel()
	disabled := false
	settings, err := buildClaudeSettings("/repo", statuslineProject(&disabled))
	if err != nil {
		t.Fatalf("buildClaudeSettings: %v", err)
	}
	if _, ok := settings["statusLine"]; ok {
		t.Fatalf("expected no statusLine block when disabled, got %#v", settings["statusLine"])
	}
}

// A user-provided agent_specific.statusLine overrides the managed wiring.
func TestBuildClaudeSettings_AgentSpecificStatusLineWins(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					AgentSpecific: map[string]any{
						"statusLine": map[string]any{
							"type":    "command",
							"command": "bash /custom/line.sh",
						},
					},
				},
			},
		},
	}
	settings, err := buildClaudeSettings("/repo", project)
	if err != nil {
		t.Fatalf("buildClaudeSettings: %v", err)
	}
	block, ok := settings["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("expected statusLine block, got %#v", settings["statusLine"])
	}
	if block["command"] != "bash /custom/line.sh" {
		t.Fatalf("statusLine.command = %v, want user override", block["command"])
	}
}
