package skillimports

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// runWith executes an operation with an injected runner and captures the report.
func (p *project) runWith(runner GitRunner, fn func(*Service) error) (string, error) {
	p.t.Helper()
	var out bytes.Buffer
	service := &Service{
		Root:    p.root,
		Runner:  runner,
		Project: func(string) error { p.projected++; return nil },
		Out:     &out,
	}
	err := fn(service)
	return out.String(), err
}

func TestAddReportsAuthenticationFailureWithRedactedOutput(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	runner := &failingRunner{
		inner:   ExecGitRunner{},
		match:   []string{"ls-remote"},
		message: "fatal: Authentication failed for 'https://user:ghp_secrettoken@example.invalid/skills.git/'",
	}
	var out bytes.Buffer
	service := &Service{Root: p.root, Runner: runner, Project: func(string) error { return nil }, Out: &out}
	err := service.Add(ctx(t), addOptions(repo, "skills/alpha"))
	message := requireError(t, out.String(), err)

	requireContains(t, message, "Authentication failed")
	// A credential that reaches Agent Layer through a helper or an insteadOf rule
	// must not be echoed into a report the user may paste into an issue.
	requireNotContains(t, message, "ghp_secrettoken")
	requireContains(t, p.configText(), "[approvals]")
	requireNotContains(t, p.configText(), "skills.imports")
}

func TestPullFetchFailureLeavesLocalStateIntact(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	locked := p.entry("alpha").SourceCommit
	original, _ := p.readSkillFile("alpha", SkillManifestName)

	runner := &failingRunner{
		inner:   ExecGitRunner{},
		match:   []string{"fetch"},
		message: "fatal: unable to access remote: Could not resolve host",
	}
	out, err = p.runWith(runner, func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "Could not resolve host")

	if got := p.entry("alpha").SourceCommit; got != locked {
		t.Fatalf("a fetch failure moved the lock: %q", got)
	}
	restored, _ := p.readSkillFile("alpha", SkillManifestName)
	if restored != original {
		t.Fatalf("a fetch failure changed local content")
	}
	if p.projected != 1 {
		t.Fatalf("a source failure with nothing to publish must not project again (projections=%d)", p.projected)
	}
}

func TestPushFailureLeavesTheWholeDestinationGroupUnwritten(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/*")) })
	requireNoError(t, out, err)
	lockedBefore := p.entry("alpha").SourceCommit

	p.writeSkillFile("alpha", "references/a.md", "a\n")
	p.writeSkillFile("beta", "references/b.md", "b\n")

	runner := &failingRunner{
		inner:   ExecGitRunner{},
		match:   []string{"push"},
		message: "fatal: remote rejected the update",
	}
	out, err = p.runWith(runner, func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "remote rejected")

	// Both members of the group fail together, and no lock advances.
	if strings.Count(out, "failed") < 2 {
		t.Fatalf("both group members must be reported as failed:\n%s", out)
	}
	if got := p.entry("alpha").SourceCommit; got != lockedBefore {
		t.Fatalf("a failed push advanced the source lock: %q", got)
	}
	if _, ok := repo.fileAt("main", "skills/alpha/references/a.md"); ok {
		t.Fatalf("a failed push wrote to the destination")
	}
}

func TestPullFailsWhenTheMergeBaseIsUnavailable(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/notes.md", "base\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	locked := p.entry("alpha").SourceCommit

	// The user edits locally and upstream moves, so a merge base is required.
	p.writeSkillFile("alpha", "references/notes.md", "local\n")
	repo.writeFile("skills/alpha/references/notes.md", "upstream\n", 0o644)
	repo.commit("upstream change")

	// The locked commit becomes unfetchable, standing in for a rewritten history.
	runner := &failingRunner{
		inner:   ExecGitRunner{},
		match:   []string{"fetch", locked},
		message: "fatal: remote error: upload-pack: not our ref",
	}
	out, err = p.runWith(runner, func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "merge base")

	// Agent Layer must never invent a base: local content and lock both survive.
	notes, _ := p.readSkillFile("alpha", "references/notes.md")
	if notes != "local\n" {
		t.Fatalf("local content changed without a merge base: %q", notes)
	}
	if got := p.entry("alpha").SourceCommit; got != locked {
		t.Fatalf("the lock advanced without a merge base: %q", got)
	}
}

func TestBlockedSourceStillCountsTowardCollisionPreflight(t *testing.T) {
	hermeticGitEnv(t)
	first := newSourceRepo(t, "main")
	first.writeSkill("skills/alpha", "alpha", "First alpha")
	first.commit("add alpha")

	second := newSourceRepo(t, "main")
	second.writeSkill("vendor/alpha", "alpha", "Second alpha")
	second.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(first, "skills/alpha")) })
	requireNoError(t, out, err)

	// The first source becomes unreachable. Its entry is preserved, so the second
	// import still collides with it: a blocked source must not be able to hide a
	// collision and let two owners claim one directory.
	runner := &failingRunner{
		inner:   ExecGitRunner{},
		match:   []string{"ls-remote", first.path()},
		message: "fatal: repository not found",
	}
	out, err = p.runWith(runner, func(s *Service) error {
		return s.Add(ctx(t), addOptions(second, "vendor/alpha"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "collides")
	if got := len(p.lock().Entries); got != 1 {
		t.Fatalf("lock entries = %d, want the single preserved entry", got)
	}
}

func TestGitErrorExposesExitCodeForMergeOutcomes(t *testing.T) {
	hermeticGitEnv(t)
	// git merge-file reports its conflict count as an exit status, so the runner
	// must surface the code rather than flattening every failure into "error".
	runner := ExecGitRunner{}
	_, err := runner.Run(t.Context(), t.TempDir(), "rev-parse", "--verify", "definitely-not-a-ref")
	if err == nil {
		t.Fatal("expected a git failure")
	}
	var gitErr *GitError
	if !errors.As(err, &gitErr) {
		t.Fatalf("error is not a GitError: %v", err)
	}
	if gitErr.ExitCode <= 0 {
		t.Fatalf("exit code = %d, want a positive status", gitErr.ExitCode)
	}
	requireContains(t, gitErr.Error(), "git rev-parse")
}

func TestLockRejectsInconsistentState(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"a tracked tag": `{"version":1,"entries":[{"repository":"r","source_path":"skills/a","configured_ref":"v1",
			"ref_omitted":false,"resolved_ref_name":"v1","resolved_ref_type":"tag","source_commit":"abc",
			"upstream_tree_hash":"h","tracking":"tracked","write":"none","push_repository":"r","push_branch":"","skill_name":"a"}]}`,
		"a duplicate source path": `{"version":1,"entries":[
			{"repository":"r","source_path":"skills/a","configured_ref":"","ref_omitted":true,"resolved_ref_name":"main",
			 "resolved_ref_type":"branch","source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked",
			 "write":"none","push_repository":"r","push_branch":"","skill_name":"a"},
			{"repository":"r","source_path":"skills/a","configured_ref":"","ref_omitted":true,"resolved_ref_name":"main",
			 "resolved_ref_type":"branch","source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked",
			 "write":"none","push_repository":"r","push_branch":"","skill_name":"b"}]}`,
		"two entries claiming one skill name": `{"version":1,"entries":[
			{"repository":"r","source_path":"skills/a","configured_ref":"","ref_omitted":true,"resolved_ref_name":"main",
			 "resolved_ref_type":"branch","source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked",
			 "write":"none","push_repository":"r","push_branch":"","skill_name":"shared"},
			{"repository":"r","source_path":"skills/b","configured_ref":"","ref_omitted":true,"resolved_ref_name":"main",
			 "resolved_ref_type":"branch","source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked",
			 "write":"none","push_repository":"r","push_branch":"","skill_name":"shared"}]}`,
		"a missing upstream hash": `{"version":1,"entries":[{"repository":"r","source_path":"skills/a","configured_ref":"",
			"ref_omitted":true,"resolved_ref_name":"main","resolved_ref_type":"branch","source_commit":"abc",
			"upstream_tree_hash":"","tracking":"tracked","write":"none","push_repository":"r","push_branch":"","skill_name":"a"}]}`,
		"a push branch without branch write mode": `{"version":1,"entries":[{"repository":"r","source_path":"skills/a",
			"configured_ref":"","ref_omitted":true,"resolved_ref_name":"main","resolved_ref_type":"branch",
			"source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked","write":"none","push_repository":"r",
			"push_branch":"contrib","skill_name":"a"}]}`,
		"an unresolved wildcard source path": `{"version":1,"entries":[{"repository":"r","source_path":"skills/*",
			"configured_ref":"","ref_omitted":true,"resolved_ref_name":"main","resolved_ref_type":"branch",
			"source_commit":"abc","upstream_tree_hash":"h","tracking":"tracked","write":"none","push_repository":"r",
			"push_branch":"","skill_name":"a"}]}`,
	}
	for name, document := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := config.ParseSkillImportLock([]byte(document), "lock"); err == nil {
				t.Fatalf("%s must be rejected rather than reconstructed", name)
			}
		})
	}
}
