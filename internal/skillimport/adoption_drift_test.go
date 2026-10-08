package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestAdoptionPreservesConcurrentLegacyChanges(t *testing.T) {
	for _, kind := range []string{"edit", "ignored"} {
		for _, step := range []string{"fetch", "before-journal", "journal", "retire:ship-pr", "write:ship-pr", "config", "lock", "open-directory:lock"} {
			if kind == "edit" && (step == "fetch" || step == "before-journal" || step == "open-directory:lock") {
				continue
			}
			t.Run(kind+"/"+step, func(t *testing.T) {
				testutil.CatalogGitFixture(t)
				proj := newProject(t)
				original := writeLegacy(t, proj, "ship-pr")
				source := filepath.Join(proj.paths.SkillsDir, "ship-pr")
				beforeConfig := proj.ConfigContent()
				before, err := os.ReadFile(filepath.Join(source, "SKILL.md")) // #nosec G304 -- test-owned legacy source.
				require.NoError(t, err)
				edited := append(append([]byte{}, before...), []byte("\nA concurrent personal edit must survive.\n")...)
				opened, err := os.OpenRoot(source)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, opened.Close()) })
				insert := func(dir string) {
					if kind == "edit" {
						require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), edited, 0o600)) // #nosec G703 -- dir is a test-owned legacy or staging root.
					} else {
						node := filepath.Join(dir, "references", ".git", "unpushed")
						require.NoError(t, os.MkdirAll(filepath.Dir(node), 0o750))
						require.NoError(t, os.WriteFile(node, []byte("history"), 0o600))
					}
				}
				service := New(proj.root)
				if step == "fetch" {
					service.newRunner = func(env map[string]string) (*gitrepo.Runner, error) { insert(source); return gitrepo.NewRunner(env) }
				} else {
					service.newTransaction = func(paths pathSet, lock *skilllock.File) *transaction {
						tx := newTransaction(paths, lock)
						tx.checkpoint = func(current string) {
							if step == "open-directory:lock" && current == "lock" {
								require.NoError(t, opened.MkdirAll("references/.git", 0o750))
								require.NoError(t, opened.WriteFile("references/.git/unpushed", []byte("history"), 0o600))
								return
							}
							if current != step {
								return
							}
							dir := source
							if step != "journal" && step != "before-journal" {
								dir = filepath.Join(skilljournal.StagingRoot(paths.ImportedSkillsDir), skilljournal.LocalBackupPrefix+"ship-pr")
							}
							insert(dir)
						}
						return tx
					}
				}
				_, err = service.InstallCatalog(context.Background(), []string{"skills/development/ship-pr"}, []string{"skills/development/ship-pr"})
				require.Error(t, err)
				require.Equal(t, beforeConfig, proj.ConfigContent())
				require.Nil(t, proj.Lock())
				if kind == "edit" {
					actual, err := os.ReadFile(filepath.Join(source, "SKILL.md")) // #nosec G304 -- test-owned restored legacy source.
					require.NoError(t, err)
					require.Equal(t, edited, actual)
				} else {
					require.ErrorContains(t, err, "move and preserve")
					raw, err := os.ReadFile(filepath.Join(source, "references", ".git", "unpushed")) // #nosec G304 -- owned raw history evidence.
					require.NoError(t, err)
					require.Equal(t, "history", string(raw))
					tree, err := skilltree.Read(skilltree.OSFS{}, source)
					require.NoError(t, err)
					require.Equal(t, original.Hash(), tree.Hash())
				}
				require.NoDirExists(t, filepath.Join(proj.paths.ImportedSkillsDir, "ship-pr"))
			})
		}
	}
}
