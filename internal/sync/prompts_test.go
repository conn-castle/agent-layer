package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

const generatedMarkerFixture = "<!--\n  GENERATED FILE\n  Source: .agent-layer/skills/test.md\n  Regenerate: al sync\n-->\n"

func TestBuildVSCodePrompt(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Body: "Body"}
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
	cmds := []config.Skill{{Name: "alpha", Body: "Body"}}
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
	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body"}
	content, err := buildCodexSkill(cmd)
	if err != nil {
		t.Fatalf("buildCodexSkill error: %v", err)
	}
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "description: >-") {
		t.Fatalf("expected folded description in frontmatter")
	}
	if !strings.Contains(content, "# alpha") {
		t.Fatalf("expected heading in skill")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestBuildAntigravitySkill(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body"}
	content, err := buildAntigravitySkill(cmd)
	if err != nil {
		t.Fatalf("buildAntigravitySkill error: %v", err)
	}
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

func TestBuildSkillFrontMatter_FieldOrderAndMetadataSorting(t *testing.T) {
	cmd := config.Skill{
		Name:          "alpha",
		Description:   "desc",
		License:       "MIT",
		Compatibility: "requires git",
		Metadata: map[string]string{
			"z-key": "last",
			"a-key": "first",
		},
		AllowedTools: "Bash(git:*) Read",
	}

	content, err := buildAntigravitySkill(cmd)
	if err != nil {
		t.Fatalf("buildAntigravitySkill error: %v", err)
	}

	expectedOrder := []string{
		"name:",
		"description:",
		"license:",
		"compatibility:",
		"metadata:",
		"allowed-tools:",
	}
	last := -1
	for _, token := range expectedOrder {
		idx := strings.Index(content, token)
		if idx == -1 {
			t.Fatalf("expected token %q in output", token)
		}
		if idx < last {
			t.Fatalf("expected token %q after previous token, got output:\n%s", token, content)
		}
		last = idx
	}

	idxA := strings.Index(content, "a-key:")
	idxZ := strings.Index(content, "z-key:")
	if idxA == -1 || idxZ == -1 {
		t.Fatalf("expected metadata keys in output, got:\n%s", content)
	}
	if idxA > idxZ {
		t.Fatalf("expected sorted metadata keys, got:\n%s", content)
	}
}

func TestBuildSkillFrontMatter_OmitsEmptyOptionalFields(t *testing.T) {
	cmd := config.Skill{
		Name:          "alpha",
		Description:   "desc",
		License:       "",
		Compatibility: "  ",
		Metadata:      map[string]string{},
		AllowedTools:  "",
	}

	content, err := buildCodexSkill(cmd)
	if err != nil {
		t.Fatalf("buildCodexSkill error: %v", err)
	}
	if strings.Contains(content, "license:") {
		t.Fatalf("did not expect license field in output:\n%s", content)
	}
	if strings.Contains(content, "compatibility:") {
		t.Fatalf("did not expect compatibility field in output:\n%s", content)
	}
	if strings.Contains(content, "metadata:") {
		t.Fatalf("did not expect metadata field in output:\n%s", content)
	}
	if strings.Contains(content, "allowed-tools:") {
		t.Fatalf("did not expect allowed-tools field in output:\n%s", content)
	}
}

func TestBuildSkillFrontMatter_UsesLiteralDescriptionForMultiline(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Description: "line1\nline2"}
	content, err := buildAntigravitySkill(cmd)
	if err != nil {
		t.Fatalf("buildAntigravitySkill error: %v", err)
	}
	if !strings.Contains(content, "description: |-") {
		t.Fatalf("expected literal description style for multiline description, got:\n%s", content)
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
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
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
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
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
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
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
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteCodexSkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir creation failure")
	}
	if !strings.Contains(err.Error(), "failed to create") {
		t.Fatalf("expected mkdir error, got %v", err)
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

func TestGeneratedSkillSourcePath(t *testing.T) {
	cmd := config.Skill{Name: "alpha"}
	if got := generatedSkillSourcePath(cmd); got != ".agent-layer/skills/alpha/SKILL.md" {
		t.Fatalf("unexpected default source path: %q", got)
	}

	cmd.SourcePath = filepath.Join("/tmp/repo", ".agent-layer", "skills", "alpha", "SKILL.md")
	if got := generatedSkillSourcePath(cmd); got != ".agent-layer/skills/alpha/SKILL.md" {
		t.Fatalf("unexpected normalized source path: %q", got)
	}
}
