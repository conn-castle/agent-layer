package benchmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func TestFrozenProvenanceUsesRecordedSelectorsAndIgnoresOnlyMetadata(t *testing.T) {
	for _, kind := range []string{"alternate recorded set", "ignored metadata", "extra directory", "extra ordinary file", "extra hidden file", "unsafe selector", "glob selector", "exclusion selector", "duplicate leaf", "unnormalized selector", "changed tree", "linked skill", "linked root", "empty selectors", "invalid repository", "invalid commit", "invalid ref", "invalid manifest"} {
		t.Run(kind, func(t *testing.T) {
			root, skills, source := frozenSnapshotFixture(t, "older-skill")
			node := filepath.Join(skills, "older-skill")
			hash := source.Trees["historical/group/older-skill"]
			switch kind {
			case "ignored metadata":
				for _, name := range []string{".git", ".DS_Store", "Thumbs.db"} {
					require.NoError(t, os.Symlink(filepath.Join(root, "absent"), filepath.Join(skills, name)))
				}
			case "extra directory":
				require.NoError(t, os.Mkdir(filepath.Join(skills, "extra"), 0o750))
			case "extra ordinary file", "extra hidden file":
				name := "notes.txt"
				if kind == "extra hidden file" {
					name = ".hidden"
				}
				require.NoError(t, os.WriteFile(filepath.Join(skills, name), []byte("content"), 0o600))
			case "unsafe selector":
				source.Trees = map[string]string{"../older-skill": hash}
			case "glob selector":
				source.Trees = map[string]string{"historical/*/older-skill": hash}
			case "exclusion selector":
				source.Trees = map[string]string{"!historical/older-skill": hash}
			case "duplicate leaf":
				source.Trees["other/group/older-skill"] = hash
			case "unnormalized selector":
				source.Trees = map[string]string{" historical/older-skill ": hash}
			case "changed tree":
				require.NoError(t, os.WriteFile(filepath.Join(node, "extra.txt"), []byte("changed"), 0o600))
			case "linked skill":
				target := filepath.Join(root, "saved-skill")
				require.NoError(t, os.Rename(node, target))
				require.NoError(t, os.Symlink(target, node))
			case "linked root":
				target := filepath.Join(root, "saved-root")
				require.NoError(t, os.Rename(skills, target))
				require.NoError(t, os.Symlink(target, skills))
			case "empty selectors":
				source.Trees = nil
			case "invalid repository":
				source.Repository = ""
			case "invalid commit":
				source.Commit = "main"
			case "invalid ref":
				source.ResolvedRef = "bad ref"
			case "invalid manifest":
				invalid, err := skilltree.NewTree([]skilltree.File{{Path: "SKILL.md", Data: []byte("invalid")}})
				require.NoError(t, err)
				require.NoError(t, skilltree.Materialize(invalid, node))
				source.Trees["historical/group/older-skill"] = invalid.Hash()
			}
			data, err := json.Marshal(source)
			require.NoError(t, err)
			provenance := filepath.Join(root, "skills-source.json")
			require.NoError(t, os.WriteFile(provenance, data, 0o600))
			err = validateFrozenSkillsSource(provenance, skills)
			if kind == "alternate recorded set" || kind == "ignored metadata" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// Filesystem snapshot tests use plain frozen inputs; Git fetching is covered by InitStudy tests.
func frozenSnapshotFixture(t *testing.T, name string) (string, string, SkillSnapshotSource) {
	t.Helper()
	root := t.TempDir()
	skills := filepath.Join(root, "treatment", "official-skills")
	tree, err := skilltree.NewTree([]skilltree.File{{Path: "SKILL.md", Data: []byte("---\nname: " + name + "\ndescription: Frozen prior catalog\n---\nContent\n")}, {Path: "resources/example.txt", Data: []byte("resource\n")}})
	require.NoError(t, err)
	require.NoError(t, skilltree.Materialize(tree, filepath.Join(skills, name)))
	source := SkillSnapshotSource{Repository: "https://github.com/example/frozen-skills.git", ResolvedRef: "refs/heads/older", Commit: strings.Repeat("a", 40), Trees: map[string]string{"historical/group/" + name: tree.Hash()}}
	return root, skills, source
}
