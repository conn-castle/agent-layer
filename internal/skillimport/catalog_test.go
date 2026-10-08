package skillimport

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func developmentEntry(t *testing.T) templates.CLISkillCatalogEntry {
	t.Helper()
	catalog, err := templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	for _, entry := range catalog {
		if entry.ID == "development-skills" {
			return entry
		}
	}
	t.Fatal("missing development catalog")
	return templates.CLISkillCatalogEntry{}
}

func writeLegacy(t *testing.T, proj *project, name string) skilltree.Tree {
	t.Helper()
	dir := filepath.Join(proj.paths.SkillsDir, name)
	tree := mustSkillTree(t, name, "Legacy local text\r\n")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, skilltree.Materialize(tree, dir))
	extra := filepath.Join(dir, "references")
	require.NoError(t, os.MkdirAll(extra, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(extra, "repo-specific-pr-policy.md"), []byte("Keep all policy bytes\r\n"), 0o755)) // #nosec G306 -- verifies executable-bit preservation.
	tree, err := skilltree.Read(skilltree.OSFS{}, dir)
	require.NoError(t, err)
	return tree
}

func TestCatalogAdoptionBundleConflictsAreAtomic(t *testing.T) {
	for _, kind := range []string{"invalid sibling", "dual tier"} {
		t.Run(kind, func(t *testing.T) {
			testutil.CatalogGitFixture(t)
			proj := newProject(t)
			writeLegacy(t, proj, "ship-pr")
			entry := developmentEntry(t)
			service := New(proj.root)
			local := filepath.Join(proj.paths.SkillsDir, "implement")
			require.NoError(t, os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(proj.root, ".agent-layer", "sync.lock"), nil, 0o600))
			switch kind {
			case "invalid sibling":
				require.NoError(t, os.Mkdir(local, 0o750))
			case "dual tier":
				require.NoError(t, os.Mkdir(filepath.Join(proj.paths.ImportedSkillsDir, "ship-pr"), 0o750))
			}
			before := testutil.SnapshotEvidence(t, proj.root)
			_, err := service.InstallCatalog(context.Background(), entry.Selectors, entry.Selectors)
			require.Error(t, err)
			require.Equal(t, before, testutil.SnapshotEvidence(t, proj.root), "refusal must preserve both source tiers, lock, config, links and external targets")
		})
	}
}

func TestAdoptionCrashChild(t *testing.T) {
	if os.Getenv("AL_TEST_ADOPTION_CHILD") == "" {
		return
	}
	root, err := os.Getwd()
	require.NoError(t, err)
	service := New(root)
	service.newTransaction = func(paths pathSet, lock *skilllock.File) *transaction {
		txn := newTransaction(paths, lock)
		txn.checkpoint = func(step string) {
			if step == os.Getenv("AL_TEST_CRASH_STEP") {
				os.Exit(73)
			}
		}
		return txn
	}
	if _, err := service.InstallCatalog(context.Background(), []string{"skills/development/ship-pr", "skills/development/implement"}, []string{"skills/development/ship-pr", "skills/development/implement"}); err != nil {
		t.Fatal(err)
	}
	t.Fatal("crash checkpoint never reached")
}

func TestAdoptionCrashRecoveryKeepsOneCommittedGeneration(t *testing.T) {
	for _, step := range []string{"before-journal", "journal", "retire:implement", "retire:ship-pr", "write:implement", "write:ship-pr", "config", "lock", "committed"} {
		t.Run(step, func(t *testing.T) {
			source := testutil.CatalogGitFixture(t)
			proj := newProject(t)
			writeLegacy(t, proj, "ship-pr")
			writeLegacy(t, proj, "implement")
			before := proj.ConfigContent()
			expected := testutil.SnapshotEvidence(t, proj.paths.SkillsDir)
			cmd := exec.Command(os.Args[0], "-test.run=^TestAdoptionCrashChild$") // #nosec G204 G702 -- reexecutes this test binary with a fixed test selector.
			cmd.Dir = proj.root
			cmd.Env = append(os.Environ(), "AL_TEST_ADOPTION_CHILD=1", "AL_TEST_CRASH_STEP="+step)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("child: %v %s", err, out)
			}
			_, err = CatalogState(proj.root, developmentEntry(t))
			require.NoError(t, err)
			tier := proj.paths.SkillsDir
			if step == "committed" {
				tier = proj.paths.ImportedSkillsDir
				remapped := map[string]string{}
				for path, value := range expected {
					remapped[strings.Replace(path, proj.paths.SkillsDir, tier, 1)] = value
				}
				expected = remapped
				require.Equal(t, 2, len(proj.Lock().Skills))
				for _, locked := range proj.Lock().Skills {
					upstream, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(source, locked.SelectedPath))
					require.NoError(t, err)
					require.Equal(t, upstream.Hash(), locked.TreeHash, "adoption must record fetched upstream, never customized local bytes")
				}
				require.NoDirExists(t, filepath.Join(proj.paths.SkillsDir, "ship-pr"))
				require.NoDirExists(t, filepath.Join(proj.paths.SkillsDir, "implement"))
			} else {
				require.Equal(t, before, proj.ConfigContent())
				require.NoDirExists(t, filepath.Join(proj.paths.ImportedSkillsDir, "ship-pr"))
				require.NoDirExists(t, filepath.Join(proj.paths.ImportedSkillsDir, "implement"))
			}
			// Staging is separately checked; compare only the two skill trees, not the tier's root mode.
			delete(expected, tier)
			actual := testutil.SnapshotEvidence(t, tier)
			delete(actual, tier)
			require.Equal(t, expected, actual)

			if _, err := os.Lstat(skilljournal.StagingRoot(proj.paths.ImportedSkillsDir)); !os.IsNotExist(err) {
				t.Fatal("settled recovery staging survived")
			}
		})
	}
}

func TestCatalogForeignOwnershipRequiresUnambiguousConfiguration(t *testing.T) {
	block := config.SkillImport{Repository: "https://example.invalid/fork", Selectors: []string{"skills/tools/playwright"}}
	locked := skilllock.Entry{Name: "playwright", Repository: block.Repository, SelectedPath: block.Selectors[0]}
	st := &state{cfg: &config.Config{}, lock: skilllock.New(), local: map[string]localSkill{"playwright": {Name: "playwright", Present: true}}}
	st.lock.Upsert(locked)
	entry := templates.CLISkillCatalogEntry{Repository: templates.GeneralSkillsRepository, Selectors: block.Selectors}
	for _, imports := range [][]config.SkillImport{{block}, nil, {block, {Repository: entry.Repository, Selectors: entry.Selectors}}} {
		st.cfg.Skills.Imports = imports
		require.Equal(t, len(imports) == 1, catalogMember(st, entry.Repository, entry.Selectors[0]).ForeignOwned)
	}
}
