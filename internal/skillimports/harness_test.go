package skillimports

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitenv"
)

// hermeticGitEnv makes every git invocation in a test independent of the
// developer's global configuration and identity, so results are reproducible on
// any machine and in CI.
func hermeticGitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", filepath.Join(t.TempDir(), "gitconfig-system"))
	t.Setenv("GIT_AUTHOR_NAME", "Agent Layer Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Agent Layer Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
}

// runGit runs git in dir and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitenv.WithoutDiscovery()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return stdout.String()
}

// sourceRepo is a local Git repository used as an import source or push
// destination. Everything is on the local filesystem, so tests never touch a
// network.
type sourceRepo struct {
	t   *testing.T
	dir string
}

// newSourceRepo creates a repository with a deterministic default branch.
func newSourceRepo(t *testing.T, defaultBranch string) *sourceRepo {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", defaultBranch, dir)
	repo := &sourceRepo{t: t, dir: dir}
	// A bare-like server still needs to accept pushes to the checked-out branch
	// in these tests, so the receiving repository updates its work tree instead
	// of refusing the push.
	runGit(t, dir, "config", "receive.denyCurrentBranch", "updateInstead")
	return repo
}

// path returns the repository's filesystem path, which doubles as its URL.
func (r *sourceRepo) path() string { return r.dir }

// writeFile writes a file into the repository work tree.
func (r *sourceRepo) writeFile(relative string, content string, mode os.FileMode) {
	r.t.Helper()
	target := filepath.Join(r.dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		r.t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(content), mode); err != nil {
		r.t.Fatalf("write %s: %v", target, err)
	}
	if err := os.Chmod(target, mode); err != nil {
		r.t.Fatalf("chmod %s: %v", target, err)
	}
}

// writeSkill writes a minimal valid skill at path with the given name.
func (r *sourceRepo) writeSkill(path string, name string, body string) {
	r.t.Helper()
	r.writeFile(path+"/"+SkillManifestName,
		"---\nname: "+name+"\ndescription: The "+name+" skill.\n---\n\n"+body+"\n", 0o644)
}

// removeAll deletes a path from the repository work tree.
func (r *sourceRepo) removeAll(relative string) {
	r.t.Helper()
	if err := os.RemoveAll(filepath.Join(r.dir, filepath.FromSlash(relative))); err != nil {
		r.t.Fatalf("remove %s: %v", relative, err)
	}
}

// commit stages everything and creates a commit, returning its id.
func (r *sourceRepo) commit(message string) string {
	r.t.Helper()
	runGit(r.t, r.dir, "add", "-A")
	runGit(r.t, r.dir, "commit", "--quiet", "-m", message)
	return strings.TrimSpace(runGit(r.t, r.dir, "rev-parse", "HEAD"))
}

// tag creates a lightweight tag at HEAD.
func (r *sourceRepo) tag(name string) {
	r.t.Helper()
	runGit(r.t, r.dir, "tag", name)
}

// branch creates and switches to a branch.
func (r *sourceRepo) branch(name string) {
	r.t.Helper()
	runGit(r.t, r.dir, "checkout", "--quiet", "-b", name)
}

// checkout switches to an existing branch.
func (r *sourceRepo) checkout(name string) {
	r.t.Helper()
	runGit(r.t, r.dir, "checkout", "--quiet", name)
}

// fileAt returns a file's content at a ref, or ok=false when it is absent.
func (r *sourceRepo) fileAt(ref string, relative string) (string, bool) {
	r.t.Helper()
	cmd := exec.Command("git", "show", ref+":"+relative)
	cmd.Dir = r.dir
	cmd.Env = gitenv.WithoutDiscovery()
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// project is a consuming Agent Layer project used by import operations.
type project struct {
	t         *testing.T
	root      string
	projected int
	projectFn func(string) error
}

// newProject creates a minimal .agent-layer layout with a valid config.
func newProject(t *testing.T) *project {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	p := &project{t: t, root: root}
	p.writeConfig(minimalConfig)
	return p
}

const minimalConfig = `[approvals]
mode = "all"

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

func (p *project) writeConfig(content string) {
	p.t.Helper()
	if err := os.WriteFile(filepath.Join(p.root, ".agent-layer", "config.toml"), []byte(content), 0o600); err != nil {
		p.t.Fatalf("write config: %v", err)
	}
}

func (p *project) configText() string {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.root, ".agent-layer", "config.toml"))
	if err != nil {
		p.t.Fatalf("read config: %v", err)
	}
	return string(data)
}

// service builds a Service whose projector only counts calls, so import
// behavior is tested without running the whole sync pipeline.
func (p *project) service(out *bytes.Buffer) *Service {
	projector := func(root string) error {
		p.projected++
		if p.projectFn != nil {
			return p.projectFn(root)
		}
		return nil
	}
	return &Service{Root: p.root, Runner: ExecGitRunner{}, Project: projector, Out: out}
}

// run executes an operation and returns the report output.
func (p *project) run(fn func(*Service) error) (string, error) {
	p.t.Helper()
	var out bytes.Buffer
	err := fn(p.service(&out))
	return out.String(), err
}

// lock reads the current lock file.
func (p *project) lock() *config.SkillImportLock {
	p.t.Helper()
	lock, err := config.LoadSkillImportLock(config.DefaultPaths(p.root).SkillImportLockPath)
	if err != nil {
		p.t.Fatalf("load lock: %v", err)
	}
	return lock
}

// entry returns the lock entry for a skill name.
func (p *project) entry(name string) config.SkillImportLockEntry {
	p.t.Helper()
	for _, entry := range p.lock().Entries {
		if entry.SkillName == name {
			return entry
		}
	}
	p.t.Fatalf("no lock entry for %q; lock has %d entries", name, len(p.lock().Entries))
	return config.SkillImportLockEntry{}
}

// hasEntry reports whether a lock entry exists for a skill name.
func (p *project) hasEntry(name string) bool {
	p.t.Helper()
	for _, entry := range p.lock().Entries {
		if entry.SkillName == name {
			return true
		}
	}
	return false
}

// skillDir returns an imported skill's managed directory.
func (p *project) skillDir(name string) string {
	return filepath.Join(ImportedSkillsRoot(p.root), name)
}

// readSkillFile reads a file inside an imported skill.
func (p *project) readSkillFile(name string, relative string) (string, bool) {
	p.t.Helper()
	data, err := os.ReadFile(filepath.Join(p.skillDir(name), filepath.FromSlash(relative)))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// writeSkillFile edits a file inside an imported skill, simulating a local edit.
func (p *project) writeSkillFile(name string, relative string, content string) {
	p.t.Helper()
	target := filepath.Join(p.skillDir(name), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		p.t.Fatalf("mkdir %s: %v", filepath.Dir(target), err)
	}
	if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
		p.t.Fatalf("write %s: %v", target, err)
	}
}

// writeUserSkill creates a user-managed skill in .agent-layer/skills.
func (p *project) writeUserSkill(name string) {
	p.t.Helper()
	dir := filepath.Join(p.root, ".agent-layer", "skills", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		p.t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := "---\nname: " + name + "\ndescription: A user-managed skill.\n---\n\nBody\n"
	if err := os.WriteFile(filepath.Join(dir, SkillManifestName), []byte(content), 0o600); err != nil {
		p.t.Fatalf("write user skill: %v", err)
	}
}

// addOptions builds AddOptions for a repository and selectors.
func addOptions(repo *sourceRepo, selectors ...string) AddOptions {
	return AddOptions{Repository: repo.path(), Selectors: selectors}
}

// ctx returns a context bound to the test's lifetime.
func ctx(t *testing.T) context.Context {
	t.Helper()
	return t.Context()
}

// requireNoError fails the test with the captured report when err is non-nil.
func requireNoError(t *testing.T, output string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v\nreport:\n%s", err, output)
	}
}

// requireError fails the test when err is nil, and returns the message.
func requireError(t *testing.T, output string, err error) string {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got none\nreport:\n%s", output)
	}
	return err.Error() + "\n" + output
}

// requireContains fails the test when text does not contain want.
func requireContains(t *testing.T, text string, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("expected to find %q in:\n%s", want, text)
	}
}

// requireNotContains fails the test when text contains unwanted.
func requireNotContains(t *testing.T, text string, unwanted string) {
	t.Helper()
	if strings.Contains(text, unwanted) {
		t.Fatalf("expected not to find %q in:\n%s", unwanted, text)
	}
}

// removeAllForTest deletes a path from the managed root, simulating a user who
// deleted an imported skill or one of its files.
func removeAllForTest(path string) error {
	return os.RemoveAll(path)
}

// atoiForTest parses a small integer produced by git output.
func atoiForTest(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// recordingRunner wraps a GitRunner and records every invocation, so tests can
// assert on the exact arguments Agent Layer hands to git.
type recordingRunner struct {
	inner GitRunner
	calls [][]string
}

func (r *recordingRunner) Run(c context.Context, dir string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	return r.inner.Run(c, dir, args...)
}

// failingRunner fails the first invocation whose arguments contain every string
// in match, and delegates everything else. It injects deterministic fetch,
// authentication, and push failures without a network.
type failingRunner struct {
	inner   GitRunner
	match   []string
	message string
	fired   int
}

func (r *failingRunner) Run(c context.Context, dir string, args ...string) ([]byte, error) {
	joined := strings.Join(args, " ")
	matched := true
	for _, needle := range r.match {
		if !strings.Contains(joined, needle) {
			matched = false
			break
		}
	}
	if matched {
		r.fired++
		return nil, &GitError{Args: args, Stderr: r.message, Err: errInjectedGitFailure, ExitCode: 128}
	}
	return r.inner.Run(c, dir, args...)
}

var errInjectedGitFailure = errors.New("injected git failure")
