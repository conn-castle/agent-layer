package templates

import (
	"embed"
	"io/fs"
)

// FS contains the embedded default templates for installation and generation.
//
//go:embed config.toml commands.allow env gitignore.block agent-layer.gitignore instructions/*.md skills/*/SKILL.md docs/agent-layer/*.md launchers manifests/*.json migrations/*.json
var FS embed.FS

// ReadFunc is the function used by Read. Tests can replace it to simulate errors.
var ReadFunc = func(path string) ([]byte, error) {
	return fs.ReadFile(FS, path)
}

// WalkFunc is the function used by Walk. Tests can replace it to simulate errors.
var WalkFunc = func(root string, fn fs.WalkDirFunc) error {
	return fs.WalkDir(FS, root, fn)
}

// Read returns the contents of an embedded template file.
func Read(path string) ([]byte, error) {
	return ReadFunc(path)
}

// Walk walks the embedded template filesystem under the given root.
func Walk(root string, fn fs.WalkDirFunc) error {
	return WalkFunc(root, fn)
}
