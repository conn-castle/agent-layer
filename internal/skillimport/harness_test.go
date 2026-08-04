package skillimport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// baseConfigTOML is a minimally valid Agent Layer configuration. Import blocks
// are appended to it by the test helpers.
const baseConfigTOML = `[approvals]
mode = "none"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false
`

// gitRepo is a hermetic local Git repository used as an import source or push
// destination. No network or credential is involved.
type gitRepo struct {
	t   *testing.T
	dir string
}

// newGitRepo creates an initialized repository with an initial commit on the
// named default branch.
func newGitRepo(t *testing.T, defaultBranch string) *gitRepo {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	repo := &gitRepo{t: t, dir: dir}
	repo.run("init", "--quiet", "--initial-branch="+defaultBranch)
	repo.run("config", "user.name", "Agent Layer Test")
	repo.run("config", "user.email", "test@example.invalid")
	// Local push destinations must accept updates to their checked-out branch.
	repo.run("config", "receive.denyCurrentBranch", "updateInstead")
	repo.WriteFile("README.md", "seed\n", 0o644)
	repo.Commit("seed")
	return repo
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is required for skill import tests: %v", err)
	}
}

// URL returns the repository reference used in configuration.
func (r *gitRepo) URL() string { return r.dir }

func (r *gitRepo) run(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

// WriteFile writes a file into the repository working tree.
func (r *gitRepo) WriteFile(relative string, content string, mode os.FileMode) {
	r.t.Helper()
	path := filepath.Join(r.dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil { // #nosec G306 -- mode is part of the fixture contract.
		r.t.Fatalf("write %s: %v", relative, err)
	}
}

// RemovePath deletes a path from the repository working tree.
func (r *gitRepo) RemovePath(relative string) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.dir, filepath.FromSlash(relative))); err != nil {
		r.t.Fatalf("remove %s: %v", relative, err)
	}
}

// WriteSkill writes a minimal valid skill at a repository path.
func (r *gitRepo) WriteSkill(path string, name string, body string) {
	r.t.Helper()
	r.WriteFile(path+"/SKILL.md", skillManifest(name, body), 0o644)
}

func skillManifest(name string, body string) string {
	return "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n" + body + "\n"
}

// Commit stages everything and records a commit, returning its object id.
func (r *gitRepo) Commit(message string) string {
	r.t.Helper()
	r.run("add", "--all")
	r.run("commit", "--quiet", "--allow-empty", "--message", message)
	return r.run("rev-parse", "HEAD")
}

// Tag creates a lightweight tag at HEAD.
func (r *gitRepo) Tag(name string) { r.t.Helper(); r.run("tag", name) }

// Checkout switches to a branch, creating it when create is true.
func (r *gitRepo) Checkout(branch string, create bool) {
	r.t.Helper()
	if create {
		r.run("checkout", "--quiet", "-b", branch)
		return
	}
	r.run("checkout", "--quiet", branch)
}

// Head returns the commit id of a branch.
func (r *gitRepo) Head(branch string) string {
	r.t.Helper()
	return r.run("rev-parse", branch)
}

// FileAt returns a file's content at a ref.
func (r *gitRepo) FileAt(ref string, path string) string {
	r.t.Helper()
	return r.run("show", ref+":"+path)
}

// HasPath reports whether a path exists at a ref.
func (r *gitRepo) HasPath(ref string, path string) bool {
	r.t.Helper()
	cmd := exec.Command("git", "cat-file", "-e", ref+":"+path) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = r.dir
	return cmd.Run() == nil
}

// project is a hermetic Agent Layer repository root used by import tests.
type project struct {
	t     *testing.T
	root  string
	paths config.Paths
}

// newProject creates a repository root with valid Agent Layer scaffolding.
func newProject(t *testing.T) *project {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	agentLayer := filepath.Join(root, ".agent-layer")
	for _, dir := range []string{agentLayer, filepath.Join(agentLayer, "skills"), filepath.Join(agentLayer, "instructions")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeProjectFile(t, filepath.Join(agentLayer, "config.toml"), baseConfigTOML)
	writeProjectFile(t, filepath.Join(agentLayer, ".env"), "")
	writeProjectFile(t, filepath.Join(agentLayer, "commands.allow"), "")
	writeProjectFile(t, filepath.Join(agentLayer, "gitignore.block"), "/.agent-layer/tmp/\n")
	writeProjectFile(t, filepath.Join(agentLayer, "instructions", "00_rules.md"), "Follow the rules.\n")
	return &project{t: t, root: root, paths: config.DefaultPaths(root)}
}

func writeProjectFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { // #nosec G703 -- path is always rooted at a t.TempDir() the test owns.
		t.Fatalf("write %s: %v", path, err)
	}
}

// Service returns an import service bound to this project.
func (p *project) Service() *Service { return New(p.root) }

// AppendConfig appends raw configuration content.
func (p *project) AppendConfig(content string) {
	p.t.Helper()
	existing, err := os.ReadFile(p.paths.ConfigPath) // #nosec G304 -- test-controlled path.
	if err != nil {
		p.t.Fatalf("read config: %v", err)
	}
	writeProjectFile(p.t, p.paths.ConfigPath, string(existing)+content) // #nosec G703 -- the path is the test project's own configuration file.
}

// ConfigContent returns the current configuration file content.
func (p *project) ConfigContent() string {
	p.t.Helper()
	data, err := os.ReadFile(p.paths.ConfigPath) // #nosec G304 -- test-controlled path.
	if err != nil {
		p.t.Fatalf("read config: %v", err)
	}
	return string(data)
}

// Lock returns the current lock document, or nil when none exists.
func (p *project) Lock() *skilllock.File {
	p.t.Helper()
	file, err := skilllock.Load(p.paths.SkillsLockPath)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil
		}
		p.t.Fatalf("load lock: %v", err)
	}
	return file
}

// ImportedFile returns the content of a file inside an imported skill.
func (p *project) ImportedFile(skill string, relative string) string {
	p.t.Helper()
	path := filepath.Join(p.paths.ImportedSkillsDir, skill, filepath.FromSlash(relative))
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		p.t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// WriteImportedFile edits a file inside an imported skill, simulating local work.
func (p *project) WriteImportedFile(skill string, relative string, content string) {
	p.t.Helper()
	path := filepath.Join(p.paths.ImportedSkillsDir, skill, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		p.t.Fatalf("mkdir: %v", err)
	}
	writeProjectFile(p.t, path, content)
}

// ImportedExists reports whether an imported skill directory is present.
func (p *project) ImportedExists(skill string) bool {
	p.t.Helper()
	_, err := os.Stat(filepath.Join(p.paths.ImportedSkillsDir, skill))
	return err == nil
}

// WriteUserSkill creates a user-managed skill.
func (p *project) WriteUserSkill(name string) {
	p.t.Helper()
	dir := filepath.Join(p.paths.SkillsDir, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		p.t.Fatalf("mkdir: %v", err)
	}
	writeProjectFile(p.t, filepath.Join(dir, "SKILL.md"), skillManifest(name, "User body"))
}

// ProjectedFile returns a projected Claude skill file's content.
func (p *project) ProjectedFile(skill string, relative string) (string, bool) {
	p.t.Helper()
	path := filepath.Join(p.root, ".claude", "skills", skill, filepath.FromSlash(relative))
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		return "", false
	}
	return string(data), true
}

// outcomeFor returns the recorded outcome for a skill.
func outcomeFor(t *testing.T, report *Report, name string) SkillResult {
	t.Helper()
	for _, skill := range report.Skills {
		if skill.Name == name {
			return skill
		}
	}
	t.Fatalf("report has no result for %q: %s", name, report.Render("test"))
	return SkillResult{}
}

// requireOutcome asserts one skill's outcome.
func requireOutcome(t *testing.T, report *Report, name string, want Outcome) SkillResult {
	t.Helper()
	result := outcomeFor(t, report, name)
	if result.Outcome != want {
		t.Fatalf("%s outcome = %q (%v), want %q", name, result.Outcome, result.Err, want)
	}
	return result
}

// mustSkillTree builds a minimal valid skill tree for transaction tests.
func mustSkillTree(t *testing.T, name string, body string) skilltree.Tree {
	t.Helper()
	tree, err := skilltree.NewTree([]skilltree.File{{
		Path: skilltree.SkillManifestName,
		Data: []byte(skillManifest(name, body)),
	}})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return tree
}

// mustEmptyLock returns an empty lock document.
func mustEmptyLock() *skilllock.File { return skilllock.New() }

// mustEscapingTree returns a tree carrying a path that escapes its root. Only
// the transaction and materialization safety checks construct one.
func mustEscapingTree(t *testing.T) skilltree.Tree {
	t.Helper()
	tree, err := skilltree.NewTree([]skilltree.File{{Path: skilltree.SkillManifestName, Data: []byte("x")}})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	// Materialize validates each path, so an unsafe path is injected by
	// re-reading the tree's files through a modified copy.
	files := tree.Files()
	files[0].Path = "../escape.md"
	unsafe, err := skilltree.NewTree(files)
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return unsafe
}
