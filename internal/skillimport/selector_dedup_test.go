package skillimport

import (
	"context"
	"testing"
)

// TestOverlappingSelectorsImportEachSkillOnce covers selector resolution when a
// user names the same skill twice — once literally and once through a wildcard
// that also matches it. Each upstream skill must resolve to exactly one import,
// because a second candidate for the same path would either duplicate the skill
// or make its lock ownership ambiguous.
func TestOverlappingSelectorsImportEachSkillOnce(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.WriteSkill("other/gamma", "gamma", "Gamma body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{
		"skills/alpha",
		"skills/*",
	}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	requireOutcome(t, report, "beta", OutcomeImported)

	lock := proj.Lock()
	if _, ok := lock.Entry("gamma"); ok {
		t.Fatal("a selector matched a skill outside the configured prefix")
	}
	alphaResults := 0
	for _, result := range report.Skills {
		if result.Name == "alpha" {
			alphaResults++
		}
	}
	if alphaResults != 1 {
		t.Fatalf("alpha was reported %d times, want exactly one import", alphaResults)
	}
	if body := proj.ImportedFile("alpha", "SKILL.md"); body == "" {
		t.Fatal("alpha was not imported")
	}
}
