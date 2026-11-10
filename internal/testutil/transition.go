package testutil

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Original v0.24.3 release bytes, checked against published manifests by tests.
//
//go:embed testdata/transition
var transitionFixtures embed.FS

// TransitionFixture returns pristine published transition content.
func TransitionFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := transitionFixtures.ReadFile("testdata/transition/" + name)
	require.NoError(t, err)
	return raw
}

// WriteTransitionSkill writes the complete published v0.24.3 tree of a moved
// skill (implement or ship-pr) to .agent-layer/skills/<name> under root.
func WriteTransitionSkill(t *testing.T, root, name string) string {
	t.Helper()
	source := path.Join("testdata/transition/skills", name)
	dest := filepath.Join(root, ".agent-layer", "skills", name)
	require.NoError(t, fs.WalkDir(transitionFixtures, source, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := transitionFixtures.ReadFile(file)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(file[len(source)+1:]))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o600) // #nosec G703 -- fixture paths are embedded and rooted in a test directory.
	}))
	return dest
}
