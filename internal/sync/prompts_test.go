package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

const generatedMarkerFixture = "<!--\n  GENERATED FILE\n  Source: .agent-layer/slash-commands/test.md\n  Regenerate: al sync\n-->\n"

func TestBuildVSCodePrompt(t *testing.T) {
	cmd := config.SlashCommand{Name: "alpha", Body: "Body"}
	content := buildVSCodePrompt(cmd)
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in prompt")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestWriteVSCodePromptsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteVSCodePrompts(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodePromptsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	promptDir := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(promptDir, "alpha.prompt.md"), 0o755); err != nil {
		t.Fatalf("mkdir prompt: %v", err)
	}
	cmds := []config.SlashCommand{{Name: "alpha", Body: "Body"}}
	if err := WriteVSCodePrompts(RealSystem{}, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestRemoveStalePromptFilesMissingDir(t *testing.T) {
	t.Parallel()
	err := removeStalePromptFiles(RealSystem{}, filepath.Join(t.TempDir(), "missing"), map[string]struct{}{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRemoveStalePromptFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wanted := map[string]struct{}{
		"keep": {},
	}

	keep := filepath.Join(dir, "keep.prompt.md")
	stale := filepath.Join(dir, "stale.prompt.md")
	manual := filepath.Join(dir, "manual.prompt.md")
	other := filepath.Join(dir, "notes.txt")
	subdir := filepath.Join(dir, "nested")
	if err := os.WriteFile(keep, []byte(generatedMarkerFixture), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(stale, []byte(generatedMarkerFixture), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if err := os.WriteFile(manual, []byte("manual"), 0o644); err != nil {
		t.Fatalf("write manual: %v", err)
	}
	if err := os.WriteFile(other, []byte("note"), 0o644); err != nil {
		t.Fatalf("write other: %v", err)
	}
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	if err := removeStalePromptFiles(RealSystem{}, dir, wanted); err != nil {
		t.Fatalf("removeStalePromptFiles error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale to be removed")
	}
	if _, err := os.Stat(manual); err != nil {
		t.Fatalf("expected manual to remain: %v", err)
	}
}

func TestBuildCodexSkill(t *testing.T) {
	cmd := config.SlashCommand{Name: "alpha", Description: "desc", Body: "Body"}
	content := buildCodexSkill(cmd)
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "# alpha") {
		t.Fatalf("expected heading in skill")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestBuildAntigravitySkill(t *testing.T) {
	cmd := config.SlashCommand{Name: "alpha", Description: "desc", Body: "Body"}
	content := buildAntigravitySkill(cmd)
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "description:") {
		t.Fatalf("expected description in skill")
	}
	if !strings.Contains(content, "Body") {
		t.Fatalf("expected body in skill")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestWriteCodexSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteCodexSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteCodexSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillDir := filepath.Join(root, ".codex", "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(skillDir, "SKILL.md"), 0o755); err != nil {
		t.Fatalf("mkdir SKILL.md: %v", err)
	}
	cmds := []config.SlashCommand{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteCodexSkills(RealSystem{}, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAntigravitySkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteAntigravitySkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAntigravitySkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent", "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(skillDir, "SKILL.md"), 0o755); err != nil {
		t.Fatalf("mkdir SKILL.md: %v", err)
	}
	cmds := []config.SlashCommand{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteAntigravitySkills(RealSystem{}, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAntigravitySkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cmds := []config.SlashCommand{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteAntigravitySkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir creation failure")
	}
	if !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestWriteCodexSkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".codex", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Create a file where the skill directory would be created
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	cmds := []config.SlashCommand{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteCodexSkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir creation failure")
	}
	if !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("expected mkdir error, got %v", err)
	}
}

func TestWrapDescription(t *testing.T) {
	lines := wrapDescription("one two three four", 5)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped lines, got %v", lines)
	}
	lines = wrapDescription("single", 0)
	if len(lines) != 1 || lines[0] != "single" {
		t.Fatalf("unexpected wrap result: %v", lines)
	}
	lines = wrapDescription("", 10)
	if len(lines) != 1 || lines[0] != "" {
		t.Fatalf("unexpected wrap result for empty: %v", lines)
	}
}

func TestRemoveStaleSkillDirs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wanted := map[string]struct{}{
		"keep": {},
	}

	keepDir := filepath.Join(dir, "keep")
	staleDir := filepath.Join(dir, "stale")
	manualDir := filepath.Join(dir, "manual")
	ignoreFile := filepath.Join(dir, "ignore.txt")
	for _, d := range []string{keepDir, staleDir, manualDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(ignoreFile, []byte("ignore"), 0o644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keepDir, "SKILL.md"), []byte(generatedMarkerFixture), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "SKILL.md"), []byte(generatedMarkerFixture), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manualDir, "SKILL.md"), []byte("manual"), 0o644); err != nil {
		t.Fatalf("write manual: %v", err)
	}

	if err := removeStaleSkillDirs(RealSystem{}, dir, wanted); err != nil {
		t.Fatalf("removeStaleSkillDirs error: %v", err)
	}
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Fatalf("expected stale dir to be removed")
	}
	if _, err := os.Stat(manualDir); err != nil {
		t.Fatalf("expected manual dir to remain: %v", err)
	}
}

func TestRemoveStaleSkillDirsMissingDir(t *testing.T) {
	t.Parallel()
	err := removeStaleSkillDirs(RealSystem{}, filepath.Join(t.TempDir(), "missing"), map[string]struct{}{})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestHasGeneratedMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.md")
	if err := os.WriteFile(path, []byte(generatedMarkerFixture), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	ok, err := hasGeneratedMarker(RealSystem{}, path)
	if err != nil || !ok {
		t.Fatalf("expected generated marker, got %v %v", ok, err)
	}
	missing, err := hasGeneratedMarker(RealSystem{}, filepath.Join(dir, "missing.md"))
	if err != nil || missing {
		t.Fatalf("expected missing to return false, got %v %v", missing, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err = hasGeneratedMarker(RealSystem{}, filepath.Join(dir, "dir"))
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
}
