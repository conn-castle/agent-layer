package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestFlatSkillNameAddsOnlyMissingEligibleIdentity(t *testing.T) {
	raw := "\xef\xbb\xbf---\r\ndescription: Keep\r\nmetadata: {name: nested}\r\n---\r\nBody\x00\xff"
	want := "\xef\xbb\xbf---\r\nname: \"audit-documentation\"\r\ndescription: Keep\r\nmetadata: {name: nested}\r\n---\r\nBody\x00\xff"
	require.Equal(t, []byte(want), addMissingFlatSkillName([]byte(raw), "audit-documentation"))
	require.Equal(t, []byte("---\nname: \"123\"\ndescription: Keep\n---"), addMissingFlatSkillName([]byte("---\ndescription: Keep\n---"), "123"))
	for _, raw := range []string{
		"plain custom workflow", "---\nname: null\ndescription: Keep\n---",
		"---\nname: wrong-name\ndescription: Keep\n---", "---\ncustom: keep\n---",
		"---\ndescription: null\n---", "---\ndescription: [\n---",
		"---\ndescription: Keep\n", "---\ndescription: first\ndescription: second\n---",
		"---\nmetadata: &metadata {name: inherited}\n<<: *metadata\ndescription: Keep\n---",
		"---\ncustom: &key name\n? *key\n: inherited\ndescription: Keep\n---",
		"---\ndescription: Language-agnostic cleanup pass: optionally remove dead code.\n---",
	} {
		require.Equal(t, []byte(raw), addMissingFlatSkillName([]byte(raw), "audit-documentation"))
	}
	require.Equal(t, []byte(raw), addMissingFlatSkillName([]byte(raw), "INVALID"))
}

type flatNameFailureSystem struct {
	RealSystem
	failWrite, failRemove, failAfterWrite bool
}

func (s flatNameFailureSystem) WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	if s.failWrite {
		return errors.New("injected metadata write failure")
	}
	if err := s.RealSystem.WriteFileAtomic(path, data, mode); err != nil {
		return err
	}
	if s.failAfterWrite {
		return errors.New("injected post-publication sync failure")
	}
	return nil
}
func (s flatNameFailureSystem) RemoveAll(path string) error {
	if s.failRemove {
		return errors.New("injected flat removal failure")
	}
	return s.RealSystem.RemoveAll(path)
}

func TestFlatMigrationPublicationFailuresPreserveSourceAndAllowRetry(t *testing.T) {
	for _, sys := range []flatNameFailureSystem{{failWrite: true}, {failAfterWrite: true}, {failRemove: true}} {
		root := t.TempDir()
		flat, dir := filepath.Join(root, "alpha.md"), filepath.Join(root, "alpha")
		raw := []byte("---\ndescription: Keep\n---\nOriginal body")
		require.NoError(t, os.WriteFile(flat, raw, 0o751)) // #nosec G306 -- executable source mode is a preservation requirement.
		before := testutil.SnapshotEvidence(t, flat)
		changed, err := migrateSingleFlatSkill(sys, flat, dir, filepath.Join(dir, "SKILL.md"))
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, testutil.SnapshotEvidence(t, flat))
		count, conflicts, err := preflightSkillsMigration(RealSystem{}, root)
		require.NoError(t, err)
		require.Equal(t, 1, count)
		require.Empty(t, conflicts)
		changed, err = migrateSingleFlatSkill(RealSystem{}, flat, dir, filepath.Join(dir, "SKILL.md"))
		require.NoError(t, err)
		require.True(t, changed)
		data, err := os.ReadFile(filepath.Join(dir, "SKILL.md")) // #nosec G304 -- test-owned migration output.
		require.NoError(t, err)
		require.Equal(t, []byte("---\nname: \"alpha\"\ndescription: Keep\n---\nOriginal body"), data)
		info, err := os.Stat(filepath.Join(dir, "SKILL.md"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o751), info.Mode().Perm())
	}
}

func TestFlatMigrationRefusesLinkedSourceOrDestination(t *testing.T) {
	for _, node := range []string{"flat symlink", "directory", "manifest"} {
		root, outside := t.TempDir(), t.TempDir()
		flat, dir := filepath.Join(root, "alpha.md"), filepath.Join(root, "alpha")
		dest := filepath.Join(dir, "SKILL.md")
		require.NoError(t, os.WriteFile(flat, []byte("original"), 0o600))
		switch node {
		case "flat symlink":
			require.NoError(t, os.Remove(flat))
			require.NoError(t, os.Symlink(outside, flat))
		case "directory":
			require.NoError(t, os.Symlink(outside, dir))
		default:
			require.NoError(t, os.Mkdir(dir, 0o750))
			require.NoError(t, os.Symlink(outside, dest))
		}
		before := testutil.SnapshotEvidence(t, root, outside)
		_, _, err := preflightSkillsMigration(RealSystem{}, root)
		require.Error(t, err)
		changed, err := migrateSingleFlatSkill(RealSystem{}, flat, dir, dest)
		require.Error(t, err)
		require.False(t, changed)
		require.Equal(t, before, testutil.SnapshotEvidence(t, root, outside))
	}
}
