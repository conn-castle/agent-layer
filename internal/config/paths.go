package config

import "path/filepath"

const (
	// ImportedSkillsDirName is the fully managed editable source tier for
	// Git-backed skill imports, relative to .agent-layer/.
	ImportedSkillsDirName = "skills-imported"
	// SkillsLockFileName is the machine-managed skill import lock, relative to
	// .agent-layer/.
	SkillsLockFileName = "skills.lock.json"
)

// Paths holds resolved paths for config files and directories.
type Paths struct {
	Root              string
	ConfigPath        string
	EnvPath           string
	InstructionsDir   string
	SkillsDir         string
	ImportedSkillsDir string
	SkillsLockPath    string
	CommandsAllow     string
}

// DefaultPaths returns the default config paths for a repo root.
func DefaultPaths(root string) Paths {
	return Paths{
		Root:              root,
		ConfigPath:        filepath.Join(root, ".agent-layer", "config.toml"),
		EnvPath:           filepath.Join(root, ".agent-layer", ".env"),
		InstructionsDir:   filepath.Join(root, ".agent-layer", "instructions"),
		SkillsDir:         filepath.Join(root, ".agent-layer", "skills"),
		ImportedSkillsDir: filepath.Join(root, ".agent-layer", ImportedSkillsDirName),
		SkillsLockPath:    filepath.Join(root, ".agent-layer", SkillsLockFileName),
		CommandsAllow:     filepath.Join(root, ".agent-layer", "commands.allow"),
	}
}
