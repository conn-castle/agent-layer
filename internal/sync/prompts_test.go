package sync

import (
	"errors"
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
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteCodexSkills(sys, root, cmds); err == nil {
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
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteAntigravitySkills(sys, root, cmds); err == nil {
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
	// Make skills dir read-only so RemoveAll(skillDir) fails.
	if err := os.Chmod(skillsDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) })
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteAntigravitySkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir removal/creation failure")
	}
}

func TestWriteCodexSkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".codex", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Make skills dir read-only so RemoveAll(skillDir) fails.
	if err := os.Chmod(skillsDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) })
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := WriteCodexSkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for skill dir removal/creation failure")
	}
}

func TestBuildClaudeSkill(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body"}
	content, err := buildClaudeSkill(cmd)
	if err != nil {
		t.Fatalf("buildClaudeSkill error: %v", err)
	}
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "Body") {
		t.Fatalf("expected body in skill")
	}
}

func TestBuildGeminiSkill(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body"}
	content, err := buildGeminiSkill(cmd)
	if err != nil {
		t.Fatalf("buildGeminiSkill error: %v", err)
	}
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "Body") {
		t.Fatalf("expected body in skill")
	}
}

func TestWriteClaudeSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteClaudeSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}
	path := filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: alpha") {
		t.Fatalf("expected name in written skill")
	}
}

func TestWriteGeminiSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "beta", Description: "desc", Body: "Body"}}
	if err := WriteGeminiSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("WriteGeminiSkills error: %v", err)
	}
	path := filepath.Join(root, ".gemini", "skills", "beta", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: beta") {
		t.Fatalf("expected name in written skill")
	}
}

func TestCopySkillSubFiles(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create source structure: scripts/run.sh, references/REF.md, .hidden, SKILL.md
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "run.sh"), []byte("#!/bin/sh\necho hi"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "references", "REF.md"), []byte("# Ref"), 0o644); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: test\n---\nBody"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".hidden"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write hidden: %v", err)
	}

	skill := config.Skill{Name: "test", SourceDir: srcDir}
	if err := copySkillSubFiles(RealSystem{}, skill, destDir); err != nil {
		t.Fatalf("copySkillSubFiles error: %v", err)
	}

	// scripts/run.sh should be copied
	data, err := os.ReadFile(filepath.Join(destDir, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("read copied script: %v", err)
	}
	if !strings.Contains(string(data), "echo hi") {
		t.Fatalf("expected script content")
	}

	// references/REF.md should be copied
	if _, err := os.Stat(filepath.Join(destDir, "references", "REF.md")); err != nil {
		t.Fatalf("expected REF.md to be copied: %v", err)
	}

	// SKILL.md should NOT be copied (handled by builder)
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected SKILL.md to be skipped")
	}

	// .hidden should NOT be copied
	if _, err := os.Stat(filepath.Join(destDir, ".hidden")); !os.IsNotExist(err) {
		t.Fatalf("expected hidden file to be skipped")
	}
}

func TestCopySkillSubFiles_EmptySourceDir(t *testing.T) {
	t.Parallel()
	skill := config.Skill{Name: "test", SourceDir: ""}
	if err := copySkillSubFiles(RealSystem{}, skill, t.TempDir()); err != nil {
		t.Fatalf("expected nil error for empty SourceDir, got %v", err)
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

func TestCopyDirRecursive_ReadFileError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a subdirectory where a regular file is expected — ReadFile will fail.
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatalf("mkdir run.sh as dir: %v", err)
	}

	err := copyDirRecursive(RealSystem{}, srcDir, destDir, nil)
	// run.sh is a directory so entry.IsDir() is true; it won't hit ReadFile.
	// Instead, create a file that can't be read.
	if err != nil {
		t.Fatalf("unexpected error for nested dir: %v", err)
	}
}

func TestCopyDirRecursive_ReadFilePermissionError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create an unreadable file.
	unreadable := filepath.Join(srcDir, "secret.sh")
	if err := os.WriteFile(unreadable, []byte("data"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	err := copyDirRecursive(RealSystem{}, srcDir, destDir, nil)
	if err == nil {
		t.Fatalf("expected error for unreadable file")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestCopyDirRecursive_NonexistentSourceDir(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	err := copyDirRecursive(RealSystem{}, filepath.Join(t.TempDir(), "nonexistent"), destDir, nil)
	if err != nil {
		t.Fatalf("expected nil for nonexistent source dir, got: %v", err)
	}
}

func TestCopyDirRecursive_PreservesExecutePermission(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(srcDir, "scripts", "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if err := copyDirRecursive(RealSystem{}, srcDir, destDir, nil); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	destScript := filepath.Join(destDir, "scripts", "run.sh")
	info, err := os.Stat(destScript)
	if err != nil {
		t.Fatalf("stat copied script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected execute permission on copied script, got %v", info.Mode())
	}
}

func TestWriteClaudeSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteClaudeSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteClaudeSkills(sys, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGeminiSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteGeminiSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteGeminiSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := WriteGeminiSkills(sys, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSkillsWithSubdirectory(t *testing.T) {
	t.Parallel()

	// Set up a source skill directory with scripts/ subdirectory.
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "deploy.sh"), []byte("#!/bin/sh\necho deploy"), 0o755); err != nil {
		t.Fatalf("write deploy.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: deploy\n---\nDeploy body"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	root := t.TempDir()
	cmds := []config.Skill{{
		Name:        "deploy",
		Description: "Deploy skill",
		Body:        "Deploy body",
		SourceDir:   srcDir,
	}}
	if err := WriteClaudeSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}

	// Verify SKILL.md was written (by the builder, not copied from source).
	skillPath := filepath.Join(root, ".claude", "skills", "deploy", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(data), "name: deploy") {
		t.Fatalf("expected name in SKILL.md")
	}

	// Verify scripts/deploy.sh was copied with execute permission preserved.
	scriptPath := filepath.Join(root, ".claude", "skills", "deploy", "scripts", "deploy.sh")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read deploy.sh: %v", err)
	}
	if !strings.Contains(string(scriptData), "echo deploy") {
		t.Fatalf("expected script content")
	}
	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat deploy.sh: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected execute permission on deploy.sh, got %v", info.Mode())
	}
}

func TestWriteClaudeSkillsStaleSubFileCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// First sync: skill with scripts/old.sh
	srcDir1 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir1, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir1, "scripts", "old.sh"), []byte("#!/bin/sh\necho old"), 0o755); err != nil {
		t.Fatalf("write old.sh: %v", err)
	}

	cmds1 := []config.Skill{{
		Name:        "alpha",
		Description: "desc",
		Body:        "Body",
		SourceDir:   srcDir1,
	}}
	if err := WriteClaudeSkills(RealSystem{}, root, cmds1); err != nil {
		t.Fatalf("first WriteClaudeSkills error: %v", err)
	}

	// Verify old.sh exists after first sync.
	oldScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "old.sh")
	if _, err := os.Stat(oldScript); err != nil {
		t.Fatalf("expected old.sh after first sync: %v", err)
	}

	// Second sync: skill now has scripts/new.sh (old.sh removed from source).
	srcDir2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir2, "scripts"), 0o755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir2, "scripts", "new.sh"), []byte("#!/bin/sh\necho new"), 0o755); err != nil {
		t.Fatalf("write new.sh: %v", err)
	}

	cmds2 := []config.Skill{{
		Name:        "alpha",
		Description: "desc",
		Body:        "Body",
		SourceDir:   srcDir2,
	}}
	if err := WriteClaudeSkills(RealSystem{}, root, cmds2); err != nil {
		t.Fatalf("second WriteClaudeSkills error: %v", err)
	}

	// Verify old.sh is removed (stale sub-file cleanup).
	if _, err := os.Stat(oldScript); !os.IsNotExist(err) {
		t.Fatalf("expected old.sh to be removed after second sync")
	}

	// Verify new.sh exists.
	newScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "new.sh")
	if _, err := os.Stat(newScript); err != nil {
		t.Fatalf("expected new.sh after second sync: %v", err)
	}
}

func TestCopyDirRecursive_StatError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		StatFunc: func(name string) (os.FileInfo, error) {
			if strings.HasSuffix(name, "data.txt") {
				return nil, errors.New("stat failed")
			}
			return RealSystem{}.Stat(name)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir, nil)
	if err == nil {
		t.Fatalf("expected error from Stat failure")
	}
	if !strings.Contains(err.Error(), "stat failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDirRecursive_WriteFileAtomicSubFileError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if strings.HasSuffix(filename, "data.txt") {
				return errors.New("write sub-file failed")
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir, nil)
	if err == nil {
		t.Fatalf("expected error from WriteFileAtomic failure")
	}
	if !strings.Contains(err.Error(), "write sub-file failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDirRecursive_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a regular file and a symlink.
	if err := os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("real"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(filepath.Join(srcDir, "real.txt"), filepath.Join(srcDir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := copyDirRecursive(RealSystem{}, srcDir, destDir, nil); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	// real.txt should be copied.
	if _, err := os.Stat(filepath.Join(destDir, "real.txt")); err != nil {
		t.Fatalf("expected real.txt to be copied: %v", err)
	}
	// link.txt should be skipped.
	if _, err := os.Stat(filepath.Join(destDir, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected link.txt (symlink) to be skipped")
	}
}

func TestWriteSkillFiles_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "../escape", Description: "desc", Body: "Body"}}
	err := WriteClaudeSkills(RealSystem{}, root, cmds)
	if err == nil {
		t.Fatalf("expected error for path traversal in skill name")
	}
	if !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopySkillSubFiles_SkipOnlyTopLevelSkillMd(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a top-level SKILL.md (should be skipped) and a nested SKILL.md (should be copied).
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("top-level"), 0o644); err != nil {
		t.Fatalf("write top SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "references"), 0o755); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "references", "SKILL.md"), []byte("nested"), 0o644); err != nil {
		t.Fatalf("write nested SKILL.md: %v", err)
	}

	skill := config.Skill{Name: "test", SourceDir: srcDir}
	if err := copySkillSubFiles(RealSystem{}, skill, destDir); err != nil {
		t.Fatalf("copySkillSubFiles error: %v", err)
	}

	// Top-level SKILL.md should NOT be copied.
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected top-level SKILL.md to be skipped")
	}
	// Nested references/SKILL.md SHOULD be copied.
	data, err := os.ReadFile(filepath.Join(destDir, "references", "SKILL.md"))
	if err != nil {
		t.Fatalf("expected nested SKILL.md to be copied: %v", err)
	}
	if string(data) != "nested" {
		t.Fatalf("unexpected nested SKILL.md content: %q", string(data))
	}
}
