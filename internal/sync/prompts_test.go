package sync

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

const generatedMarkerFixture = "<!--\n  GENERATED FILE\n  Source: .agent-layer/skills/test.md\n  Regenerate: al sync\n-->\n"

type unknownTypeDirEntry struct {
	name string
}

func (e unknownTypeDirEntry) Name() string    { return e.name }
func (unknownTypeDirEntry) IsDir() bool       { return false }
func (unknownTypeDirEntry) Type() fs.FileMode { return 0 }
func (unknownTypeDirEntry) Info() (fs.FileInfo, error) {
	return nil, errors.New("directory entry info should not be used")
}

func TestBuildAgentSkill(t *testing.T) {
	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body"}
	content, err := buildAgentSkill(cmd)
	if err != nil {
		t.Fatalf("buildAgentSkill error: %v", err)
	}
	if !strings.Contains(content, "name: alpha") {
		t.Fatalf("expected name in skill")
	}
	if !strings.Contains(content, "description: >-") {
		t.Fatalf("expected folded description in frontmatter")
	}
	if !strings.Contains(content, "Body") {
		t.Fatalf("expected body in skill")
	}
	if !strings.HasSuffix(content, "\n") {
		t.Fatalf("expected trailing newline")
	}
	// The agentskills.io format relies on the `name:` front-matter field; the
	// builder must not also inject a `# <name>` heading into the body.
	if strings.Contains(content, "# alpha") {
		t.Fatalf("did not expect injected name heading in body, got:\n%s", content)
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

	content, err := buildAgentSkill(cmd)
	if err != nil {
		t.Fatalf("buildAgentSkill error: %v", err)
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

	content, err := buildAgentSkill(cmd)
	if err != nil {
		t.Fatalf("buildAgentSkill error: %v", err)
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
	content, err := buildAgentSkill(cmd)
	if err != nil {
		t.Fatalf("buildAgentSkill error: %v", err)
	}
	if !strings.Contains(content, "description: |-") {
		t.Fatalf("expected literal description style for multiline description, got:\n%s", content)
	}
}

func TestWriteAgentSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := writeAgentSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAgentSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := writeAgentSkills(sys, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAgentSkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Make skills dir read-only so creating the skill directory fails.
	if err := os.Chmod(skillsDir, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	err := writeAgentSkills(RealSystem{}, root, cmds)
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

func TestWriteClaudeSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := writeClaudeSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("writeClaudeSkills error: %v", err)
	}
	path := filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: alpha") {
		t.Fatalf("expected name in written skill")
	}
}

func TestWriteAgentSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "beta", Description: "desc", Body: "Body"}}
	if err := writeAgentSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("writeAgentSkills error: %v", err)
	}
	path := filepath.Join(root, ".agents", "skills", "beta", "SKILL.md")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(data), "name: beta") {
		t.Fatalf("expected name in written skill")
	}
}

func TestWriteAgentSkillsRefreshKeepsSkillReadable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := writeAgentSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("initial writeAgentSkills error: %v", err)
	}

	skillDir := filepath.Join(root, ".agents", "skills", "alpha")
	skillPath := filepath.Join(skillDir, "SKILL.md")
	var readerErr error
	observeRemoval := func(path string) {
		relativeSkillPath, err := filepath.Rel(path, skillPath)
		if err != nil || relativeSkillPath == ".." || strings.HasPrefix(relativeSkillPath, ".."+string(filepath.Separator)) {
			return
		}
		_, readerErr = os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	}
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveFunc: func(path string) error {
			if err := (RealSystem{}).Remove(path); err != nil {
				return err
			}
			observeRemoval(path)
			return nil
		},
		RemoveAllFunc: func(path string) error {
			if err := (RealSystem{}).RemoveAll(path); err != nil {
				return err
			}
			observeRemoval(path)
			return nil
		},
	}

	if err := writeAgentSkills(sys, root, cmds); err != nil {
		t.Fatalf("refresh writeAgentSkills error: %v", err)
	}
	if readerErr != nil {
		t.Fatalf("existing SKILL.md became unreadable during refresh: %v", readerErr)
	}
}

func TestWriteAgentSkillsReplacesPreexistingSkillDirectorySymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	externalDir := t.TempDir()
	externalSkillDir := filepath.Join(externalDir, "alpha")
	if err := os.MkdirAll(externalSkillDir, 0o700); err != nil {
		t.Fatalf("mkdir external skill: %v", err)
	}
	externalSkillPath := filepath.Join(externalSkillDir, "SKILL.md")
	if err := os.WriteFile(externalSkillPath, []byte("external content"), 0o600); err != nil {
		t.Fatalf("write external skill: %v", err)
	}

	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil { // #nosec G301 -- the fixture uses the same managed-directory mode as production.
		t.Fatalf("mkdir skills: %v", err)
	}
	skillDir := filepath.Join(skillsDir, "alpha")
	if err := os.Symlink(externalSkillDir, skillDir); err != nil {
		t.Fatalf("symlink skill directory: %v", err)
	}

	cmds := []config.Skill{{Name: "alpha", Description: "desc", Body: "Body"}}
	if err := writeAgentSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("writeAgentSkills error: %v", err)
	}

	info, err := os.Lstat(skillDir)
	if err != nil {
		t.Fatalf("lstat generated skill directory: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("expected a real generated skill directory, got mode %v", info.Mode())
	}
	assertCanonicalSkillEntrypoint(t, root, filepath.Join(".agents", "skills"), "alpha")

	data, err := os.ReadFile(externalSkillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external skill: %v", err)
	}
	if string(data) != "external content" {
		t.Fatalf("external symlink target was modified: %q", data)
	}
}

func TestCopySkillSubFiles(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create source structure: scripts/run.sh, references/REF.md, .hidden, SKILL.md
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "run.sh"), []byte("#!/bin/sh\necho hi"), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "references"), 0o700); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "references", "REF.md"), []byte("# Ref"), 0o600); err != nil {
		t.Fatalf("write ref: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: test\n---\nBody"), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".hidden"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("write hidden: %v", err)
	}

	skill := config.Skill{Name: "test", SourceDir: srcDir}
	if err := copySkillSubFiles(RealSystem{}, skill, destDir); err != nil {
		t.Fatalf("copySkillSubFiles error: %v", err)
	}

	// scripts/run.sh should be copied
	data, err := os.ReadFile(filepath.Join(destDir, "scripts", "run.sh")) // #nosec G304 -- path is constructed from test-controlled inputs.
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
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if err := os.WriteFile(ignoreFile, []byte("ignore"), 0o600); err != nil {
		t.Fatalf("write ignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keepDir, "SKILL.md"), []byte(generatedMarkerFixture), 0o600); err != nil {
		t.Fatalf("write keep: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staleDir, "SKILL.md"), []byte(generatedMarkerFixture), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manualDir, "SKILL.md"), []byte("manual"), 0o600); err != nil {
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

func TestCleanSharedAgentSkillsRemovesGeneratedOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	generatedDir := filepath.Join(root, ".agents", "skills", "generated")
	manualDir := filepath.Join(root, ".agents", "skills", "manual")
	for _, dir := range []string{generatedDir, manualDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(generatedDir, "SKILL.md"), []byte(generatedMarkerFixture), 0o600); err != nil {
		t.Fatalf("write generated skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manualDir, "SKILL.md"), []byte("# manual\n"), 0o600); err != nil {
		t.Fatalf("write manual skill: %v", err)
	}

	if err := cleanSharedAgentSkills(RealSystem{}, root); err != nil {
		t.Fatalf("cleanSharedAgentSkills error: %v", err)
	}
	if _, err := os.Stat(generatedDir); !os.IsNotExist(err) {
		t.Fatalf("expected generated shared skill to be removed")
	}
	if _, err := os.Stat(manualDir); err != nil {
		t.Fatalf("expected manual shared skill to remain: %v", err)
	}
}

func TestCleanLegacySkillOutputsRemovesRetiredDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{
		filepath.Join(".codex", "skills", "alpha"),
		filepath.Join(".agent", "skills", "alpha"),
		filepath.Join(".gemini", "skills", "alpha"),
		filepath.Join(".github", "skills", "alpha"),
		filepath.Join(".vscode", "prompts"),
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	if err := cleanLegacySkillOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanLegacySkillOutputs error: %v", err)
	}

	for _, rel := range []string{
		filepath.Join(".codex", "skills"),
		filepath.Join(".agent", "skills"),
		filepath.Join(".gemini", "skills"),
		filepath.Join(".github", "skills"),
		filepath.Join(".vscode", "prompts"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed", rel)
		}
	}
}

func TestHasGeneratedMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.md")
	if err := os.WriteFile(path, []byte(generatedMarkerFixture), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Join(dir, "dir"), 0o700); err != nil {
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

func TestCopyDirRecursive_ReadFilePermissionError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create an unreadable file.
	unreadable := filepath.Join(srcDir, "secret.sh")
	if err := os.WriteFile(unreadable, []byte("data"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

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

	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(srcDir, "scripts", "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi"), 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
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
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := writeClaudeSkills(RealSystem{}, file, nil)
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
	if err := writeClaudeSkills(sys, root, cmds); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSkillsWithSubdirectory(t *testing.T) {
	t.Parallel()

	// Set up a source skill directory with scripts/ subdirectory.
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "deploy.sh"), []byte("#!/bin/sh\necho deploy"), 0o755); err != nil { // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.
		t.Fatalf("write deploy.sh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("---\nname: deploy\n---\nDeploy body"), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	root := t.TempDir()
	cmds := []config.Skill{{
		Name:        "deploy",
		Description: "Deploy skill",
		Body:        "Deploy body",
		SourceDir:   srcDir,
	}}
	if err := writeClaudeSkills(RealSystem{}, root, cmds); err != nil {
		t.Fatalf("writeClaudeSkills error: %v", err)
	}

	// Verify SKILL.md was written (by the builder, not copied from source).
	skillPath := filepath.Join(root, ".claude", "skills", "deploy", "SKILL.md")
	data, err := os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(data), "name: deploy") {
		t.Fatalf("expected name in SKILL.md")
	}

	// Verify scripts/deploy.sh was copied with execute permission preserved.
	scriptPath := filepath.Join(root, ".claude", "skills", "deploy", "scripts", "deploy.sh")
	scriptData, err := os.ReadFile(scriptPath) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.MkdirAll(filepath.Join(srcDir1, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir1, "scripts", "old.sh"), []byte("#!/bin/sh\necho old"), 0o755); err != nil { // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.
		t.Fatalf("write old.sh: %v", err)
	}

	cmds1 := []config.Skill{{
		Name:        "alpha",
		Description: "desc",
		Body:        "Body",
		SourceDir:   srcDir1,
	}}
	if err := writeClaudeSkills(RealSystem{}, root, cmds1); err != nil {
		t.Fatalf("first writeClaudeSkills error: %v", err)
	}

	// Verify old.sh exists after first sync.
	oldScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "old.sh")
	if _, err := os.Stat(oldScript); err != nil {
		t.Fatalf("expected old.sh after first sync: %v", err)
	}

	// Second sync: skill now has scripts/new.sh (old.sh removed from source).
	srcDir2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir2, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir2, "scripts", "new.sh"), []byte("#!/bin/sh\necho new"), 0o755); err != nil { // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.
		t.Fatalf("write new.sh: %v", err)
	}

	cmds2 := []config.Skill{{
		Name:        "alpha",
		Description: "desc",
		Body:        "Body",
		SourceDir:   srcDir2,
	}}
	if err := writeClaudeSkills(RealSystem{}, root, cmds2); err != nil {
		t.Fatalf("second writeClaudeSkills error: %v", err)
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

func TestWriteClaudeSkillsReconcilesResourceTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()
	keepScript := filepath.Join(srcDir, "scripts", "keep.sh")
	staleFile := filepath.Join(srcDir, "references", "obsolete", "nested.txt")
	if err := os.MkdirAll(filepath.Dir(keepScript), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(keepScript, []byte("#!/bin/sh\necho keep"), 0o755); err != nil { // #nosec G306 -- execute permission is the behavior under test.
		t.Fatalf("write keep script: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o700); err != nil {
		t.Fatalf("mkdir stale references: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skill.md"), []byte("source-owned lowercase entrypoint"), 0o600); err != nil {
		t.Fatalf("write ignored lowercase entrypoint: %v", err)
	}

	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body", SourceDir: srcDir}
	refresh := func(sourceDir string) {
		t.Helper()
		cmd.SourceDir = sourceDir
		if err := writeClaudeSkills(RealSystem{}, root, []config.Skill{cmd}); err != nil {
			t.Fatalf("writeClaudeSkills refresh error: %v", err)
		}
		assertCanonicalSkillEntrypoint(t, root, filepath.Join(".claude", "skills"), cmd.Name)
	}

	refresh(srcDir)
	refresh(srcDir)
	destScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "keep.sh")
	data, err := os.ReadFile(destScript) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil || !strings.Contains(string(data), "echo keep") {
		t.Fatalf("unchanged desired resource did not survive refresh: data=%q err=%v", data, err)
	}
	info, err := os.Stat(destScript)
	if err != nil {
		t.Fatalf("stat refreshed executable: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected execute permission after refresh, got %v", info.Mode())
	}

	if err := os.RemoveAll(filepath.Join(srcDir, "references")); err != nil {
		t.Fatalf("remove source references: %v", err)
	}
	refresh(srcDir)
	destReferences := filepath.Join(root, ".claude", "skills", "alpha", "references")
	if _, err := os.Stat(destReferences); !os.IsNotExist(err) {
		t.Fatalf("expected whole stale resource directory to be removed, got %v", err)
	}

	refresh("")
	destScripts := filepath.Join(root, ".claude", "skills", "alpha", "scripts")
	if _, err := os.Stat(destScripts); !os.IsNotExist(err) {
		t.Fatalf("expected empty SourceDir to remove resources, got %v", err)
	}

	refresh(srcDir)
	refresh(filepath.Join(t.TempDir(), "missing"))
	if _, err := os.Stat(destScripts); !os.IsNotExist(err) {
		t.Fatalf("expected nonexistent SourceDir to remove resources, got %v", err)
	}
}

func TestWriteClaudeSkillsReconcilesResourceTypeTransitions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()
	sourceResource := filepath.Join(srcDir, "resource")
	if err := os.WriteFile(sourceResource, []byte("file first"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	cmd := config.Skill{Name: "alpha", Description: "desc", Body: "Body", SourceDir: srcDir}
	refresh := func() {
		t.Helper()
		if err := writeClaudeSkills(RealSystem{}, root, []config.Skill{cmd}); err != nil {
			t.Fatalf("writeClaudeSkills refresh error: %v", err)
		}
		assertCanonicalSkillEntrypoint(t, root, filepath.Join(".claude", "skills"), cmd.Name)
	}
	destResource := filepath.Join(root, ".claude", "skills", "alpha", "resource")

	refresh()
	if err := os.Remove(sourceResource); err != nil {
		t.Fatalf("remove source file: %v", err)
	}
	if err := os.MkdirAll(sourceResource, 0o700); err != nil {
		t.Fatalf("mkdir source resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceResource, "nested.txt"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("write nested source file: %v", err)
	}
	refresh()
	if data, err := os.ReadFile(filepath.Join(destResource, "nested.txt")); err != nil || string(data) != "nested" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("file-to-directory transition failed: data=%q err=%v", data, err)
	}

	if err := os.RemoveAll(sourceResource); err != nil {
		t.Fatalf("remove source resource directory: %v", err)
	}
	if err := os.WriteFile(sourceResource, []byte("file again"), 0o600); err != nil {
		t.Fatalf("write replacement source file: %v", err)
	}
	refresh()
	if data, err := os.ReadFile(destResource); err != nil || string(data) != "file again" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("non-empty-directory-to-file transition failed: data=%q err=%v", data, err)
	}
}

func assertCanonicalSkillEntrypoint(t *testing.T, root string, skillsPath string, skillName string) {
	t.Helper()
	path := filepath.Join(root, skillsPath, skillName, "SKILL.md")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("canonical SKILL.md is unreadable after refresh: %v", err)
	}
	if !strings.Contains(string(data), "name: "+skillName) {
		t.Fatalf("canonical SKILL.md has unexpected content: %q", data)
	}
}

func TestCopyDirRecursive_LstatError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if strings.HasSuffix(name, "data.txt") {
				return nil, errors.New("lstat failed")
			}
			return RealSystem{}.Lstat(name)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir, nil)
	if err == nil {
		t.Fatalf("expected error from Lstat failure")
	}
	if !strings.Contains(err.Error(), "lstat failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDirRecursive_DestinationLstatError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(string) (os.FileInfo, error) {
			return nil, errors.New("destination lstat failed")
		},
	}
	err := copyDirRecursive(sys, srcDir, destDir, nil)
	if err == nil || !strings.Contains(err.Error(), "destination lstat failed") {
		t.Fatalf("expected actionable destination lstat error, got %v", err)
	}
}

func TestCopyDirRecursive_ResourceConflictErrors(t *testing.T) {
	t.Parallel()

	t.Run("remove conflicting file", func(t *testing.T) {
		srcDir := t.TempDir()
		destDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(srcDir, "resource"), 0o700); err != nil {
			t.Fatalf("mkdir source resource: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destDir, "resource"), []byte("old"), 0o600); err != nil {
			t.Fatalf("write destination resource: %v", err)
		}

		sys := &MockSystem{
			Fallback: RealSystem{},
			RemoveFunc: func(string) error {
				return errors.New("conflict removal failed")
			},
		}
		err := copyDirRecursive(sys, srcDir, destDir, nil)
		if err == nil || !strings.Contains(err.Error(), "conflict removal failed") {
			t.Fatalf("expected actionable conflict removal error, got %v", err)
		}
	})

	t.Run("create desired directory", func(t *testing.T) {
		srcDir := t.TempDir()
		destDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(srcDir, "resource"), 0o700); err != nil {
			t.Fatalf("mkdir source resource: %v", err)
		}

		sys := &MockSystem{
			Fallback: RealSystem{},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				if filepath.Base(path) == "resource" {
					return errors.New("desired directory creation failed")
				}
				return RealSystem{}.MkdirAll(path, perm)
			},
		}
		err := copyDirRecursive(sys, srcDir, destDir, nil)
		if err == nil || !strings.Contains(err.Error(), "desired directory creation failed") {
			t.Fatalf("expected actionable desired directory error, got %v", err)
		}
	})
}

func TestCopyDirRecursive_StaleCleanupErrors(t *testing.T) {
	t.Parallel()

	t.Run("read destination", func(t *testing.T) {
		destDir := t.TempDir()
		sys := &MockSystem{
			Fallback: RealSystem{},
			ReadDirFunc: func(path string) ([]os.DirEntry, error) {
				if path == destDir {
					return nil, errors.New("destination read failed")
				}
				return RealSystem{}.ReadDir(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir, nil)
		if err == nil || !strings.Contains(err.Error(), "destination read failed") {
			t.Fatalf("expected actionable destination read error, got %v", err)
		}
	})

	t.Run("inspect stale node", func(t *testing.T) {
		destDir := t.TempDir()
		stalePath := filepath.Join(destDir, "stale.txt")
		if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale resource: %v", err)
		}
		sys := &MockSystem{
			Fallback: RealSystem{},
			LstatFunc: func(path string) (os.FileInfo, error) {
				if path == stalePath {
					return nil, errors.New("stale node lstat failed")
				}
				return RealSystem{}.Lstat(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir, nil)
		if err == nil || !strings.Contains(err.Error(), "stale node lstat failed") {
			t.Fatalf("expected actionable stale-node lstat error, got %v", err)
		}
	})

	t.Run("remove stale node", func(t *testing.T) {
		destDir := t.TempDir()
		stalePath := filepath.Join(destDir, "stale.txt")
		if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale resource: %v", err)
		}
		sys := &MockSystem{
			Fallback: RealSystem{},
			RemoveFunc: func(path string) error {
				if path == stalePath {
					return errors.New("stale node removal failed")
				}
				return RealSystem{}.Remove(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir, nil)
		if err == nil || !strings.Contains(err.Error(), "stale node removal failed") {
			t.Fatalf("expected actionable stale-node removal error, got %v", err)
		}
	})
}

func TestCopyDirRecursive_WriteFileAtomicSubFileError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("real"), 0o600); err != nil {
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

func TestCopyDirRecursive_UsesLstatForSourceSymlinkDetection(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	realPath := filepath.Join(srcDir, "real.txt")
	linkPath := filepath.Join(srcDir, "link.txt")
	externalPath := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(realPath, []byte("real"), 0o600); err != nil {
		t.Fatalf("write real source file: %v", err)
	}
	if err := os.WriteFile(externalPath, []byte("external target"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Symlink(externalPath, linkPath); err != nil {
		t.Fatalf("symlink source file: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			if path == srcDir {
				return []os.DirEntry{
					unknownTypeDirEntry{name: "real.txt"},
					unknownTypeDirEntry{name: "link.txt"},
				}, nil
			}
			return RealSystem{}.ReadDir(path)
		},
	}

	if err := copyDirRecursive(sys, srcDir, destDir, nil); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "real.txt")); err != nil || string(data) != "real" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("real source file was not copied: data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(destDir, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected source symlink to be skipped, got %v", err)
	}
	data, err := os.ReadFile(externalPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external target: %v", err)
	}
	if string(data) != "external target" {
		t.Fatalf("external source symlink target changed: %q", data)
	}
}

func TestCopyDirRecursive_NestedSourceReadError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	nestedDir := filepath.Join(srcDir, "scripts")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("mkdir nested source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "run.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write nested source file: %v", err)
	}

	// Fail ReadDir of the NESTED source subdirectory only, so the top-level
	// collect succeeds and recurses. This exercises both the source-side ReadDir
	// error return and the collection recursion error propagation in one test.
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			if path == nestedDir {
				return nil, errors.New("nested source read failed")
			}
			return RealSystem{}.ReadDir(path)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir, nil)
	if err == nil || !strings.Contains(err.Error(), "nested source read failed") {
		t.Fatalf("expected actionable nested source read error, got %v", err)
	}
}

func TestCopyDirRecursive_DestinationSymlinkStaleRemoval(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	externalTarget := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(externalTarget, []byte("external target"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	symlinkPath := filepath.Join(destDir, "link.txt")
	if err := os.Symlink(externalTarget, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Empty srcDir means nothing is desired, so the destination symlink is stale.
	if err := copyDirRecursive(RealSystem{}, "", destDir, nil); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	// The stale symlink itself must be unlinked...
	if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale destination symlink to be removed, got %v", err)
	}
	// ...without being followed: the external target must survive with intact content.
	data, err := os.ReadFile(externalTarget) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("external symlink target was not preserved: %v", err)
	}
	if string(data) != "external target" {
		t.Fatalf("external target content changed: %q", data)
	}
}

func TestCopyDirRecursive_PreservesCanonicalSkillFileCase(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	canonicalPath := filepath.Join(destDir, "skill.md")
	if err := os.WriteFile(canonicalPath, []byte("generated content"), 0o600); err != nil {
		t.Fatalf("write lowercase canonical skill file: %v", err)
	}

	if err := copyDirRecursive(RealSystem{}, "", destDir, map[string]struct{}{"SKILL.md": {}}); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	data, err := os.ReadFile(canonicalPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read lowercase canonical skill file: %v", err)
	}
	if string(data) != "generated content" {
		t.Fatalf("canonical skill file changed: %q", data)
	}
}

func TestCopyDirRecursive_RemovesDestinationSymlinkBeforeWriting(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	sourcePath := filepath.Join(srcDir, "resource.txt")
	destPath := filepath.Join(destDir, "resource.txt")
	externalPath := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(sourcePath, []byte("source content"), 0o600); err != nil {
		t.Fatalf("write source resource: %v", err)
	}
	if err := os.WriteFile(externalPath, []byte("external content"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Symlink(externalPath, destPath); err != nil {
		t.Fatalf("symlink destination resource: %v", err)
	}

	var removedPath string
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveFunc: func(path string) error {
			if path == destPath {
				removedPath = path
			}
			return RealSystem{}.Remove(path)
		},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if filename == destPath {
				info, err := os.Lstat(filename)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if err == nil && info.Mode()&os.ModeSymlink != 0 {
					return errors.New("refused to write over destination symlink")
				}
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	if err := copyDirRecursive(sys, srcDir, destDir, nil); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}
	if removedPath != destPath {
		t.Fatalf("expected destination symlink to be removed before writing, got %q", removedPath)
	}
	info, err := os.Lstat(destPath)
	if err != nil {
		t.Fatalf("lstat destination resource: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("destination resource remained a symlink")
	}
	data, err := os.ReadFile(destPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read destination resource: %v", err)
	}
	if string(data) != "source content" {
		t.Fatalf("unexpected destination content: %q", data)
	}
	data, err = os.ReadFile(externalPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external target: %v", err)
	}
	if string(data) != "external content" {
		t.Fatalf("external symlink target changed: %q", data)
	}
}

func TestWriteSkillFiles_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmds := []config.Skill{{Name: "../escape", Description: "desc", Body: "Body"}}
	err := writeClaudeSkills(RealSystem{}, root, cmds)
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
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("top-level"), 0o600); err != nil {
		t.Fatalf("write top SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "references"), 0o700); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "references", "SKILL.md"), []byte("nested"), 0o600); err != nil {
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
	data, err := os.ReadFile(filepath.Join(destDir, "references", "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("expected nested SKILL.md to be copied: %v", err)
	}
	if string(data) != "nested" {
		t.Fatalf("unexpected nested SKILL.md content: %q", string(data))
	}
}
