package skillimport

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestPullResolvesRepositoryPlaceholdersWithoutRecordingThem proves the whole
// contract of a placeholder-backed repository in one pass: the import works
// against the resolved value, and the placeholder text — not the value — is
// what stays in configuration, in the lockfile, and in status output.
func TestPullResolvesRepositoryPlaceholdersWithoutRecordingThem(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.WriteEnv(map[string]string{"AL_SKILLS_REPOSITORY": source.URL()})
	proj.AppendConfig(importBlock("${AL_SKILLS_REPOSITORY}", []string{"skills/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("al skills pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("the placeholder-backed import produced no content")
	}

	// The lock records the configured text. Anything else would persist the
	// resolved value and make the recorded identity depend on the machine.
	entry, ok := proj.Lock().Entry("alpha")
	if !ok {
		t.Fatal("no lock entry was recorded")
	}
	if entry.Repository != "${AL_SKILLS_REPOSITORY}" {
		t.Fatalf("lock repository = %q, want the placeholder text", entry.Repository)
	}
	for name, content := range map[string]string{
		"skills.lock.json": proj.LockContent(),
		"config.toml":      proj.ConfigContent(),
	} {
		if strings.Contains(content, source.URL()) {
			t.Fatalf("%s persisted the resolved value:\n%s", name, content)
		}
	}

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	rendered := status.Render(true)
	if strings.Contains(rendered, source.URL()) {
		t.Fatalf("status disclosed the resolved value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "${AL_SKILLS_REPOSITORY}") {
		t.Fatalf("status did not report the configured reference:\n%s", rendered)
	}

	// A second pull reconciles against the same recorded identity rather than
	// treating the placeholder as a changed repository.
	second, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second Pull: %v", err)
	}
	requireOutcome(t, second, "alpha", OutcomeUnchanged)
}

// TestPullFailsActionablyWhenAPlaceholderIsUndefined proves a referenced value
// that .agent-layer/.env does not define stops the block with a message naming
// the variable and the file, rather than handing git a half-substituted URL.
func TestPullFailsActionablyWhenAPlaceholderIsUndefined(t *testing.T) {
	proj := newProject(t)
	proj.WriteEnv(map[string]string{"AL_UNRELATED": "value"})
	proj.AppendConfig(importBlock("https://${AL_SKILLS_TOKEN}@example.test/skills.git", []string{"skills/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("an undefined placeholder was tolerated:\n%s", report.Render("al skills pull"))
	}
	if len(report.Sources) != 1 {
		t.Fatalf("source failures = %+v, want one", report.Sources)
	}
	message := report.Sources[0].Err.Error()
	for _, want := range []string{"${AL_SKILLS_TOKEN}", ".agent-layer/.env"} {
		if !strings.Contains(message, want) {
			t.Fatalf("failure %q does not name %q", message, want)
		}
	}
	if proj.Lock() != nil {
		t.Fatal("an unresolvable block recorded lock state")
	}
}

// TestPullRedactsResolvedSecretsFromGitFailures proves the resolved value stays
// out of a failure report even though repository URLs are ordinary git command
// arguments, and git echoes them back in its own diagnostics.
func TestPullRedactsResolvedSecretsFromGitFailures(t *testing.T) {
	// The resolved value is a path that is not a repository, so git fails with
	// the value in both the command arguments and its stderr.
	secret := filepath.Join(t.TempDir(), "s3cr3t-not-a-repository")

	proj := newProject(t)
	proj.WriteEnv(map[string]string{"AL_SKILLS_REPOSITORY": secret})
	proj.AppendConfig(importBlock("${AL_SKILLS_REPOSITORY}", []string{"skills/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("an unreachable repository was tolerated:\n%s", report.Render("al skills pull"))
	}
	rendered := report.Render("al skills pull")
	if strings.Contains(rendered, secret) {
		t.Fatalf("the report disclosed the resolved value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "${AL_SKILLS_REPOSITORY}") {
		t.Fatalf("the report does not name the configured reference:\n%s", rendered)
	}
}

// TestConfigurationRejectsLiteralCredentialsButAcceptsPlaceholders proves the
// policy boundary end to end: a literal secret never loads, while the same URL
// shape written as a placeholder does.
func TestConfigurationRejectsLiteralCredentialsButAcceptsPlaceholders(t *testing.T) {
	proj := newProject(t)
	proj.WriteEnv(map[string]string{"AL_SKILLS_TOKEN": "token-value"})
	// #nosec G101 -- an invented credential in a fixture proving such URLs are refused.
	proj.AppendConfig(importBlock("https://user:literal-secret@example.test/skills.git", []string{"skills/alpha"}))

	_, err := proj.Service().Status()
	if err == nil {
		t.Fatal("a literal credential in a repository URL was accepted")
	}
	if !strings.Contains(err.Error(), "literal password") {
		t.Fatalf("error %q does not explain the rejection", err)
	}
	if strings.Contains(err.Error(), "literal-secret") {
		t.Fatalf("the rejection echoed the credential back: %v", err)
	}

	placeholder := newProject(t)
	placeholder.WriteEnv(map[string]string{"AL_SKILLS_TOKEN": "token-value"})
	placeholder.AppendConfig(importBlock("https://oauth2:${AL_SKILLS_TOKEN}@example.test/skills.git", []string{"skills/alpha"}))
	if _, err := placeholder.Service().Status(); err != nil {
		t.Fatalf("a placeholder-backed credential was rejected: %v", err)
	}
}

// TestPushResolvesPlaceholdersForSourceAndDestination proves a push works end
// to end when both repositories are placeholder-backed, and that the configured
// text — not the resolved value — is what groups the work, advances the lock,
// and appears in the report.
//
// Push is the operation with the most places a resolved value could escape: it
// opens a source and a destination, groups by destination identity, compares
// the group against the recorded repository to decide lock advancement, and
// renders the destination into every result detail.
func TestPushResolvesPlaceholdersForSourceAndDestination(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/zulu", "zulu", "Zulu body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.WriteEnv(map[string]string{"AL_SKILLS_REPOSITORY": source.URL()})
	// The destination is the same repository, written through the same
	// placeholder, so the push targets the exact tracked source ref and the
	// lock is eligible to advance.
	proj.AppendConfig(importBlock("${AL_SKILLS_REPOSITORY}", []string{"skills/*"},
		`write_policy = "direct"`, `push_repository = "${AL_SKILLS_REPOSITORY}"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("al skills push"))
	}
	pushed := requireOutcome(t, report, "alpha", OutcomePushed)
	if !strings.Contains(pushed.Detail, "lock advanced") {
		t.Fatalf("a push to the tracked source ref did not advance the lock: %q", pushed.Detail)
	}
	// The unchanged sibling rides the same commit, which only works when the
	// group and the lock entry agree on the configured text.
	unchanged := requireOutcome(t, report, "zulu", OutcomeUnchanged)
	if !strings.Contains(unchanged.Detail, "lock advanced") {
		t.Fatalf("the unchanged sibling did not advance with the group: %q", unchanged.Detail)
	}

	if got := source.FileAt("main", "skills/alpha/notes.md"); got != "local note" {
		t.Fatalf("the placeholder-backed push published %q", got)
	}
	head := source.Head("main")
	lock := proj.Lock()
	for _, name := range []string{"alpha", "zulu"} {
		entry, ok := lock.Entry(name)
		if !ok {
			t.Fatalf("%s left the lock", name)
		}
		if entry.Repository != "${AL_SKILLS_REPOSITORY}" {
			t.Fatalf("%s lock repository = %q, want the placeholder text", name, entry.Repository)
		}
		if entry.Commit != head {
			t.Fatalf("%s is locked to %s, want the pushed commit %s", name, entry.Commit, head)
		}
	}

	// Neither the report nor the persisted state may carry the resolved value.
	rendered := report.Render("al skills push")
	if strings.Contains(rendered, source.URL()) {
		t.Fatalf("the push report disclosed the resolved value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "${AL_SKILLS_REPOSITORY}") {
		t.Fatalf("the push report does not name the configured destination:\n%s", rendered)
	}
	if strings.Contains(proj.LockContent(), source.URL()) {
		t.Fatalf("the lockfile persisted the resolved value:\n%s", proj.LockContent())
	}
}

// TestPushRedactsResolvedSecretsFromDestinationFailures proves the redaction
// covers the destination side too: a push opens a second repository, and its
// git failures carry that URL just as the source's do.
func TestPushRedactsResolvedSecretsFromDestinationFailures(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	// The destination resolves to a path that is not a repository, so the push
	// fails with the value in both the command arguments and git's stderr.
	secret := filepath.Join(t.TempDir(), "s3cr3t-destination")

	proj := newProject(t)
	proj.WriteEnv(map[string]string{
		"AL_SKILLS_REPOSITORY": source.URL(),
		"AL_SKILLS_FORK":       secret,
	})
	proj.AppendConfig(importBlock("${AL_SKILLS_REPOSITORY}", []string{"skills/alpha"},
		`write_policy = "direct"`, `push_repository = "${AL_SKILLS_FORK}"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("an unreachable destination was tolerated:\n%s", report.Render("al skills push"))
	}
	rendered := report.Render("al skills push")
	if strings.Contains(rendered, secret) {
		t.Fatalf("the push report disclosed the resolved destination:\n%s", rendered)
	}
	if !strings.Contains(rendered, "${AL_SKILLS_FORK}") {
		t.Fatalf("the push report does not name the configured destination:\n%s", rendered)
	}
}
