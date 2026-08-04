package sync

const (
	maxEffort          = "max"
	matcherKey         = "matcher"
	codexFeaturesKey   = "features"
	codexStatusLineKey = "status_line"
	codexProjectsKey   = "projects"
	githubSkillsDir    = ".github/skills"

	// skillManifestName is the canonical projected skill manifest filename.
	skillManifestName = "SKILL.md"
	// lowercaseSkillManifestName is the compatibility filename accepted for
	// user-managed sources; it is never emitted by projection.
	lowercaseSkillManifestName = "skill.md"
)
