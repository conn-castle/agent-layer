package skillimport

import (
	"context"
	"strings"
	"testing"
)

// TestResetRefusesToDiscardLocalEditsItCannotReplace covers the safety contract
// of the one permanently destructive skill operation: reset overwrites a user's
// local edits, so it must refuse whenever the replacement it would install is
// not unambiguously the configured upstream for that skill. Every refusal must
// leave the local content untouched.
func TestResetRefusesToDiscardLocalEditsItCannotReplace(t *testing.T) {
	tests := []struct {
		name       string
		arrange    func(t *testing.T, source *gitRepo, proj *project)
		wantReason string
	}{
		{
			name: "the skill is no longer selected by configuration",
			arrange: func(t *testing.T, _ *gitRepo, proj *project) {
				proj.ReplaceInConfig(`"skills/alpha"`, `"skills/nothing"`)
			},
			wantReason: "no longer selected by configuration",
		},
		{
			name: "upstream path is no longer a valid skill",
			arrange: func(t *testing.T, source *gitRepo, _ *project) {
				source.WriteFile("skills/alpha/SKILL.md", "no front matter\n", 0o644)
				source.Commit("break alpha upstream")
			},
			wantReason: "not a valid skill",
		},
		{
			name: "a user-managed skill already owns the name",
			arrange: func(t *testing.T, _ *gitRepo, proj *project) {
				proj.WriteUserSkill("alpha")
			},
			wantReason: "already owns the name",
		},
		{
			name:       "the name has no lock entry",
			arrange:    func(t *testing.T, _ *gitRepo, _ *project) {},
			wantReason: "has no lock entry",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := newGitRepo(t, "main")
			source.WriteSkill("skills/alpha", "alpha", "Alpha original")
			source.Commit("add alpha")

			proj := newProject(t)
			proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
			if _, err := proj.Service().Pull(context.Background()); err != nil {
				t.Fatalf("initial pull: %v", err)
			}
			proj.WriteImportedFile("alpha", "notes.md", "local work\n")
			test.arrange(t, source, proj)

			target := "alpha"
			if test.wantReason == "has no lock entry" {
				target = "never-imported"
			}
			if _, err := proj.Service().Reset(context.Background(), target); err == nil {
				t.Fatal("reset discarded local edits it could not safely replace")
			} else if !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("error = %v, want it to explain %q", err, test.wantReason)
			}
			if got := proj.ImportedFile("alpha", "notes.md"); got != "local work\n" {
				t.Fatalf("refused reset still changed local content: %q", got)
			}
		})
	}
}
