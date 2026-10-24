package gitrepo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretsResolveKeepsConfiguredTextCanonical proves the two forms of a
// repository reference stay separate: the configured text is what any caller
// can read back, and the resolved value reaches git alone.
func TestSecretsResolveKeepsConfiguredTextCanonical(t *testing.T) {
	t.Parallel()
	secrets := NewSecrets(map[string]string{
		"AL_SKILLS_HOST":  "git.example.test",
		"AL_SKILLS_TOKEN": "s3cr3t-token-value",
	})

	repository, err := secrets.Resolve("https://${AL_SKILLS_TOKEN}@${AL_SKILLS_HOST}/org/skills.git")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repository.String() != "https://${AL_SKILLS_TOKEN}@${AL_SKILLS_HOST}/org/skills.git" {
		t.Fatalf("String() = %q, want the configured text", repository.String())
	}
	if repository.git != "https://s3cr3t-token-value@git.example.test/org/skills.git" {
		t.Fatalf("resolved value = %q", repository.git)
	}

	// Formatting the value through fmt must not reach the resolved form. That is
	// what makes every message in this package safe by construction rather than
	// by each call site remembering to pick the right field.
	for _, verb := range []string{"%s", "%v", "%q"} {
		formatted := fmt.Sprintf(verb, repository)
		if strings.Contains(formatted, "s3cr3t-token-value") {
			t.Fatalf("formatting with %s disclosed the secret: %s", verb, formatted)
		}
		if !strings.Contains(formatted, "${AL_SKILLS_TOKEN}") {
			t.Fatalf("formatting with %s dropped the configured text: %s", verb, formatted)
		}
	}
}

// TestSecretsResolvePassesThroughLiteralReferences proves a reference with no
// placeholder is untouched and contributes nothing to redact, so ordinary
// output is never rewritten.
func TestSecretsResolvePassesThroughLiteralReferences(t *testing.T) {
	t.Parallel()
	secrets := NewSecrets(map[string]string{"AL_UNUSED": "main"})
	repository, err := secrets.Resolve("https://example.test/skills.git")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if repository.String() != "https://example.test/skills.git" || repository.git != repository.String() {
		t.Fatalf("literal reference was rewritten: %+v", repository)
	}
	if got := secrets.Redact("checked out branch main"); got != "checked out branch main" {
		t.Fatalf("an unused variable value was redacted: %q", got)
	}
}

// TestSecretsResolveReportsMissingVariables proves an unresolvable reference
// fails with the variable names and the file that must define them, rather than
// handing git a half-substituted URL.
func TestSecretsResolveReportsMissingVariables(t *testing.T) {
	t.Parallel()
	secrets := NewSecrets(map[string]string{"AL_PRESENT": "value", "AL_EMPTY": ""})

	_, err := secrets.Resolve("https://${AL_MISSING_USER}:${AL_MISSING_TOKEN}@host/${AL_PRESENT}.git")
	if err == nil {
		t.Fatal("an undefined placeholder resolved")
	}
	for _, want := range []string{"${AL_MISSING_TOKEN}", "${AL_MISSING_USER}", ".agent-layer/.env"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}

	// An empty value is as unusable as an absent one and is reported the same
	// way, rather than silently producing an empty credential.
	if _, err := secrets.Resolve("https://${AL_EMPTY}@host/skills.git"); err == nil {
		t.Fatal("an empty value resolved")
	}
}

// TestSecretsRedactPrefersLongerValues proves a secret that contains another
// secret cannot be left partly exposed by an earlier substitution.
func TestSecretsRedactPrefersLongerValues(t *testing.T) {
	t.Parallel()
	secrets := NewSecrets(map[string]string{
		"AL_SHORT": "token",
		"AL_LONG":  "token-with-suffix",
	})
	if _, err := secrets.Resolve("https://${AL_SHORT}@host/a.git"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := secrets.Resolve("https://${AL_LONG}@host/b.git"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	got := secrets.Redact("fatal: could not read from https://token-with-suffix@host/b.git")
	if strings.Contains(got, "token-with-suffix") {
		t.Fatalf("the longer secret survived redaction: %q", got)
	}
	if got != "fatal: could not read from https://${AL_LONG}@host/b.git" {
		t.Fatalf("redacted text = %q", got)
	}
}

// TestSecretsRedactCoversDiagnosticSpellings proves a credential stays hidden
// when Git or a transport echoes it percent-decoded, quoted, or re-encoded
// rather than as the substituted text.
func TestSecretsRedactCoversDiagnosticSpellings(t *testing.T) {
	t.Parallel()
	secrets := NewSecrets(map[string]string{"AL_TOKEN": "dog%22food-secret%40sentinel"}) // #nosec G101 -- intentionally fake credential for redaction regression.
	if _, err := secrets.Resolve("https://oauth2:${AL_TOKEN}@host/repo.git"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, tc := range []struct{ name, text string }{
		{"raw", "fatal: https://oauth2:dog%22food-secret%40sentinel@host/repo.git"},
		{"decoded", `fatal: https://oauth2:dog"food-secret@sentinel@host/repo.git`},
		{"quoted decoded", `fatal: "https://oauth2:dog\"food-secret@sentinel@host/repo.git"`},
		{"re-encoded", "fatal: https://oauth2:dog%2522food-secret%2540sentinel@host/repo.git"},
	} {
		got := secrets.Redact(tc.text)
		if strings.Contains(got, "food-secret") || !strings.Contains(got, "oauth2:${AL_TOKEN}@host") {
			t.Fatalf("%s spelling was not redacted: %q", tc.name, got)
		}
	}
	if got := secrets.Redact("unrelated sentinel@host text"); got != "unrelated sentinel@host text" {
		t.Fatalf("Redact changed text without a resolved spelling: %q", got)
	}

	// Git decodes the well-formed escapes and copies a malformed `%` through.
	malformed := NewSecrets(map[string]string{"AL_TOKEN": "100%_sure%40token"}) // #nosec G101 -- intentionally fake credential for redaction regression.
	if _, err := malformed.Resolve("https://oauth2:${AL_TOKEN}@host/repo.git"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := malformed.Redact("fatal: https://oauth2:100%_sure@token@host/repo.git"); got != "fatal: https://oauth2:${AL_TOKEN}@host/repo.git" {
		t.Fatalf("leniently decoded spelling was not redacted: %q", got)
	}
}

// TestCommandErrorRedactsResolvedSecrets proves the redaction reaches a real
// git failure. A repository URL is an ordinary command argument and git echoes
// it back in its own diagnostics, so both the arguments and the captured stderr
// have to be rewritten.
func TestCommandErrorRedactsResolvedSecrets(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "s3cr3t-not-a-repository")
	runner, err := NewRunner(map[string]string{"AL_SKILLS_REPOSITORY": secret})
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	repository, err := runner.Secrets().Resolve("${AL_SKILLS_REPOSITORY}")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	_, err = runner.run(context.Background(), t.TempDir(), "ls-remote", "--", repository.git)
	if err == nil {
		t.Fatal("expected an unreachable repository to fail")
	}
	rendered := err.Error()
	if strings.Contains(rendered, secret) {
		t.Fatalf("the git failure disclosed the resolved value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "${AL_SKILLS_REPOSITORY}") {
		t.Fatalf("the git failure does not name the configured reference:\n%s", rendered)
	}
}
