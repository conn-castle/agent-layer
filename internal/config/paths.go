package config

import "path/filepath"

// Paths holds resolved paths for config files and directories.
type Paths struct {
	Root            string
	ConfigPath      string
	EnvPath         string
	InstructionsDir string
	SkillsDir       string
	// ImportedSkillsDir is the fully managed editable root for skills imported
	// from Git repositories. It is optional on disk: a project with no imports
	// never creates it.
	ImportedSkillsDir string
	// SkillImportLockPath is the canonical resolved import state.
	SkillImportLockPath string
	CommandsAllow       string
}

// DefaultPaths returns the default config paths for a repo root.
func DefaultPaths(root string) Paths {
	return Paths{
		Root:                root,
		ConfigPath:          filepath.Join(root, ".agent-layer", "config.toml"),
		EnvPath:             filepath.Join(root, ".agent-layer", ".env"),
		InstructionsDir:     filepath.Join(root, ".agent-layer", "instructions"),
		SkillsDir:           filepath.Join(root, ".agent-layer", "skills"),
		ImportedSkillsDir:   filepath.Join(root, ".agent-layer", ImportedSkillsDirName),
		SkillImportLockPath: filepath.Join(root, ".agent-layer", SkillImportLockFileName),
		CommandsAllow:       filepath.Join(root, ".agent-layer", "commands.allow"),
	}
}
